package main

import (
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/pencode-labs/pencode/peb"
	"golang.org/x/net/html"
)

func TestCodeHighlightMergesAdjacentStyles(t *testing.T) {
	const code = `value = "hello\nworld" + "<>&"`
	var output strings.Builder
	writeCode(&output, codeKey{source: code, language: "python"})
	markup := output.String()
	root, err := html.Parse(strings.NewReader(markup))
	if err != nil || nodeText(findElement(root, "code")) != code {
		t.Fatal("merging styles changed code text", err, markup)
	}
	iterator, err := lexers.Get("python").Tokenise(nil, code)
	if err != nil {
		t.Fatal(err)
	}
	previous, styledTokens, runs := "", 0, 0
	for token := iterator(); token != chroma.EOF; token = iterator() {
		if token.Value == "" {
			continue
		}
		class := tokenClass(token.Type)
		if class != "" {
			styledTokens++
			if previous != class {
				runs++
			}
		}
		previous = class
	}
	if got := strings.Count(markup, "<span"); got != runs || got >= styledTokens {
		t.Fatalf("styles were not merged: %d spans, %d runs, %d styled tokens", got, runs, styledTokens)
	}
}

func TestCodeHighlightPreservesContent(t *testing.T) {
	for _, test := range []struct {
		name, markdown, code string
		inline               bool
	}{
		{"single backticks", "`return 42`", "return 42", true},
		{"inline call", "`fmt.Println(value)`", "fmt.Println(value)", true},
		{"inline spacing", "`return  \t42`", "return  \t42", true},
		{"double backticks", "`` const x = `value` ``", "const x = `value`", true},
		{"multiline inline", "`return\n 42`", "return 42", true},
		{"go", "```go\nvar x = \"<script>\" // comment\n```", "var x = \"<script>\" // comment\n", false},
		{"javascript", "```js\nconst x = 42;\n```", "const x = 42;\n", false},
		{"python", "~~~python\nreturn 42 # comment\n~~~", "return 42 # comment\n", false},
		{"indented", "    return 42\n", "return 42\n", false},
		{"unlabelled", "```\nreturn 42\n```", "return 42\n", false},
		{"unknown language", "```unknown-lang\nreturn 42\n```", "return 42\n", false},
		{"block whitespace", "```go\n\nfunc main() {\n\tif true {\n\t\treturn  // comment  \n\n\t}\n}\n\n```", "\nfunc main() {\n\tif true {\n\t\treturn  // comment  \n\n\t}\n}\n\n", false},
		{"indented whitespace", "    if true {\n        return  42  \n\n    }\n", "if true {\n    return  42  \n\n}\n", false},
		{"literal escapes", "`` <img src=x> &amp; \\n ``", "<img src=x> &amp; \\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reply, err := markdownPreview(peb.EditorMessage{Editor: "markdown", Content: test.markdown, Event: peb.EditorEvent{Kind: "open"}})
			if err != nil {
				t.Fatal(err)
			}
			root, err := html.Parse(strings.NewReader(reply.View.HTML))
			if err != nil {
				t.Fatal(err)
			}
			code := findElement(root, "code")
			if code == nil || nodeText(code) != test.code {
				t.Fatalf("code text changed: %s", reply.View.HTML)
			}
			if !strings.Contains(reply.View.HTML, `class="highlight`) || strings.Contains(reply.View.HTML, "<script>") || strings.Contains(reply.View.HTML, "<img") {
				t.Fatal(reply.View.HTML)
			}
			if !strings.Contains(test.name, "literal") && !strings.Contains(reply.View.HTML, `class="tok-`) {
				t.Fatal("code has no syntax tokens", reply.View.HTML)
			}
			if (findElement(root, "pre") == nil) != test.inline {
				t.Fatal("inline/block code layout changed", reply.View.HTML)
			}
		})
	}
}

func TestFlowchartRendersGeometryAndNativeLabels(t *testing.T) {
	for _, source := range []string{
		"flowchart TD\n A[开始] --> B{通过？}\n B -->|是| C([完成])\n B -->|否| A",
		"%% Comment\ngraph\tLR; A[开始] --> B[完成]",
		"flowchart BT\n subgraph 中文分组\n A[开始] --> B[完成]\n end",
		"flowchart RL\n A[\"开始<br/>下一行\"] --> B[完成]",
	} {
		reply, err := markdownPreview(peb.EditorMessage{Editor: "markdown", Content: "# Diagram\n\n```mermaid\n" + source + "\n```\n\nAfter", Event: peb.EditorEvent{Kind: "documentchange"}})
		if err != nil {
			t.Fatal(err)
		}
		markup := reply.View.HTML
		for _, want := range []string{`class="mermaid"`, `class="diagram-label"`, "开始", "完成", "<p>After</p>"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("missing %q: %s", want, markup)
			}
		}
		_, encoded, ok := strings.Cut(markup, "data:image/png;base64,")
		if !ok {
			t.Fatal("diagram has no image", markup)
		}
		encoded, _, _ = strings.Cut(encoded, `"`)
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		image, err := png.Decode(strings.NewReader(string(data)))
		if err != nil || image.Bounds().Dx() == 0 || image.Bounds().Dy() == 0 {
			t.Fatal("invalid diagram image", err)
		}
		visible := false
		for y := 0; y < image.Bounds().Dy() && !visible; y++ {
			for x := 0; x < image.Bounds().Dx(); x++ {
				if _, _, _, alpha := image.At(x, y).RGBA(); alpha != 0 {
					visible = true
					break
				}
			}
		}
		if !visible {
			t.Fatal("diagram geometry is blank")
		}
	}
}

func TestInvalidFlowchartKeepsSourceAndRestOfDocument(t *testing.T) {
	for _, source := range []string{"flowchart TD\n A[", "unsupported diagram", "flowchart TD\n" + strings.Repeat(" ", 32<<10)} {
		reply, err := markdownPreview(peb.EditorMessage{Editor: "markdown", Content: "```mermaid\n" + source + "\n```\n\nStill visible", Event: peb.EditorEvent{Kind: "open"}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(reply.View.HTML, `class="diagram-error"`) || !strings.Contains(reply.View.HTML, "Still visible") || !strings.Contains(reply.View.HTML, "<pre>") {
			t.Fatal("invalid diagram hid the document", reply.View.HTML)
		}
	}
}

func TestCodeFragmentCacheWarmPath(t *testing.T) {
	var renderer codeRenderer
	key := codeKey{source: "return 42\n", language: "go"}
	markup := renderer.fragment(key)
	if allocs := testing.AllocsPerRun(100, func() {
		if renderer.fragment(key) != markup {
			t.Fatal("cache changed markup")
		}
	}); allocs != 0 {
		t.Fatalf("cached code allocated: %v", allocs)
	}
	large := codeKey{source: strings.Repeat(" ", 1<<20)}
	renderer.fragment(large)
	if renderer.bytes > 1<<20 || len(renderer.cache) != 1 {
		t.Fatal("oversized code retained in cache")
	}
}

func findElement(node *html.Node, tag string) *html.Node {
	if node.Type == html.ElementNode && node.Data == tag {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func nodeText(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	var out strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		out.WriteString(nodeText(child))
	}
	return out.String()
}
