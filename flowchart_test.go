package main

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"image/png"
	"strconv"
	"strings"
	"testing"

	mermaid "github.com/zkrebbekx/go-mermaid"
)

func TestFlowchartDecisionHasRasterizedDashes(t *testing.T) {
	markup, err := renderFlowchart("flowchart TD; A{功能已开启}")
	if err != nil {
		t.Fatal(err)
	}
	_, encoded, _ := strings.Cut(markup, "data:image/png;base64,")
	encoded, _, _ = strings.Cut(encoded, `"`)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// Labels and native surfaces are absent here. The top of the outline must
	// contain both painted dashes and transparent gaps, even without CSS borders.
	for y := range img.Bounds().Dy() {
		painted, gaps := 0, 0
		for x := img.Bounds().Dx() / 3; x < img.Bounds().Dx()*2/3; x++ {
			if _, _, _, alpha := img.At(x, y).RGBA(); alpha != 0 {
				painted++
			} else {
				gaps++
			}
		}
		if painted > 0 {
			if gaps == 0 {
				t.Fatal("decision outline has no gaps")
			}
			return
		}
	}
	t.Fatal("decision outline is missing")
}

func TestFlowchartCompactBranches(t *testing.T) {
	const source = `flowchart TD
 A[接收阵容 ID] --> B{功能已开启}
 B -->|否| C[框架返回 sys_notOpen]
 B -->|是| D[TeamRecommendConf getByKey 读取配置]
 D --> E{目标配置结构有效}
 E -->|否| F[返回阵容配置错误]
 E -->|是| G{claimedIds 已包含该阵容}
 G -->|是| H[返回重复领取错误]
 G -->|否| I[发放阵容奖励并保存领取记录]`
	var original struct {
		Width float64 `xml:"width,attr"`
	}
	svg, err := mermaid.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal(svg, &original); err != nil {
		t.Fatal(err)
	}
	markup, err := renderFlowchart(source)
	if err != nil {
		t.Fatal(err)
	}
	_, size, _ := strings.Cut(markup, `style="width:`)
	size, _, _ = strings.Cut(size, "px")
	width, err := strconv.ParseFloat(size, 64)
	if err != nil {
		t.Fatal(err)
	}
	if width >= original.Width*.8 {
		t.Fatalf("branches remain too wide: %.2fpx, originally %.2fpx", width, original.Width)
	}
	if got := strings.Count(markup, `class="diagram-node diagram-decision"`); got != 3 {
		t.Fatalf("expected 3 decision nodes, got %d", got)
	}
	if !strings.Contains(markup, ">TeamRecommendConf</span>") || !strings.Contains(markup, ">getByKey 读取配置</span>") {
		t.Fatal("long label did not wrap at the word boundary")
	}
	t.Logf("branch canvas: %.2fpx -> %.2fpx", original.Width, width)
}

func TestFlowchartWrappingPreservesSyntax(t *testing.T) {
	for _, source := range []string{
		`flowchart TD; A["保留括号 (参数) 和很长很长的中文标签内容"] --> B[完成]`,
		`flowchart LR; A([TeamRecommendConf getByKey 读取配置]) --> B((完成))`,
		`flowchart RL; A{{这里是一个需要换行的非常长的条件节点标签}} --> B[[完成]]`,
		`flowchart BT; A["第一行<br>第二行<br />第三行<br/><br/>第五行\n第六行"] --> B[完成]`,
	} {
		wrapped := compactFlowchart(source)
		if _, err := mermaid.Render(wrapped); err != nil {
			t.Fatalf("wrapping broke syntax: %v\n%s", err, wrapped)
		}
		// Wrapping changes presentation, never label characters or node syntax.
		normalize := func(text string) string {
			return strings.Join(strings.Fields(flowchartLineBreaks.Replace(text)), "")
		}
		if normalize(source) != normalize(wrapped) {
			t.Fatalf("wrapping changed content:\n%s\n%s", source, wrapped)
		}
	}
}

func TestFlowchartFragmentCacheWarmPath(t *testing.T) {
	var renderer codeRenderer
	key := codeKey{source: "flowchart TD; A[开始] --> B{通过？}; B -->|是| C[完成]", language: "mermaid"}
	markup := renderer.fragment(key)
	if strings.Contains(markup, `class="diagram-error"`) {
		t.Fatal(markup)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if renderer.fragment(key) != markup {
			t.Fatal("cache changed markup")
		}
	}); allocs != 0 {
		t.Fatalf("cached diagram allocated: %v", allocs)
	}
}
