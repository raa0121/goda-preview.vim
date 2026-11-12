package render

import (
	"bytes"
	"sort"
	"strconv"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/raa0121/goda-preview.vim/internal/plantuml"
)

func renderMarkdown(source string, opts Options) (Result, error) {
	ropts := []renderer.Option{
		// Ahead of goldmark's own code block renderer (priority 1000), which
		// neither writes attributes nor knows mermaid or PlantUML.
		renderer.WithNodeRenderers(util.Prioritized(&codeBlockRenderer{plantUMLServer: opts.PlantUMLServer, highlight: opts.Highlight}, 100)),
	}
	if opts.HardWraps {
		ropts = append(ropts, html.WithHardWraps())
	}
	if opts.Unsafe {
		ropts = append(ropts, html.WithUnsafe())
	}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote, extension.DefinitionList, extension.CJK, mathExtension{}),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(sourceLines{}, 100)),
		),
		goldmark.WithRendererOptions(ropts...),
	)
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return Result{}, err
	}
	return Result{HTML: buf.String(), Lines: true}, nil
}

// sourceLines puts data-line on every block, for the scroll sync. goldmark's
// renderer writes data-* attributes whatever its attribute filters say.
type sourceLines struct{}

func (sourceLines) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	starts := lineStarts(reader.Source())
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Type() != ast.TypeBlock || n.Kind() == ast.KindDocument {
			return ast.WalkContinue, nil
		}
		if off, ok := blockOffset(n); ok {
			n.SetAttributeString("data-line", strconv.Itoa(lineAt(starts, off)))
		}
		return ast.WalkContinue, nil
	})
}

// blockOffset is the byte offset a block starts at. Containers (lists, block
// quotes) have no lines of their own, so their first block child stands in.
func blockOffset(n ast.Node) (int, bool) {
	// The lines of a fenced block are its content, a line below the fence.
	if f, ok := n.(*ast.FencedCodeBlock); ok && f.Info != nil {
		return f.Info.Segment.Start, true
	}
	// Likewise the TeX of a $$ block; Pos is its opening $$.
	if m, ok := n.(*mathBlock); ok && m.Pos() >= 0 {
		return m.Pos(), true
	}
	if lines := n.Lines(); lines != nil && lines.Len() > 0 {
		return lines.At(0).Start, true
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Type() != ast.TypeBlock {
			continue
		}
		if off, ok := blockOffset(c); ok {
			return off, true
		}
	}
	return 0, false
}

func lineStarts(source []byte) []int {
	starts := []int{0}
	for i, b := range source {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func lineAt(starts []int, off int) int {
	return sort.Search(len(starts), func(i int) bool { return starts[i] > off })
}

type codeBlockRenderer struct {
	plantUMLServer string
	highlight      bool
}

func (r *codeBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.renderFenced)
	reg.Register(ast.KindCodeBlock, r.renderIndented)
}

func (r *codeBlockRenderer) renderFenced(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock)
	code := blockText(n, source)
	switch lang := n.Language(source); {
	case string(lang) == "mermaid":
		// Escaped, and mermaid reads it back as textContent.
		writeOpenTag(w, n, "pre", ` class="mermaid"`)
		_, _ = w.Write(util.EscapeHTML(code))
		_, _ = w.WriteString("</pre>\n")
	case string(lang) == "plantuml" && r.plantUMLServer != "":
		writeOpenTag(w, n, "p", ` class="plantuml"`)
		_, _ = w.WriteString(`<img alt="PlantUML diagram" src="`)
		_, _ = w.Write(util.EscapeHTML([]byte(plantuml.URL(r.plantUMLServer, string(code)))))
		_, _ = w.WriteString("\"></p>\n")
	case string(lang) == "math" || string(lang) == "katex":
		writeDisplayMath(w, n, code)
	default:
		r.writeCode(w, n, lang, code)
	}
	return ast.WalkContinue, nil
}

func (r *codeBlockRenderer) renderIndented(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.writeCode(w, node, nil, blockText(node, source))
	}
	return ast.WalkContinue, nil
}

func (r *codeBlockRenderer) writeCode(w util.BufWriter, n ast.Node, lang, code []byte) {
	var colored []byte
	ok := false
	if r.highlight && len(lang) > 0 {
		colored, ok = highlight(string(lang), code)
	}
	if ok {
		writeOpenTag(w, n, "pre", ` class="chroma"`)
	} else {
		writeOpenTag(w, n, "pre", "")
	}
	_, _ = w.WriteString("<code")
	if len(lang) > 0 {
		_, _ = w.WriteString(` class="language-`)
		_, _ = w.Write(util.EscapeHTML(lang))
		_ = w.WriteByte('"')
	}
	_ = w.WriteByte('>')
	if ok {
		_, _ = w.Write(colored)
	} else {
		_, _ = w.Write(util.EscapeHTML(code))
	}
	_, _ = w.WriteString("</code></pre>\n")
}

func writeOpenTag(w util.BufWriter, n ast.Node, tag, attrs string) {
	_, _ = w.WriteString("<" + tag + attrs)
	html.RenderAttributes(w, n, nil)
	_ = w.WriteByte('>')
}

func blockText(n ast.Node, source []byte) []byte {
	var buf bytes.Buffer
	lines := n.Lines()
	for i := range lines.Len() {
		seg := lines.At(i)
		buf.Write(seg.Value(source))
	}
	return buf.Bytes()
}
