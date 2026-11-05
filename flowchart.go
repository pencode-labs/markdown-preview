package main

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	mermaid "github.com/zkrebbekx/go-mermaid"
	"github.com/zkrebbekx/go-mermaid/raster"
)

var flowchartTheme = mermaid.WithCustomTheme("pencode", mermaid.Palette{
	Background: "none", NodeFill: "#808081", NodeStroke: "#808080", Text: "#808080", Edge: "#808080",
})

// Only explicit node labels are wrapped; IDs, edges and directives stay intact.
var flowchartLabel = regexp.MustCompile(`([\p{L}\p{N}_-]+)(\[\[?|\(\[|\(\(?|\{\{?)("[^"]*"|[^\[\](){}\n"]+)(\]\]?|\]\)|\)\)?|\}\}?)`)
var flowchartLineBreaks = strings.NewReplacer("<br />", "\n", "<br/>", "\n", "<br>", "\n", `\n`, "\n")

func compactFlowchart(source string) string {
	return flowchartLabel.ReplaceAllStringFunc(source, func(node string) string {
		parts := flowchartLabel.FindStringSubmatch(node)
		label := parts[3]
		quoted := strings.HasPrefix(label, `"`)
		if quoted {
			label = label[1 : len(label)-1]
		}
		var out strings.Builder
		first := true
		for line := range strings.SplitSeq(flowchartLineBreaks.Replace(label), "\n") {
			if !first {
				out.WriteString("<br/>")
			}
			first = false
			for len(line) != 0 {
				width, end, boundary := 0.0, len(line), 0
				var previous rune
				for i, r := range line {
					if unicode.IsSpace(r) || unicode.IsLower(previous) && unicode.IsUpper(r) {
						boundary = i
					}
					width += 7.5
					if r > 255 {
						width += 6.5
					}
					if width > 144 && i > 0 {
						end = i
						if boundary > i/2 {
							end = boundary
						}
						break
					}
					previous = r
				}
				if end == len(line) {
					out.WriteString(line)
					break
				}
				out.WriteString(strings.TrimRightFunc(line[:end], unicode.IsSpace))
				line = strings.TrimSpace(line[end:])
				if line != "" {
					out.WriteString("<br/>")
				}
			}
		}
		wrapped := out.String()
		if quoted {
			wrapped = `"` + wrapped + `"`
		}
		return parts[1] + parts[2] + wrapped + parts[4]
	})
}

type diagramText struct {
	Anchor string `xml:"text-anchor,attr"`
	Weight string `xml:"font-weight,attr"`
	Text   string `xml:",chardata"`
	Lines  []struct {
		Text string  `xml:",chardata"`
		X    float64 `xml:"x,attr"`
		Y    float64 `xml:"y,attr"`
	} `xml:"tspan"`
	X  float64 `xml:"x,attr"`
	Y  float64 `xml:"y,attr"`
	DY float64 `xml:"dy,attr"`
}

