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
	if !strings.Contains(reply.View.HTML, `data-pencode-searchable="" data-pencode-selectable="" data-ref="preview"`) {
		t.Fatal("preview did not enable rendered text selection and search")
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
			`<img src="images/diagram.png" alt="Local" title="Diagram" loading="lazy">`,
			`<img src="https://example.com/diagram.svg" alt="Remote" loading="lazy">`,
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

func TestMarkdownPreviewRendersHTMLImages(t *testing.T) {
	const source = `| Windows | macOS | Linux | Android | iOS | Web | WeChat Mini Games |
| :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| <img src="editor/resources/images/icons/windows.svg" width="32" alt=""> | <img src="editor/resources/images/icons/macOS.svg" width="32" alt=""> | <img src="editor/resources/images/icons/linux.svg" width="32" alt=""> | <img src="editor/resources/images/icons/android.svg" width="32" alt=""> | <img src="editor/resources/images/icons/ios.svg" width="32" alt=""> | <img src="editor/resources/images/icons/web.svg" width="32" alt=""> | <img src="editor/resources/images/icons/wechat.svg" width="32" alt=""> |

<IMG src="https://example.com/badge.svg?style=flat&amp;color=green" alt="&quot;Build&quot;" height="24" />
<img src="local%20badge.svg" alt="Local" onerror="alert(1)">

Inline <img src="javascript&#58;alert(1)"> <img src="file:///tmp/a.svg"> <img src="peb:badge">

<script>alert(1)</script>
`
	reply, err := markdownPreview(peb.EditorMessage{
		Editor: "markdown", Path: "/workspace/README.md", Content: source, Event: peb.EditorEvent{Kind: "open"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"windows", "macOS", "linux", "android", "ios", "web", "wechat"} {
		want := `<td style="text-align:center"><img src="/workspace/editor/resources/images/icons/` + name + `.svg" alt="" width="32" loading="lazy"></td>`
		if !strings.Contains(reply.View.HTML, want) {
			t.Errorf("missing %s table icon in %s", name, reply.View.HTML)
		}
	}
	for _, want := range []string{
		`<img src="https://example.com/badge.svg?style=flat&amp;color=green" alt="&#34;Build&#34;" height="24" loading="lazy">`,
		`<img src="/workspace/local%20badge.svg" alt="Local" loading="lazy">`,
	} {
		if !strings.Contains(reply.View.HTML, want) {
			t.Errorf("missing %s in %s", want, reply.View.HTML)
		}
	}
	if strings.Count(reply.View.HTML, "<img ") != 9 || strings.Contains(reply.View.HTML, "onerror") || strings.Contains(reply.View.HTML, "<script>") {
		t.Fatal(reply.View.HTML)
	}
}
