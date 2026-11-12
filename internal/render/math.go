package render

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// TeX math, drawn by KaTeX in the page:
//
//	inline $a^2$ and $$\sum_i x_i$$ (display), within one line
//	$$          a block, up to a line ending with $$
//	\int f
//	$$
//
// The page finds them by the classes math-inline and math-display, with the
// TeX as their text.
type mathExtension struct{}

func (mathExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithBlockParsers(util.Prioritized(mathBlockParser{}, 150)),
		parser.WithInlineParsers(util.Prioritized(mathInlineParser{}, 150)),
	)
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(mathRenderer{}, 100)))
}

var (
	kindMathInline = ast.NewNodeKind("MathInline")
	kindMathBlock  = ast.NewNodeKind("MathBlock")
)

type mathInline struct {
	ast.BaseInline
	tex     []byte
	display bool
}

func (n *mathInline) Kind() ast.NodeKind { return kindMathInline }

func (n *mathInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"TeX": string(n.tex)}, nil)
}

type mathBlock struct {
	ast.BaseBlock
}

func (n *mathBlock) Kind() ast.NodeKind { return kindMathBlock }

func (n *mathBlock) IsRaw() bool { return true }

func (n *mathBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type mathInlineParser struct{}

func (mathInlineParser) Trigger() []byte { return []byte{'$'} }

// Parse follows pandoc's rules, so that prices stay text: the opening $ is
// not followed by a space, and the closing one is neither preceded by a space
// nor followed by a digit ("$5 and $10" is no math).
func (mathInlineParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if bytes.HasPrefix(line, []byte("$$")) {
		end := bytes.Index(line[2:], []byte("$$"))
		if end <= 0 || isBlankBytes(line[2:2+end]) {
			return nil
		}
		block.Advance(2 + end + 2)
		return &mathInline{tex: bytes.Clone(line[2 : 2+end]), display: true}
	}
	if len(line) < 3 || isSpace(line[1]) {
		return nil
	}
	for i := 1; i < len(line) && line[i] != '\n'; i++ {
		switch {
		case line[i] == '\\':
			i++
		case line[i] == '$' && i > 1 && !isSpace(line[i-1]) && (i+1 == len(line) || !isDigit(line[i+1])):
			block.Advance(i + 1)
			return &mathInline{tex: bytes.Clone(line[1:i])}
		}
	}
	return nil
}

type mathBlockParser struct{}

func (mathBlockParser) Trigger() []byte { return []byte{'$'} }

func (mathBlockParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, seg := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || !bytes.HasPrefix(line[pos:], []byte("$$")) {
		return nil, parser.NoChildren
	}
	start := seg.Start + pos + 2
	rest := bytes.TrimRight(line[pos+2:], " \t\r\n")
	node := &mathBlock{}
	if bytes.HasSuffix(rest, []byte("$$")) {
		// $$ x $$ on one line.
		if isBlankBytes(rest[:len(rest)-2]) {
			return nil, parser.NoChildren
		}
		node.Lines().Append(text.NewSegment(start, start+len(rest)-2))
		reader.AdvanceToEOL()
		return node, parser.Close
	}
	if !isBlankBytes(rest) {
		node.Lines().Append(text.NewSegment(start, seg.Stop))
	}
	reader.AdvanceToEOL()
	return node, parser.NoChildren
}

func (mathBlockParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	line, seg := reader.PeekLine()
	trimmed := bytes.TrimRight(line, " \t\r\n")
	if bytes.HasSuffix(trimmed, []byte("$$")) {
		if body := trimmed[:len(trimmed)-2]; !isBlankBytes(body) {
			node.Lines().Append(text.NewSegment(seg.Start, seg.Start+len(body)))
		}
		reader.AdvanceToEOL()
		return parser.Close
	}
	node.Lines().Append(seg)
	reader.AdvanceToEOL()
	return parser.Continue | parser.NoChildren
}

func (mathBlockParser) Close(ast.Node, text.Reader, parser.Context) {}

func (mathBlockParser) CanInterruptParagraph() bool { return true }

func (mathBlockParser) CanAcceptIndentedLine() bool { return false }

type mathRenderer struct{}

func (mathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMathInline, renderMathInline)
	reg.Register(kindMathBlock, renderMathBlock)
}

func renderMathInline(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		n := node.(*mathInline)
		class := "math math-inline"
		if n.display {
			class = "math math-display"
		}
		_, _ = w.WriteString(`<span class="` + class + `">`)
		_, _ = w.Write(util.EscapeHTML(n.tex))
		_, _ = w.WriteString("</span>")
	}
	return ast.WalkSkipChildren, nil
}

func renderMathBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		writeDisplayMath(w, node, blockText(node, source))
	}
	return ast.WalkSkipChildren, nil
}

// writeDisplayMath is shared with ```math blocks.
func writeDisplayMath(w util.BufWriter, n ast.Node, tex []byte) {
	writeOpenTag(w, n, "div", ` class="math math-display"`)
	_, _ = w.Write(util.EscapeHTML(tex))
	_, _ = w.WriteString("</div>\n")
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func isDigit(b byte) bool { return '0' <= b && b <= '9' }

func isBlankBytes(b []byte) bool { return len(bytes.TrimSpace(b)) == 0 }
