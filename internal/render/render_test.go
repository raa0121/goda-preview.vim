package render

import (
	"strings"
	"testing"
)

func mustRender(t *testing.T, filetype, src string, opts Options) Result {
	t.Helper()
	r, err := Render(filetype, src, opts)
	if err != nil {
		t.Fatalf("Render(%s): %v", filetype, err)
	}
	return r
}

func TestMarkdownLines(t *testing.T) {
	src := "# Title\n\npara\n\n- a\n- b\n\n```go\nx := 1\n```\n\n    indented\n\n> quote\n"
	r := mustRender(t, "markdown", src, Options{})
	if !r.Lines {
		t.Error("Lines = false")
	}
	for _, want := range []string{
		`<h1 id="title" data-line="1">`,
		`<p data-line="3">para</p>`,
		`<ul data-line="5">`,
		`<li data-line="6">b</li>`,
		`<pre data-line="8"><code class="language-go">x := 1`,
		`<pre data-line="12"><code>indented`,
		`<blockquote data-line="14">`,
	} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("missing %s in\n%s", want, r.HTML)
		}
	}
}

func TestMarkdownDiagrams(t *testing.T) {
	src := "```mermaid\ngraph TD; A-->B<C;\n```\n\n```plantuml\nA -> B\n```\n"
	r := mustRender(t, "markdown", src, Options{})
	if !strings.Contains(r.HTML, `<pre class="mermaid" data-line="1">graph TD; A--&gt;B&lt;C;`) {
		t.Errorf("mermaid block not kept as text:\n%s", r.HTML)
	}
	if strings.Contains(r.HTML, "<img") || !strings.Contains(r.HTML, `class="language-plantuml"`) {
		t.Errorf("plantuml without a server must stay code:\n%s", r.HTML)
	}

	r = mustRender(t, "markdown", src, Options{PlantUMLServer: "http://localhost:8080"})
	if !strings.Contains(r.HTML, `<p class="plantuml" data-line="5"><img alt="PlantUML diagram" src="http://localhost:8080/svg/`) {
		t.Errorf("plantuml not sent to the server:\n%s", r.HTML)
	}
}

func TestMarkdownRawHTML(t *testing.T) {
	src := "<script>alert(1)</script>\n\nx <b>y</b>\n"
	if r := mustRender(t, "markdown", src, Options{}); strings.Contains(r.HTML, "<script>") || strings.Contains(r.HTML, "<b>") {
		t.Errorf("raw HTML let through by default:\n%s", r.HTML)
	}
	if r := mustRender(t, "markdown", src, Options{Unsafe: true}); !strings.Contains(r.HTML, "<b>y</b>") {
		t.Errorf("raw HTML dropped with Unsafe:\n%s", r.HTML)
	}
}

func TestMarkdownExtensions(t *testing.T) {
	src := "| a |\n|---|\n| 1 |\n\n- [x] done\n\n~~gone~~\n\nnote[^1]\n\n[^1]: foot\n"
	r := mustRender(t, "markdown", src, Options{})
	for _, want := range []string{"<table", `type="checkbox"`, "<del>gone</del>", `class="footnotes"`} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("missing %s in\n%s", want, r.HTML)
		}
	}
	if r := mustRender(t, "markdown", "a\nb\n", Options{HardWraps: true}); !strings.Contains(r.HTML, "<br") {
		t.Errorf("HardWraps without <br>:\n%s", r.HTML)
	}
}

func TestMarkdownMath(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"a $x^2$ b", `<p data-line="1">a <span class="math math-inline">x^2</span> b</p>`},
		{"a $$\\sum_i x_i$$ b", `<span class="math math-display">\sum_i x_i</span>`},
		{"$a<b$", `<span class="math math-inline">a&lt;b</span>`},
		{"$\\{a\\}$ and $\\$$", `<span class="math math-inline">\{a\}</span> and <span class="math math-inline">\$</span>`},
		{"$x$, $y$.", `<span class="math math-inline">x</span>, <span class="math math-inline">y</span>.`},
		{"text\n\n$$\n\\int_0^1 x\\,dx\n$$\n", `<div class="math math-display" data-line="3">\int_0^1 x\,dx` + "\n</div>"},
		{"$$ E = mc^2 $$\n", `<div class="math math-display" data-line="1"> E = mc^2 </div>`},
		{"$$a\nb$$\n", `<div class="math math-display" data-line="1">a` + "\nb</div>"},
		{"para\n$$\nx\n$$\n", `<p data-line="1">para</p>` + "\n" + `<div class="math math-display" data-line="2">x`},
		{"```math\n\\frac12\n```\n", `<div class="math math-display" data-line="1">\frac12`},
		{"```katex\nx\n```\n", `<div class="math math-display" data-line="1">x`},
	} {
		if r := mustRender(t, "markdown", tc.src, Options{}); !strings.Contains(r.HTML, tc.want) {
			t.Errorf("%q: missing %s in\n%s", tc.src, tc.want, r.HTML)
		}
	}
	for _, src := range []string{
		"costs $5 and $10",
		"$ x $",
		"a \\$x$ b",
		"`$x$`",
		"a $$ b",
		"US$",
	} {
		if r := mustRender(t, "markdown", src, Options{}); strings.Contains(r.HTML, "math") {
			t.Errorf("%q became math:\n%s", src, r.HTML)
		}
	}
}

