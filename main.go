// Markdown preview is an Open With action; the host owns source editing and layout.
package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/onix-engine/onix/html/markup"
	"github.com/pencode-labs/pencode/peb"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

//go:embed resources/editor.css
var editorCSS string

var markdownParser = parser.New(parser.WithExtensions(extension.TableParser, extension.NewLinkifyParser()))
var codePreview = codeRenderer{}
var markdownRenderer = html.New(html.WithExtensions(extension.TableHTMLRenderer, &codePreview))

func markdownPreview(message peb.EditorMessage) (peb.EditorReply, error) {
	if message.Editor != "markdown" || message.Binary {
		return peb.EditorReply{}, fmt.Errorf("unsupported document")
	}
	if message.Event.Kind != "open" && message.Event.Kind != "documentchange" {
		return peb.EditorReply{}, nil
	}
	var preview bytes.Buffer
	document := markdownParser.ParseStringSource(message.Content)
	base := url.URL{Path: "/" + strings.TrimPrefix(strings.ReplaceAll(message.Path, "\\", "/"), "/")}
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if image, ok := node.(*ast.Image); ok && entering {
			image.SetAttribute("loading", text.NewMultiLineValue("lazy", nil))
			if message.Path != "" {
				if source, err := url.Parse(image.Destination.Value(util.StringToReadOnlyBytes(message.Content))); err == nil && source.Scheme == "" && source.Host == "" {
					image.Destination = text.NewSingleLineValue(base.ResolveReference(source).String(), nil)
				}
			}
		}
		return ast.WalkContinue, nil
	})
	if err := markdownRenderer.RenderStringSource(&preview, message.Content, document); err != nil {
		return peb.EditorReply{}, err
	}
	// Goldmark's default renderer omits raw HTML from the Markdown preview.
	var b markup.Builder
	b.Open("article", "class", "markdown-preview", "data-ref", "preview", "data-pencode-selectable", "", "data-pencode-searchable", "", "aria-label", "Markdown 预览")
	b.Embed(preview.String())
	b.End()
	return peb.EditorReply{View: &peb.EditorView{HTML: b.String(), CSS: editorCSS}}, nil
}

func main() {
	err := peb.ServeEditors(os.Stdin, os.Stdout, peb.Contributions{Actions: []peb.Action{
		{ID: "markdown", Text: "Markdown 预览", Tooltip: "Markdown 预览", Icon: "resources/preview.svg", Location: peb.OpenWith, Selectors: []peb.DocumentSelector{{Match: peb.FileSuffix, Pattern: ".md"}, {Match: peb.FileSuffix, Pattern: ".markdown"}}},
	}}, markdownPreview)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
