package render

import (
	"regexp"
	"strings"

	"github.com/go-docutils/docutils/html"
	"github.com/go-docutils/docutils/rst"
)

func renderRST(source string, opts Options) (Result, error) {
	o := rst.DefaultOptions()
	o.RawEnabled = opts.Unsafe
	o.ResolveReferences = true
	o.NumberAutoFootnotes = true
	// SourcePath stays empty: that disables the include directive, so a
	// document cannot pull arbitrary files into the page.
	return Result{HTML: rstMath(html.Render(rst.ParseWithOptions(source, o)))}, nil
}

var (
	rstVerbatim    = regexp.MustCompile(`(?s)<pre\b.*?</pre>|<code\b.*?</code>`)
	rstDisplayMath = regexp.MustCompile(`(?s)<div>\\\[(.*?)\\\]</div>`)
	rstInlineMath  = regexp.MustCompile(`(?s)\\\((.*?)\\\)`)
)

// rstMath turns the \(…\) and \[…\] that go-docutils writes for the math role
// and directive into the elements of Markdown's math, which the page draws.
// Literal text is left alone: it may well contain \( itself.
func rstMath(h string) string {
	var b strings.Builder
	last := 0
	for _, loc := range rstVerbatim.FindAllStringIndex(h, -1) {
		b.WriteString(rstMathText(h[last:loc[0]]))
		b.WriteString(h[loc[0]:loc[1]])
		last = loc[1]
	}
	b.WriteString(rstMathText(h[last:]))
	return b.String()
}

func rstMathText(s string) string {
	s = rstDisplayMath.ReplaceAllString(s, `<div class="math math-display">$1</div>`)
	return rstInlineMath.ReplaceAllString(s, `<span class="math math-inline">$1</span>`)
}
