package main

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
)

type flowPoint struct{ x, y float64 }
type flowBand struct{ left, right float64 }

type flowNode struct {
	children   []int
	box        [4]float64
	start, end int64
	x          float64
	span       float64
	depth      int
}

type flowEdge struct {
	path                string
	points              []flowPoint
	start, end, tailEnd int64
	from, to            int
	labelWidth          float64
}

// Compact vertical trees by aligning each node with its longest continuation.
// Graphs with joins, loops, subgraphs or other directions retain Mermaid's layout.
func compactFlowchartLayout(svg []byte) []byte {
	nodes, edges, ok := readFlowLayout(svg)
	if !ok || len(edges) != len(nodes)-1 || len(nodes) < 2 {
		return svg
	}
	incoming := make([]bool, len(nodes))
	direction := 0.0
	for i := range edges {
		e := &edges[i]
		e.from = flowEndpoint(nodes, e.points[0])
		e.to = flowEndpoint(nodes, e.points[len(e.points)-1])
		if e.from < 0 || e.to < 0 || e.from == e.to || incoming[e.to] {
			return svg
		}
		delta := nodes[e.to].box[1] - nodes[e.from].box[1]
		if delta == 0 || direction*delta < 0 {
			return svg
		}
		direction = math.Copysign(1, delta)
		incoming[e.to] = true
		nodes[e.to].span = math.Max(nodes[e.to].span, e.labelWidth)
		nodes[e.from].children = append(nodes[e.from].children, e.to)
	}
	root := -1
	for i := range nodes {
		if !incoming[i] {
			if root >= 0 {
				return svg
			}
			root = i
		}
		slices.SortFunc(nodes[i].children, func(a, b int) int {
			return cmp.Compare(nodes[a].box[0], nodes[b].box[0])
		})
	}
	if root < 0 {
		return svg
	}
	visited := 0
	bands := layoutFlowTree(nodes, root, &visited)
	if visited != len(nodes) {
		return svg
	}
	left, right := 0.0, 0.0
	for _, band := range bands {
		left, right = math.Min(left, band.left), math.Max(right, band.right)
	}
	placeFlowTree(nodes, root, -left)
	width := right - left + 24 // Render uses twelve pixels of outer padding.
	var document struct {
		Width  float64 `xml:"width,attr"`
		Height float64 `xml:"height,attr"`
	}
	if xml.Unmarshal(svg, &document) != nil || width >= document.Width {
		return svg
	}
	var out strings.Builder
	out.Grow(len(svg))
	header := string(svg[:edges[0].start])
	header = strings.Replace(header, `width="`+flowNumber(document.Width)+`"`, `width="`+flowNumber(width)+`"`, 1)
	header = strings.Replace(header, `viewBox="0 0 `+flowNumber(document.Width)+` `, `viewBox="0 0 `+flowNumber(width)+` `, 1)
	out.WriteString(header)
	for _, edge := range edges {
		from, to := nodes[edge.from], nodes[edge.to]
		sy, ty := from.box[1]+from.box[3], to.box[1]
		if direction < 0 {
			sy, ty = from.box[1], to.box[1]+to.box[3]
		}
		bandEnd := sy
		for _, node := range nodes {
			if math.Abs(node.box[1]+node.box[3]/2-from.box[1]-from.box[3]/2) < .1 {
				if direction > 0 {
					bandEnd = math.Max(bandEnd, node.box[1]+node.box[3])
				} else {
					bandEnd = math.Min(bandEnd, node.box[1])
				}
			}
		}
		elbow := bandEnd + direction*math.Min(8, math.Abs(ty-bandEnd)/3)
		path := roundedFlowPath(from.x, sy, to.x, ty, elbow)
		out.WriteString(strings.Replace(string(svg[edge.start:edge.end]), `d="`+edge.path+`"`, `d="`+path+`"`, 1))
		mid := flowMidpoint(edge.points)
		fmt.Fprintf(&out, `<g transform="translate(%.2f,%.2f)">`, to.x-mid.x, (elbow+ty)/2-direction*4-mid.y)
		out.Write(svg[edge.end:edge.tailEnd])
		out.WriteString("</g>")
	}
	for _, node := range nodes {
		fmt.Fprintf(&out, `<g transform="translate(%.2f,0)">`, node.x-node.box[0]-node.box[2]/2)
		out.Write(svg[node.start:node.end])
		out.WriteString("</g>")
	}
	out.Write(svg[nodes[len(nodes)-1].end:])
	return []byte(out.String())
}

