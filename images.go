package main

import (
	"bytes"
	"html"
	"io"
	"net/url"
	"slices"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	renderhtml "github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
	htmltoken "golang.org/x/net/html"
)

var imageBaseKey = renderer.NewContextKey()

func decorateMarkdownImage(next renderhtml.NodeRenderer) renderhtml.NodeRenderer {
	return renderhtml.NodeRendererFunc(func(w io.Writer, source []byte, node ast.Node, entering bool, ctx renderer.Context) (ast.WalkStatus, error) {
		if entering {
			image := node.(*ast.Image)
			image.SetAttribute("loading", text.NewMultiLineValue("lazy", nil))
			if base, ok := ctx.Get(imageBaseKey).(*url.URL); ok {
				if src, err := url.Parse(image.Destination.Value(source)); err == nil && src.Scheme == "" && src.Host == "" {
					image.Destination = text.NewSingleLineValue(base.ResolveReference(src).String(), nil)
				}
			}
		}
		return next.Render(w, source, node, entering, ctx)
	})
}

func decorateHTMLImages(next renderhtml.NodeRenderer) renderhtml.NodeRenderer {
	return renderhtml.NodeRendererFunc(func(w io.Writer, source []byte, node ast.Node, entering bool, ctx renderer.Context) (ast.WalkStatus, error) {
		if !entering {
			return next.Render(w, source, node, entering, ctx)
		}
		var raw string
		switch n := node.(type) {
		case *ast.RawHTML:
			raw = n.Value.Str(source)
		case *ast.HTMLBlock:
			if n.HTMLBlockKind == ast.HTMLBlockKind7 {
				raw = n.Value.Str(source)
			}
		}
		raw = strings.TrimLeft(raw, " \t\r\n")
		if len(raw) < 5 || !strings.EqualFold(raw[:4], "<img") {
			return next.Render(w, source, node, entering, ctx)
		}
		base, _ := ctx.Get(imageBaseKey).(*url.URL)
		z := htmltoken.NewTokenizer(strings.NewReader(raw))
		for {
			switch z.Next() {
			case htmltoken.TextToken:
				if len(bytes.TrimSpace(z.Raw())) == 0 {
					continue
				}
			case htmltoken.StartTagToken, htmltoken.SelfClosingTagToken:
				name, more := z.TagName()
				if string(name) == "img" {
					writeHTMLImage(w.(util.BufWriter), z, more, base)
					continue
				}
			}
			return ast.WalkSkipChildren, nil
		}
	})
}

func writeHTMLImage(w util.BufWriter, z *htmltoken.Tokenizer, more bool, base *url.URL) {
	names := [...]string{"src", "alt", "title", "width", "height"}
	var values [len(names)][]byte
	var seen [len(names)]bool
	for more {
		name, value, hasMore := z.TagAttr()
		more = hasMore
		if i := slices.Index(names[:], string(name)); i >= 0 && !seen[i] {
			values[i] = value
			seen[i] = true
		}
	}
	src := strings.TrimSpace(util.BytesToReadOnlyString(values[0]))
	u, err := url.Parse(src)
	if err != nil || src == "" {
		return
	}
	switch u.Scheme {
	case "":
		if base != nil && u.Host == "" {
			src = base.ResolveReference(u).String()
		}
	case "http", "https":
	default:
		return
	}
	_, _ = w.WriteString(`<img src="`)
	_, _ = w.WriteString(html.EscapeString(src))
	_ = w.WriteByte('"')
	for i, name := range names[1:] {
		if seen[i+1] {
			_ = w.WriteByte(' ')
			_, _ = w.WriteString(name)
			_, _ = w.WriteString(`="`)
			_, _ = w.WriteString(html.EscapeString(util.BytesToReadOnlyString(values[i+1])))
			_ = w.WriteByte('"')
		}
	}
	_, _ = w.WriteString(` loading="lazy">`)
}
