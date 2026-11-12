package preview

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newPlugin(t *testing.T) (*Plugin, *[]string) {
	t.Helper()
	p := New()
	var opened []string
	p.Open = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := p.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return p, &opened
}

func call(t *testing.T, p *Plugin, method string, params ...any) (any, error) {
	t.Helper()
	return p.Handlers()[method](context.Background(), nil, params)
}

// As the editor sends them: integers are int64, objects map[string]any.
func openParams(path, filetype, text string, cfg map[string]any) []any {
	return []any{
		map[string]any{"bufnr": int64(7), "path": path, "filetype": filetype, "text": text, "line": int64(1), "total": int64(3)},
		cfg,
	}
}

// fetch resolves "../" in ref as the browser does: the client sends it as is.
func fetch(t *testing.T, ref string) string {
	t.Helper()
	pu, err := url.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	u := pu.ResolveReference(&url.URL{}).String()
	res, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", u, res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

// firstRender reads the event stream up to the HTML of its first render.
func firstRender(t *testing.T, u string) string {
	t.Helper()
	res, err := http.Get(u + "events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := make([]byte, 64<<10)
	var got strings.Builder
	for !strings.Contains(got.String(), "\n\n") {
		n, err := res.Body.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return got.String()
}

func TestOpenUpdateClose(t *testing.T) {
	p, opened := newPlugin(t)
	path := filepath.Join(t.TempDir(), "doc.md")
	res, err := call(t, p, "open", openParams(path, "markdown", "# One", map[string]any{"openBrowser": true})...)
	if err != nil {
		t.Fatal(err)
	}
	u := res.(string)
	if len(*opened) != 1 || (*opened)[0] != u {
		t.Errorf("opened %v, want [%s]", *opened, u)
	}
	if got, _ := call(t, p, "url", int64(7)); got != u {
		t.Errorf("url = %v, want %s", got, u)
	}
	if page := fetch(t, u); !strings.Contains(page, "<title>doc.md</title>") {
		t.Errorf("page:\n%s", page)
	}
	if ev := firstRender(t, u); !strings.Contains(ev, "One") || !strings.Contains(ev, `"lines":true`) {
		t.Errorf("render event: %s", ev)
	}

	if _, err := call(t, p, "update", int64(7), "# Two"); err != nil {
		t.Fatal(err)
	}
	if ev := firstRender(t, u); !strings.Contains(ev, "Two") {
		t.Errorf("after update: %s", ev)
	}
	if _, err := call(t, p, "cursor", int64(7), int64(2), int64(3)); err != nil {
		t.Fatal(err)
	}

	if _, err := call(t, p, "close", int64(7)); err != nil {
		t.Fatal(err)
	}
	if got, _ := call(t, p, "url", int64(7)); got != "" {
		t.Errorf("url after close = %v", got)
	}
	// Notifications for a buffer without a preview are ignored.
	for _, m := range [][]any{{"update", int64(7), "x"}, {"cursor", int64(7), int64(1), int64(1)}, {"close", int64(7)}} {
		if _, err := call(t, p, m[0].(string), m[1:]...); err != nil {
			t.Errorf("%s after close: %v", m[0], err)
		}
	}
}

func TestOpenErrors(t *testing.T) {
	p, opened := newPlugin(t)
	for _, tc := range []struct {
		name     string
		filetype string
		cfg      map[string]any
		want     string
	}{
		{"filetype", "org", map[string]any{}, `filetype "org" is not supported`},
		{"asciidoctor", "asciidoc", map[string]any{"asciidoctorURL": "asciidoctor.js"}, "g:goda_preview#asciidoctor_url"},
		{"plantuml", "markdown", map[string]any{"plantumlServer": "javascript:alert(1)"}, "g:goda_preview#plantuml_server"},
		{"mermaid", "markdown", map[string]any{"mermaidURL": "/local/mermaid.js"}, "g:goda_preview#mermaid_url"},
		{"katex", "markdown", map[string]any{"katexURL": "file:///katex/"}, "g:goda_preview#katex_url"},
		{"code style", "markdown", map[string]any{"codeStyle": "nosuchstyle"}, "g:goda_preview#code_style"},
		{"dark code style", "markdown", map[string]any{"codeStyle": "github", "codeStyleDark": "nosuchstyle"}, "g:goda_preview#code_style"},
	} {
		_, err := call(t, p, "open", openParams("", tc.filetype, "", tc.cfg)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	if len(*opened) != 0 {
		t.Errorf("browser opened on error: %v", *opened)
	}
	// A Vim number where a boolean is expected is a mistake of the Vim
	// script, which converts them.
	if _, err := call(t, p, "open", openParams("", "markdown", "", map[string]any{"openBrowser": int64(1)})...); err == nil {
		t.Error("no error for a number as openBrowser")
	}
}

func TestMathAndHighlight(t *testing.T) {
	p, _ := newPlugin(t)
	src := "$x$\n\n```go\nfunc f() {}\n```\n"
	res, err := call(t, p, "open", openParams("", "markdown", src, map[string]any{"codeStyle": "github", "codeStyleDark": "github-dark"})...)
	if err != nil {
		t.Fatal(err)
	}
	u := res.(string)
	page := fetch(t, u)
	if !strings.Contains(page, `data-katex="`+DefaultKaTeXURL+`"`) || !strings.Contains(page, "highlight.css") {
		t.Errorf("page:\n%s", page)
	}
	if css := fetch(t, u+"../../highlight.css"); !strings.Contains(css, "prefers-color-scheme: dark") {
		t.Errorf("highlight.css:\n%s", css)
	}
	if ev := firstRender(t, u); !strings.Contains(ev, `math math-inline`) || !strings.Contains(ev, `class=\"chroma\"`) {
		t.Errorf("render event: %s", ev)
	}

	// No style: no highlighting. The KaTeX directory gets its slash.
	res, err = call(t, p, "open", openParams("", "markdown", src, map[string]any{"katexURL": "https://cdn.example.com/katex"})...)
	if err != nil {
		t.Fatal(err)
	}
	u = res.(string)
	if page := fetch(t, u); strings.Contains(page, "highlight.css") || !strings.Contains(page, `data-katex="https://cdn.example.com/katex/"`) {
		t.Errorf("page:\n%s", page)
	}
	if ev := firstRender(t, u); strings.Contains(ev, "chroma") {
		t.Errorf("highlighted without a style: %s", ev)
	}
}

func TestAsciiDoc(t *testing.T) {
	p, _ := newPlugin(t)
	res, err := call(t, p, "open", openParams("", "asciidoc", "= Title\n\n<b>text</b>\n", map[string]any{})...)
	if err != nil {
		t.Fatal(err)
	}
	u := res.(string)
	if page := fetch(t, u); !strings.Contains(page, `data-asciidoctor="`+DefaultAsciidoctorURL+`"`) {
		t.Errorf("page:\n%s", page)
	}
	// The source as it is, for the page to convert.
	if ev := firstRender(t, u); !strings.Contains(ev, `"asciidoc":"= Title\n\n`) || !strings.Contains(ev, `text`) || !strings.Contains(ev, `"html":""`) {
		t.Errorf("render event: %s", ev)
	}
}

func TestNoNameBuffer(t *testing.T) {
	p, _ := newPlugin(t)
	res, err := call(t, p, "open", openParams("", "rst", "Title\n=====\n", map[string]any{})...)
	if err != nil {
		t.Fatal(err)
	}
	u := res.(string)
	if page := fetch(t, u); !strings.Contains(page, "<title>[No Name]</title>") {
		t.Errorf("page:\n%s", page)
	}
	if ev := firstRender(t, u); !strings.Contains(ev, `"lines":false`) {
		t.Errorf("rst render event: %s", ev)
	}
}