func readFlowLayout(svg []byte) ([]flowNode, []flowEdge, bool) {
	var nodes []flowNode
	var edges []flowEdge
	decoder := xml.NewDecoder(bytes.NewReader(svg))
	depth := 0
	pending := flowNode{start: -1}
	for {
		start := decoder.InputOffset()
		token, err := decoder.Token()
		if err == io.EOF {
			return nodes, edges, true
		}
		if err != nil {
			return nil, nil, false
		}
		if end, ok := token.(xml.EndElement); ok && end.Name.Local == "g" {
			depth--
		}
		element, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if element.Name.Local == "g" {
			depth++
			if depth > 1 {
				return nil, nil, false
			}
		}
		if depth != 1 {
			continue
		}
		switch element.Name.Local {
		case "a", "circle":
			return nil, nil, false
		case "rect", "polygon":
			if box, _, ok := diagramNodeBox(element); ok {
				if len(edges) > 0 && edges[len(edges)-1].tailEnd == 0 {
					edges[len(edges)-1].tailEnd = start
				}
				pending = flowNode{box: box, span: box[2], start: start}
			} else if flowAttr(element, "stroke-dasharray") != "" {
				return nil, nil, false // A subgraph needs its own layout bounds.
			} else if len(edges) > 0 && len(nodes) == 0 && pending.start < 0 {
				width, err := strconv.ParseFloat(flowAttr(element, "width"), 64)
				if err != nil {
					return nil, nil, false
				}
				edges[len(edges)-1].labelWidth = width + 12 // Native badge padding and borders.
			}
		case "path":
			if flowAttr(element, "fill") != "none" {
				return nil, nil, false
			}
			if len(nodes) > 0 || pending.start >= 0 {
				return nil, nil, false
			}
			if len(edges) > 0 {
				edges[len(edges)-1].tailEnd = start
			}
			edge := flowEdge{start: start, path: flowAttr(element, "d")}
			for part := range strings.FieldsSeq(edge.path) {
				if len(part) == 0 || part[0] != 'M' && part[0] != 'L' {
					return nil, nil, false
				}
				x, y, ok := strings.Cut(part[1:], ",")
				px, ex := strconv.ParseFloat(x, 64)
				py, ey := strconv.ParseFloat(y, 64)
				if !ok || ex != nil || ey != nil {
					return nil, nil, false
				}
				edge.points = append(edge.points, flowPoint{px, py})
			}
			if len(edge.points) < 2 || decoder.Skip() != nil {
				return nil, nil, false
			}
			edge.end = decoder.InputOffset()
			edges = append(edges, edge)
		case "text":
			var label diagramText
			if decoder.DecodeElement(&label, &element) != nil {
				return nil, nil, false
			}
			if label.DY != 0 {
				continue
			}
			if pending.start < 0 {
				return nil, nil, false
			}
			pending.end = decoder.InputOffset()
			nodes = append(nodes, pending)
			pending.start = -1
		}
	}
}

func flowAttr(element xml.StartElement, name string) string {
	for _, attr := range element.Attr {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func flowEndpoint(nodes []flowNode, point flowPoint) int {
	for i, node := range nodes {
		b := node.box
		if point.x >= b[0]-.1 && point.x <= b[0]+b[2]+.1 && (math.Abs(point.y-b[1]) < .1 || math.Abs(point.y-b[1]-b[3]) < .1) {
			return i
		}
	}
	return -1
}

func layoutFlowTree(nodes []flowNode, index int, visited *int) []flowBand {
	*visited++
	node := &nodes[index]
	var bands []flowBand
	main, ties := -1, false
	for _, child := range node.children {
		contour := layoutFlowTree(nodes, child, visited)
		shift := 0.0
		if len(bands) > 0 {
			shift = math.Inf(-1)
			for level := range min(len(bands), len(contour)) {
				shift = math.Max(shift, bands[level].right+24-contour[level].left)
			}
		}
		nodes[child].x = shift
		for level, band := range contour {
			band.left, band.right = band.left+shift, band.right+shift
			if level == len(bands) {
				bands = append(bands, band)
			} else {
				bands[level].left = math.Min(bands[level].left, band.left)
				bands[level].right = math.Max(bands[level].right, band.right)
			}
		}
		depth := nodes[child].depth + 1
		if depth > node.depth {
			node.depth, main, ties = depth, child, false
		} else if depth == node.depth {
			ties = true
		}
	}
	center := 0.0
	if main >= 0 {
		center = nodes[main].x
		if ties {
			center = (nodes[node.children[0]].x + nodes[node.children[len(node.children)-1]].x) / 2
		}
	}
	for _, child := range node.children {
		nodes[child].x -= center
	}
	for i := range bands {
		bands[i].left -= center
		bands[i].right -= center
	}
	return append([]flowBand{{-node.span / 2, node.span / 2}}, bands...)
}

func placeFlowTree(nodes []flowNode, index int, offset float64) {
	nodes[index].x += offset
	for _, child := range nodes[index].children {
		placeFlowTree(nodes, child, nodes[index].x)
	}
}

func flowMidpoint(points []flowPoint) flowPoint {
	length := 0.0
	for i := 1; i < len(points); i++ {
		length += math.Hypot(points[i].x-points[i-1].x, points[i].y-points[i-1].y)
	}
	remaining := length / 2
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		segment := math.Hypot(b.x-a.x, b.y-a.y)
		if segment > 0 && remaining <= segment {
			return flowPoint{a.x + (b.x-a.x)*remaining/segment, a.y + (b.y-a.y)*remaining/segment}
		}
		remaining -= segment
	}
	return points[len(points)-1]
}

func roundedFlowPath(sx, sy, tx, ty, elbow float64) string {
	if math.Abs(sx-tx) < .01 {
		return fmt.Sprintf("M%.2f,%.2f L%.2f,%.2f", sx, sy, tx, ty)
	}
	r := math.Min(4, math.Abs(tx-sx)/2)
	dx, dy := math.Copysign(r, tx-sx), math.Copysign(r, ty-sy)
	return fmt.Sprintf("M%.2f,%.2f L%.2f,%.2f Q%.2f,%.2f %.2f,%.2f L%.2f,%.2f Q%.2f,%.2f %.2f,%.2f L%.2f,%.2f", sx, sy, sx, elbow-dy, sx, elbow, sx+dx, elbow, tx-dx, elbow, tx, elbow, tx, elbow+dy, tx, ty)
}

func flowNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
