package render

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// The code is written with classes, not colours, so that the stylesheet of
// HighlightCSS can follow the colour scheme of the browser.
var codeFormatter = chromahtml.New(chromahtml.WithClasses(true), chromahtml.PreventSurroundingPre(true))

// Highlight is highlight for the code that the page converts itself
// (AsciiDoc).
func Highlight(lang, code string) (string, bool) {
	b, ok := highlight(lang, []byte(code))
	return string(b), ok
}

// highlight returns code as the spans of chroma, or false when chroma has no
// lexer for lang.
func highlight(lang string, code []byte) ([]byte, bool) {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return nil, false
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, string(code))
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	if err := codeFormatter.Format(&buf, styles.Fallback, it); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// HighlightCSS is the stylesheet of the highlighted code: the chroma style
// light, and dark when the browser prefers a dark colour scheme. dark may be
// empty, for light in both.
func HighlightCSS(light, dark string) (string, error) {
	var b strings.Builder
	if dark == "" || dark == light {
		if err := writeStyleCSS(&b, light); err != nil {
			return "", err
		}
		return b.String(), nil
	}
	// Each style only in its own scheme: a style has no rule for the tokens
	// it leaves in the text colour, and the other style's rule for them
	// would show through.
	for _, s := range []struct{ scheme, name string }{{"light", light}, {"dark", dark}} {
		b.WriteString("@media (prefers-color-scheme: " + s.scheme + ") {\n")
		if err := writeStyleCSS(&b, s.name); err != nil {
			return "", err
		}
		b.WriteString("}\n")
	}
	return b.String(), nil
}

func writeStyleCSS(b *strings.Builder, name string) error {
	style, ok := styles.Registry[strings.ToLower(name)]
	if !ok {
		return fmt.Errorf("unknown code style %q (see https://xyproto.github.io/splash/docs/)", name)
	}
	f := chromahtml.New(chromahtml.WithClasses(true))
	if err := f.WriteCSS(b, style); err != nil {
		return err
	}
	// More specific than the default stylesheet's pre, so the code is on the
	// background its colours were made for.
	fmt.Fprintf(b, ".goda-preview pre.chroma { %s }\n", chromahtml.StyleEntryToCSS(style.Get(chroma.Background)))
	return nil
}
