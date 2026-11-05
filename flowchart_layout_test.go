package main

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	mermaid "github.com/zkrebbekx/go-mermaid"
	"golang.org/x/net/html"
)

func TestFlowchartMainPathDoesNotDriftWithMoreBranches(t *testing.T) {
	for _, direction := range []string{"TD", "BT"} {
		for _, count := range []int{2, 6} {
			var source strings.Builder
			fmt.Fprintf(&source, "flowchart %s\n", direction)
			for i := range count {
				fmt.Fprintf(&source, "N%d{检查条件} -->|否| R%d[返回错误]\nN%d -->|是| N%d\n", i, i, i, i+1)
			}
			fmt.Fprintf(&source, "N%d[完成]\n", count)
			markup, err := renderFlowchart(source.String())
			if err != nil {
				t.Fatal(err)
			}
			diagram, nodes, labels := flowHTMLBoxes(t, markup)
			if diagram[2] > 300 {
				t.Fatalf("%s: %d branches still drift horizontally: width %.2f", direction, count, diagram[2])
			}
			center := nodes[0][0] + nodes[0][2]/2
			for i, node := range nodes {
				if i%2 == 0 && i < count*2 && math.Abs(node[0]+node[2]/2-center) > .03 {
					t.Fatalf("%s: main node %d drifted off the vertical path: %+v", direction, i, node)
				}
				if node[0] < 0 || node[0]+node[2] > diagram[2]+.03 {
					t.Fatalf("node outside compact canvas: %+v", node)
				}
				for _, other := range nodes[:i] {
					if flowBoxesOverlap(node, other) {
						t.Fatalf("nodes overlap: %+v %+v", node, other)
					}
				}
			}
			for i, label := range labels {
				target := nodes[i+1]
				if math.Abs(label[0]-target[0]-target[2]/2) > .03 {
					t.Fatalf("%s: branch label %d is not above its target: %+v %+v", direction, i, label, target)
				}
				// Allow twelve pixels for the native badge's half height.
				if direction == "TD" && label[1]+12 > target[1]-8 || direction == "BT" && label[1]-12 < target[1]+target[3]+8 {
					t.Fatalf("branch badge covers the arrowhead: %+v %+v", label, target)
				}
			}
		}
	}
}

func TestFlowchartTreePackingKeepsBranchesSeparate(t *testing.T) {
	for _, source := range []string{
		"flowchart TD; A{开始} --> B{左}; A --> C{右}; B --> D[左一]; B --> E[左二]; C --> F[右一]; C --> G[右二]",
		"flowchart TD; A{开始} -->|需要人工复核的特殊情况| B[一]; A -->|否| C[二]; A -->|是| D[三]; C --> E[这是一个需要自动换行的很长的节点标签]; D --> F[完成]",
	} {
		markup, err := renderFlowchart(source)
		if err != nil {
			t.Fatal(err)
		}
		diagram, nodes, labels := flowHTMLBoxes(t, markup)
		for i, node := range nodes {
			if node[0] < 0 || node[0]+node[2] > diagram[2]+.03 {
				t.Fatal("node outside compact canvas", node)
			}
			for _, other := range nodes[:i] {
				if flowBoxesOverlap(node, other) {
					t.Fatal("packed branches overlap", node, other)
				}
			}
		}
		for _, label := range labels {
			if label[0] < 12 || label[0] > diagram[2]-12 {
				t.Fatal("label clipped by compact canvas", label)
			}
		}
	}
}

func TestFlowchartOtherGraphLayoutsStayIntact(t *testing.T) {
	for _, source := range []string{
		"flowchart TD; A --> B; A --> C; B --> D; C --> D",
		"flowchart TD; A --> B; B --> C; C --> A",
		"flowchart TD; A --> B; C --> D",
		"flowchart LR; A --> B; A --> C",
		"flowchart RL; A --> B; A --> C",
		"flowchart TD\n subgraph Group\n A --> B\n end",
		"flowchart TD; A((开始)) --> B; A --> C",
		"flowchart TD; A --> B; A --> C\n style B fill:#ff0000",
	} {
		svg, err := mermaid.Render(source, flowchartTheme, mermaid.WithTransparentBackground(), mermaid.WithSpacing(24, 42), mermaid.WithPadding(12))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(svg, compactFlowchartLayout(svg)) {
			t.Fatalf("changed layout outside vertical tree scope: %s", source)
		}
	}
}

func flowHTMLBoxes(t *testing.T, markup string) (diagram [4]float64, nodes, labels [][4]float64) {
	t.Helper()
	root, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		class, inline := "", ""
		for _, attr := range node.Attr {
			if attr.Key == "class" {
				class = attr.Val
			}
			if attr.Key == "style" {
				inline = attr.Val
			}
		}
		var box [4]float64
		for property := range strings.SplitSeq(inline, ";") {
			name, value, _ := strings.Cut(property, ":")
			index := slices.Index([]string{"left", "top", "width", "height"}, name)
			if index >= 0 {
				box[index], err = strconv.ParseFloat(strings.TrimSuffix(value, "px"), 64)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		switch {
		case class == "diagram":
			diagram = box
		case strings.HasPrefix(class, "diagram-node"):
			nodes = append(nodes, box)
		case strings.Contains(class, "diagram-edge-label"):
			labels = append(labels, box)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return
}

func flowBoxesOverlap(a, b [4]float64) bool {
	return a[0] < b[0]+b[2] && b[0] < a[0]+a[2] && a[1] < b[1]+b[3] && b[1] < a[1]+a[3]
}
