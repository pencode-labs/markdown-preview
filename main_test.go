package main

import (
	"strings"
	"testing"

	"github.com/pencode-labs/pencode/peb"
)

func TestMarkdownPreviewIsReadOnlyAndUpdatesWithDocument(t *testing.T) {
	message := peb.EditorMessage{Editor: "markdown", Content: "# Hello\n</textarea><script>bad()</script>", Event: peb.EditorEvent{Kind: "open"}}
	reply, err := markdownPreview(message)
	if err != nil || reply.View == nil {
		t.Fatalf("open: %+v %v", reply, err)
	}
	if !strings.Contains(reply.View.HTML, "<h1>Hello</h1>") || strings.Contains(reply.View.HTML, "<script>") {
		t.Fatal("preview or escaping failed")
	}
	if !strings.Contains(reply.View.HTML, `data-ref="preview" data-pencode-searchable`) {
		t.Fatal("preview did not enable rendered text search")
	}
	message.Event = peb.EditorEvent{Kind: "input", Ref: "source", Value: ""}
	reply, err = markdownPreview(message)
	if err != nil || reply.View != nil || reply.Content != nil {
		t.Fatal("preview accepted a document edit")
	}
	message.Content = "# Updated"
	message.Event = peb.EditorEvent{Kind: "documentchange"}
	reply, err = markdownPreview(message)
	if err != nil || reply.View == nil || reply.Content != nil || len(reply.View.Events) != 0 || strings.Contains(reply.View.HTML, "<textarea") || !strings.Contains(reply.View.HTML, "<h1>Updated</h1>") {
		t.Fatalf("preview did not follow host content: %+v %v", reply, err)
	}
}

func TestMarkdownPreviewRendersImagesLinksAndTables(t *testing.T) {
	const source = `![Local](images/diagram.png "Diagram")
![Remote](https://example.com/diagram.svg)
[Guide](docs/guide.md)
[Site][site]
https://example.com

[site]: https://example.com "Site"

| Feature | Status |
| :--- | ---: |
| **Images** | [Ready](docs/guide.md) |
`
	for _, kind := range []string{"open", "documentchange"} {
		reply, err := markdownPreview(peb.EditorMessage{Editor: "markdown", Content: source, Event: peb.EditorEvent{Kind: kind}})
		if err != nil || reply.View == nil {
			t.Fatalf("%s: %+v %v", kind, reply, err)
		}
		for _, want := range []string{
			`<img src="images/diagram.png" alt="Local" title="Diagram">`,
			`<img src="https://example.com/diagram.svg" alt="Remote">`,
			`<a href="docs/guide.md">Guide</a>`,
			`<a href="https://example.com" title="Site">Site</a>`,
			`<a href="https://example.com">https://example.com</a>`,
			`<table>`, `<thead>`, `<tbody>`, `<th style="text-align:left">Feature</th>`, `<td style="text-align:right">`, `<strong>Images</strong>`,
		} {
			if !strings.Contains(reply.View.HTML, want) {
				t.Errorf("%s: missing %q in %s", kind, want, reply.View.HTML)
			}
		}
	}
}

func TestMarkdownPreviewOmitsUnsafeHTMLAndURLs(t *testing.T) {
	reply, err := markdownPreview(peb.EditorMessage{Editor: "markdown", Content: `[Bad](javascript:alert%281%29)
![Bad](javascript:alert%281%29)
<script>alert(1)</script>`, Event: peb.EditorEvent{Kind: "open"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reply.View.HTML, "javascript:") || strings.Contains(reply.View.HTML, "<script>") {
		t.Fatal(reply.View.HTML)
	}
}

func TestMarkdownPreviewResolvesDocumentImages(t *testing.T) {
	for _, filename := range []string{"/workspace/docs/README.md", `C:\workspace\docs\README.md`} {
		reply, err := markdownPreview(peb.EditorMessage{Editor: "markdown", Path: filename, Content: "![Image](../resources/local%20image.svg)\n[Guide](guide.md)", Event: peb.EditorEvent{Kind: "open"}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(reply.View.HTML, "/workspace/resources/local%20image.svg") || strings.Contains(reply.View.HTML, `src="../`) || !strings.Contains(reply.View.HTML, `<a href="guide.md">Guide</a>`) {
			t.Fatal(reply.View.HTML)
		}
	}
}