// Keep labels as native text: the host can use its CJK fonts and current theme.
// Only the diagram geometry is rasterized; PNGs use the existing cached image
// transport, including when the extension runs on a remote workspace server.
func renderFlowchart(source string) (string, error) {
	if len(source) > 32<<10 {
		return "", fmt.Errorf("流程图源码超过 32 KiB")
	}
	for line := range strings.Lines(source) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		header := line
		if end := strings.IndexAny(header, " \t;"); end >= 0 {
			header = header[:end]
		}
		if header != "graph" && header != "flowchart" {
			return "", fmt.Errorf("支持 graph / flowchart 流程图")
		}
		break
	}
	svg, err := mermaid.Render(compactFlowchart(source), flowchartTheme, mermaid.WithTransparentBackground(), mermaid.WithSpacing(24, 42), mermaid.WithPadding(12))
	if err != nil {
		return "", err
	}
	svg = compactFlowchartLayout(svg)
	var shapes, nodes, labels strings.Builder
	decoder := xml.NewDecoder(bytes.NewReader(svg))
	width, height, offsetX, offsetY := 0.0, 0.0, 0.0, 0.0
	var offsets []flowPoint
	last := int64(0)
	for {
		start := decoder.InputOffset()
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if end, ok := token.(xml.EndElement); ok && end.Name.Local == "g" && len(offsets) > 0 {
			offset := offsets[len(offsets)-1]
			offsetX, offsetY = offset.x, offset.y
			offsets = offsets[:len(offsets)-1]
		}
		element, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch element.Name.Local {
		case "svg":
			for _, attr := range element.Attr {
				if attr.Name.Local == "width" {
					width, _ = strconv.ParseFloat(attr.Value, 64)
				}
				if attr.Name.Local == "height" {
					height, _ = strconv.ParseFloat(attr.Value, 64)
				}
			}
		case "g":
			offsets = append(offsets, flowPoint{offsetX, offsetY})
			for _, attr := range element.Attr {
				if attr.Name.Local == "transform" {
					var x, y float64
					_, _ = fmt.Sscanf(attr.Value, "translate(%f,%f)", &x, &y)
					offsetX, offsetY = offsetX+x, offsetY+y
				}
			}
		case "text":
			var label diagramText
			if err := decoder.DecodeElement(&label, &element); err != nil {
				return "", err
			}
			shapes.Write(svg[last:start])
			last = decoder.InputOffset()
			if len(label.Lines) == 0 {
				writeDiagramText(&labels, label.Text, label.X+offsetX, label.Y+label.DY+offsetY, label.Anchor, label.Weight, label.DY != 0)
			}
			for _, line := range label.Lines {
				writeDiagramText(&labels, line.Text, line.X+offsetX, line.Y+label.DY+offsetY, label.Anchor, label.Weight, label.DY != 0)
			}
		case "rect", "polygon":
			if box, decision, ok := diagramNodeBox(element); ok {
				if err := decoder.Skip(); err != nil {
					return "", err
				}
				// Subroutine boxes have interior lines; keep them together in the raster.
				if element.Name.Local == "rect" && bytes.HasPrefix(bytes.TrimSpace(svg[decoder.InputOffset():]), []byte("<line ")) {
					continue
				}
				shapes.Write(svg[last:start])
				last = decoder.InputOffset()
				if decision {
					// The native HTML renderer supports solid borders only.
					fmt.Fprintf(&shapes, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="16" fill="none" stroke="#808080" stroke-width="1" stroke-dasharray="2,2"/>`, box[0], box[1], box[2], box[3])
				}
				nodes.WriteString(`<div aria-hidden="true" class="diagram-node`)
				if decision {
					nodes.WriteString(" diagram-decision")
				}
				fmt.Fprintf(&nodes, `" style="left:%.2fpx;top:%.2fpx;width:%.2fpx;height:%.2fpx"></div>`, box[0]+offsetX, box[1]+offsetY, box[2], box[3])
			}
		}
	}
	if width <= 0 || height <= 0 || math.IsNaN(width) || math.IsNaN(height) || width > 2048 || height > 2048 {
		return "", fmt.Errorf("流程图尺寸超过 2048 像素")
	}
	shapes.Write(svg[last:])
	geometry := strings.NewReplacer(`fill="#808081"`, `fill="#808080" fill-opacity="0.12"`, `stroke-width="1.5"`, `stroke-width="1" stroke-linejoin="round"`).Replace(shapes.String())
	image, err := raster.RasterizeSVG([]byte(geometry), 1)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<div class="mermaid"><div class="diagram" role="img" aria-label="流程图" style="width:%.2fpx;height:%.2fpx"><img alt="" aria-hidden="true" width="%d" height="%d" src="data:image/png;base64,`, width, height, int(width), int(height))
	out.WriteString(base64.StdEncoding.EncodeToString(image))
	out.WriteString(`">`)
	out.WriteString(nodes.String())
	out.WriteString(labels.String())
	out.WriteString("</div></div>\n")
	return out.String(), nil
}

// Keep the layout's bounds and ports when giving ordinary and decision nodes
// native, theme-aware surfaces. Other Mermaid shapes retain their geometry.
func diagramNodeBox(element xml.StartElement) (box [4]float64, decision, ok bool) {
	fill, points := "", ""
	for _, attr := range element.Attr {
		switch attr.Name.Local {
		case "fill":
			fill = attr.Value
		case "points":
			points = attr.Value
		case "x":
			box[0], _ = strconv.ParseFloat(attr.Value, 64)
		case "y":
			box[1], _ = strconv.ParseFloat(attr.Value, 64)
		case "width":
			box[2], _ = strconv.ParseFloat(attr.Value, 64)
		case "height":
			box[3], _ = strconv.ParseFloat(attr.Value, 64)
		}
	}
	if fill != "#808081" {
		return box, false, false
	}
	if element.Name.Local == "rect" {
		return box, false, true
	}
	var x1, y1, x2, y2, x3, y3, x4, y4 float64
	if n, _ := fmt.Sscanf(points, "%f,%f %f,%f %f,%f %f,%f", &x1, &y1, &x2, &y2, &x3, &y3, &x4, &y4); n == 8 && x1 == x3 && y2 == y4 && x2 > x4 && y3 > y1 {
		return [4]float64{x4, y1, x2 - x4, y3 - y1}, true, true
	}
	return box, false, false
}

func writeDiagramText(out *strings.Builder, text string, x, y float64, anchor, weight string, edge bool) {
	out.WriteString(`<span class="diagram-label`)
	if edge {
		out.WriteString(" diagram-edge-label")
	}
	if edge {
		fmt.Fprintf(out, `" style="left:%.2fpx;top:%.2fpx;transform:translate(-50%%,-50%%);`, x, y+2)
	} else {
		fmt.Fprintf(out, `" style="left:%.2fpx;top:%.2fpx;`, x, y-12)
	}
	if anchor == "middle" && !edge {
		out.WriteString("transform:translateX(-50%);")
	}
	if weight == "bold" {
		out.WriteString("font-weight:bold;")
	}
	out.WriteString(`">`)
	out.WriteString(html.EscapeString(text))
	out.WriteString("</span>")
}