func TestMarkdownHighlight(t *testing.T) {
	src := "```go\nfunc f() {}\n```\n\n```nosuchlanguage\nx\n```\n\n    indented\n"
	r := mustRender(t, "markdown", src, Options{Highlight: true})
	for _, want := range []string{
		`<pre class="chroma" data-line="1"><code class="language-go"><span class="kd">func</span>`,
		`<pre data-line="5"><code class="language-nosuchlanguage">x`,
		`<pre data-line="9"><code>indented`,
	} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("missing %s in\n%s", want, r.HTML)
		}
	}
	if r := mustRender(t, "markdown", src, Options{}); strings.Contains(r.HTML, "chroma") {
		t.Errorf("highlighted without Highlight:\n%s", r.HTML)
	}
}

func TestHighlightCSS(t *testing.T) {
	css, err := HighlightCSS("github", "github-dark")
	if err != nil {
		t.Fatal(err)
	}
	light, dark, ok := strings.Cut(css, "@media (prefers-color-scheme: dark)")
	if !ok || !strings.HasPrefix(light, "@media (prefers-color-scheme: light)") {
		t.Fatalf("styles not scoped to their colour schemes:\n%s", css)
	}
	for _, part := range []string{light, dark} {
		if !strings.Contains(part, ".chroma .kd {") || !strings.Contains(part, ".goda-preview pre.chroma {") {
			t.Errorf("missing rules in\n%s", part)
		}
	}
	if css, err := HighlightCSS("github", ""); err != nil || strings.Contains(css, "@media") {
		t.Errorf("HighlightCSS(github, \"\") = %q, %v", css, err)
	}
	if _, err := HighlightCSS("nosuchstyle", ""); err == nil {
		t.Error("no error for an unknown style")
	}
}

func TestRSTMath(t *testing.T) {
	src := "Inline :math:`a<b`.\n\n.. math::\n\n   \\int_0^1 x\\,dx\n\n::\n\n   \\(literal\\)\n\nSee ``\\(code\\)``.\n"
	r := mustRender(t, "rst", src, Options{})
	for _, want := range []string{
		`<span class="math math-inline">a&lt;b</span>`,
		`<div class="math math-display">\int_0^1 x\,dx</div>`,
		`<pre><code>\(literal\)</code></pre>`,
		`\(code\)</code>`,
	} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("missing %s in\n%s", want, r.HTML)
		}
	}
}

func TestRST(t *testing.T) {
	src := "Title\n=====\n\nSome *text*.\n\n.. raw:: html\n\n   <b>raw</b>\n"
	r := mustRender(t, "rst", src, Options{})
	if r.Lines {
		t.Error("Lines = true for rst")
	}
	if !strings.Contains(r.HTML, "<em>text</em>") || !strings.Contains(r.HTML, "Title") {
		t.Errorf("unexpected rst output:\n%s", r.HTML)
	}
	if strings.Contains(r.HTML, "<b>raw</b>") {
		t.Errorf("raw directive let through by default:\n%s", r.HTML)
	}
	if r := mustRender(t, "rst", src, Options{Unsafe: true}); !strings.Contains(r.HTML, "<b>raw</b>") {
		t.Errorf("raw directive dropped with Unsafe:\n%s", r.HTML)
	}
}

func TestTextile(t *testing.T) {
	src := "h1. Title\n\nSome _text_.\n\n<script>alert(1)</script>\n"
	r := mustRender(t, "textile", src, Options{})
	if !strings.Contains(r.HTML, "<h1>Title</h1>") || !strings.Contains(r.HTML, "<em>text</em>") {
		t.Errorf("unexpected textile output:\n%s", r.HTML)
	}
	if strings.Contains(r.HTML, "<script>") {
		t.Errorf("script let through by default:\n%s", r.HTML)
	}
}

func TestAsciiDoc(t *testing.T) {
	for _, ft := range []string{"asciidoc", "asciidoctor"} {
		r := mustRender(t, ft, "= Title\n\ntext\n", Options{})
		if r.AsciiDoc != "= Title\n\ntext\n" || r.HTML != "" || !r.Lines {
			t.Errorf("%s: %+v", ft, r)
		}
	}
}

func TestUnsupported(t *testing.T) {
	if _, err := Render("org", "", Options{}); err == nil {
		t.Error("no error for org")
	}
	if Supported("org") || !Supported("markdown") {
		t.Error("Supported is wrong")
	}
}
