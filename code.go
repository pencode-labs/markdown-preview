package main

import (
	"html"
	"io"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	renderhtml "github.com/yuin/goldmark/v2/renderer/html"
)

// Unlabelled snippets often lack enough context for language detection. These
// shared lexical rules still distinguish literals, comments and common keywords.
var snippetLexer = chroma.MustNewLexer(&chroma.Config{Name: "snippet"}, func() chroma.Rules {
	return chroma.Rules{"root": {
		{Pattern: `\s+`, Type: chroma.Text},
		{Pattern: `//[^\n]*|\#[^\n]*|/\*(?s:.*?)\*/`, Type: chroma.Comment},
		{Pattern: "\"(?:\\\\.|[^\"\\\\])*\"|'(?:\\\\.|[^'\\\\])*'|`[^`]*`", Type: chroma.LiteralString},
		{Pattern: `\b(?:0[xX][\da-fA-F]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)\b`, Type: chroma.LiteralNumber},
		{Pattern: `\b(?:as|async|await|break|case|catch|class|const|continue|def|defer|delete|do|else|enum|export|extends|false|finally|fn|for|from|func|function|go|if|impl|import|in|interface|let|match|new|nil|null|package|private|public|raise|range|return|select|static|struct|switch|this|throw|true|try|type|typeof|use|var|void|while|with|yield)\b`, Type: chroma.Keyword},
		{Pattern: `[\p{L}_$][\p{L}\p{N}_$]*(?=\s*\()`, Type: chroma.NameFunction},
		{Pattern: `[\p{L}_$][\p{L}\p{N}_$]*`, Type: chroma.Name},
		{Pattern: `[-+*/%=!<>|&^~?:]+`, Type: chroma.Operator},
		{Pattern: `.`, Type: chroma.Text},
	}}
})

type codeKey struct {
	source, language string
	inline           bool
}

type codeRenderer struct {
	cache map[codeKey]string
	bytes int
}

func (r *codeRenderer) RendererOptions(_ *renderhtml.Config) []renderhtml.Option {
	return []renderhtml.Option{renderhtml.WithNodeRenderers(map[ast.NodeKind]renderhtml.NodeRenderer{
		ast.KindCodeBlock: renderhtml.NodeRendererFunc(r.render),
		ast.KindCodeSpan:  renderhtml.NodeRendererFunc(r.render),
	})}
}

func (r *codeRenderer) render(w io.Writer, source []byte, node ast.Node, entering bool, _ renderer.Context) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	key := codeKey{inline: node.Kind() == ast.KindCodeSpan}
	if key.inline {
		key.source = node.(*ast.CodeSpan).Value.Value(source)
	} else {
		block := node.(*ast.CodeBlock)
		key.source = block.Value.Str(source)
		key.language, _ = block.Language(source)
		key.language = strings.ToLower(key.language)
	}
	_, err := io.WriteString(w, r.fragment(key))
	return ast.WalkSkipChildren, err
}

func (r *codeRenderer) fragment(key codeKey) string {
	if cached, ok := r.cache[key]; ok {
		return cached
	}
	var out strings.Builder
	if !key.inline && key.language == "mermaid" {
		if diagram, err := renderFlowchart(key.source); err == nil {
			out.WriteString(diagram)
		} else {
			out.WriteString(`<p class="diagram-error" role="status">流程图渲染失败：`)
			out.WriteString(html.EscapeString(err.Error()))
			out.WriteString("</p>\n")
			writeCode(&out, key)
		}
	} else {
		writeCode(&out, key)
	}
	markup := out.String()
	// Bound retained source and markup; editing prose reuses unchanged blocks.
	const limit = 1 << 20
	size := len(key.source) + len(key.language) + len(markup)
	if size <= limit {
		if r.bytes+size > limit || len(r.cache) >= 128 {
			clear(r.cache)
			r.bytes = 0
		}
		if r.cache == nil {
			r.cache = make(map[codeKey]string)
		}
		key.source, key.language = strings.Clone(key.source), strings.Clone(key.language)
		r.cache[key] = markup
		r.bytes += size
	}
	return markup
}

func writeCode(out *strings.Builder, key codeKey) {
	if !key.inline {
		out.WriteString("<pre>")
	}
	out.WriteString(`<code class="highlight`)
	if key.language != "" {
		out.WriteString(" language-")
		out.WriteString(html.EscapeString(key.language))
	}
	out.WriteString(`">`)
	lexer := lexers.Get(key.language)
	if key.language == "" {
		lexer = lexers.Analyse(key.source)
	}
	if lexer == nil {
		lexer = snippetLexer
	}
	iterator, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root", Nested: true}, key.source)
	if err != nil {
		out.WriteString(html.EscapeString(key.source))
	} else {
		active := ""
		for token := iterator(); token != chroma.EOF; token = iterator() {
			if token.Value == "" {
				continue
			}
			class := tokenClass(token.Type)
			if class != active {
				if active != "" {
					out.WriteString("</span>")
				}
				if class != "" {
					out.WriteString(`<span class="`)
					out.WriteString(class)
					out.WriteString(`">`)
				}
				active = class
			}
			out.WriteString(html.EscapeString(token.Value))
		}
		if active != "" {
			out.WriteString("</span>")
		}
	}
	out.WriteString("</code>")
	if !key.inline {
		out.WriteString("</pre>\n")
	}
}

func tokenClass(token chroma.TokenType) string {
	switch token.Category() {
	case chroma.Keyword:
		return "tok-keyword"
	case chroma.Literal:
		if token.SubCategory() == chroma.LiteralNumber {
			return "tok-number"
		}
		return "tok-string"
	case chroma.Comment:
		return "tok-comment"
	case chroma.Operator:
		return "tok-operator"
	case chroma.Name:
		switch token {
		case chroma.NameClass, chroma.NameBuiltin, chroma.NameException:
			return "tok-type"
		case chroma.NameFunction, chroma.NameFunctionMagic:
			return "tok-function"
		case chroma.NameTag, chroma.NameAttribute:
			return "tok-keyword"
		}
	}
	return ""
}
