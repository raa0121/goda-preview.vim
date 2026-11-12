// Package preview is the goda plugin "preview": it renders the buffers it is
// sent and shows them in the browser.
package preview

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Shougo/goda.vim/sdk"

	"github.com/raa0121/goda-preview.vim/internal/browser"
	"github.com/raa0121/goda-preview.vim/internal/render"
	"github.com/raa0121/goda-preview.vim/internal/server"
)

const Name = "preview"

// ScriptVersion goes with s:version in autoload/goda_preview.vim. Bump both
// when the Vim script and the binary no longer work together.
const ScriptVersion = 3

const (
	DefaultMermaidURL = "https://cdn.jsdelivr.net/npm/mermaid@11.4.1/dist/mermaid.min.js"
	DefaultKaTeXURL   = "https://cdn.jsdelivr.net/npm/katex@0.19.1/dist/"
	// The browser build of the ES module, which the page imports.
	DefaultAsciidoctorURL = "https://cdn.jsdelivr.net/npm/@asciidoctor/core@4.1.1/build/browser/index.js"
)

type Plugin struct {
	// Open is replaced in tests.
	Open func(url string) error

	mu      sync.Mutex
	srv     *server.Server
	opts    render.Options
	buffers map[int64]buffer
}

type buffer struct {
	path     string
	filetype string
}

func New() *Plugin {
	return &Plugin{Open: browser.Open, buffers: map[int64]buffer{}}
}

func (p *Plugin) Name() string { return Name }

func (p *Plugin) ScriptVersion() int { return ScriptVersion }

func (p *Plugin) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		// open(buffer, config) -> url: shows the buffer, opening the
		// browser when config.openBrowser.
		"open": p.open,
		// update(bufnr, text): the buffer changed. A notification.
		"update": p.update,
		// cursor(bufnr, line, total): the cursor moved. A notification.
		"cursor": p.cursor,
		// close(bufnr): the preview of the buffer ends. A notification.
		"close": p.close,
		// url(bufnr) -> the URL of the preview, "" when there is none.
		"url": p.url,
	}
}

type openBuffer struct {
	Bufnr    int64  `json:"bufnr"`
	Path     string `json:"path"`
	Filetype string `json:"filetype"`
	Text     string `json:"text"`
	Line     int64  `json:"line"`
	Total    int64  `json:"total"`
}

type config struct {
	CustomCSS         string `json:"customCSS"`
	DisableDefaultCSS bool   `json:"disableDefaultCSS"`
	MermaidURL        string `json:"mermaidURL"`
	KaTeXURL          string `json:"katexURL"`
	AsciidoctorURL    string `json:"asciidoctorURL"`
	PlantUMLServer    string `json:"plantumlServer"`
	// CodeStyle is the chroma style of the code, "" for no highlighting;
	// CodeStyleDark is the one for a dark colour scheme, "" for the same.
	CodeStyle     string `json:"codeStyle"`
	CodeStyleDark string `json:"codeStyleDark"`
	UnsafeHTML    bool   `json:"unsafeHTML"`
	HardLineBreak bool   `json:"hardLineBreak"`
	OpenBrowser   bool   `json:"openBrowser"`
}

func (p *Plugin) open(_ context.Context, _ sdk.Host, params []any) (any, error) {
	var b openBuffer
	var cfg config
	if err := sdk.DecodeArgs(params, &b, &cfg); err != nil {
		return nil, err
	}
	if !render.Supported(b.Filetype) {
		return nil, fmt.Errorf("goda-preview: filetype %q is not supported (supported: %v)", b.Filetype, render.Filetypes())
	}
	if cfg.MermaidURL == "" {
		cfg.MermaidURL = DefaultMermaidURL
	}
	if cfg.KaTeXURL == "" {
		cfg.KaTeXURL = DefaultKaTeXURL
	}
	if !strings.HasSuffix(cfg.KaTeXURL, "/") {
		cfg.KaTeXURL += "/"
	}
	if cfg.AsciidoctorURL == "" {
		cfg.AsciidoctorURL = DefaultAsciidoctorURL
	}
	for name, u := range map[string]string{
		"mermaid_url":     cfg.MermaidURL,
		"katex_url":       cfg.KaTeXURL,
		"asciidoctor_url": cfg.AsciidoctorURL,
		"plantuml_server": cfg.PlantUMLServer,
	} {
		if err := checkURL(u); err != nil {
			return nil, fmt.Errorf("goda-preview: g:goda_preview#%s: %w", name, err)
		}
	}
	var highlightCSS string
	if cfg.CodeStyle != "" {
		css, err := render.HighlightCSS(cfg.CodeStyle, cfg.CodeStyleDark)
		if err != nil {
			return nil, fmt.Errorf("goda-preview: g:goda_preview#code_style: %w", err)
		}
		highlightCSS = css
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv == nil {
		srv, err := server.Start()
		if err != nil {
			return nil, fmt.Errorf("goda-preview: %w", err)
		}
		p.srv = srv
	}
	var highlight func(lang, code string) (string, bool)
	if highlightCSS != "" {
		highlight = render.Highlight
	}
	p.srv.SetConfig(server.Config{
		Highlight:         highlight,
		CustomCSS:         cfg.CustomCSS,
		DisableDefaultCSS: cfg.DisableDefaultCSS,
		MermaidURL:        cfg.MermaidURL,
		KaTeXURL:          cfg.KaTeXURL,
		AsciidoctorURL:    cfg.AsciidoctorURL,
		HighlightCSS:      highlightCSS,
	})
	p.opts = render.Options{
		Unsafe:         cfg.UnsafeHTML,
		HardWraps:      cfg.HardLineBreak,
		PlantUMLServer: cfg.PlantUMLServer,
		Highlight:      highlightCSS != "",
	}
	p.buffers[b.Bufnr] = buffer{path: b.Path, filetype: b.Filetype}
	p.renderLocked(b.Bufnr, b.Text)
	p.srv.SetCursor(b.Bufnr, b.Line, b.Total)

	u := p.srv.URL(b.Bufnr)
	if cfg.OpenBrowser {
		if err := p.Open(u); err != nil {
			return nil, fmt.Errorf("goda-preview: open %s: %w", u, err)
		}
	}
	return u, nil
}

func checkURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) URL", raw)
	}
	return nil
}

func (p *Plugin) update(_ context.Context, _ sdk.Host, params []any) (any, error) {
	var bufnr int64
	var text string
	if err := sdk.DecodeArgs(params, &bufnr, &text); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.buffers[bufnr]; ok {
		p.renderLocked(bufnr, text)
	}
	return nil, nil
}

// renderLocked shows a render error in the page: the editor only sees
// errors of requests, and update is a notification.
func (p *Plugin) renderLocked(bufnr int64, text string) {
	b := p.buffers[bufnr]
	res, err := render.Render(b.filetype, text, p.opts)
	if err != nil {
		res = render.Result{HTML: `<pre class="goda-preview-error">` + html.EscapeString(err.Error()) + "</pre>"}
	}
	page := server.Page{HTML: res.HTML, AsciiDoc: res.AsciiDoc, Lines: res.Lines, Title: "[No Name]"}
	if b.path != "" {
		page.Title = filepath.Base(b.path)
		page.Dir = filepath.Dir(b.path)
	}
	p.srv.Update(bufnr, page)
}

func (p *Plugin) cursor(_ context.Context, _ sdk.Host, params []any) (any, error) {
	var bufnr, line, total int64
	if err := sdk.DecodeArgs(params, &bufnr, &line, &total); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.buffers[bufnr]; ok {
		p.srv.SetCursor(bufnr, line, total)
	}
	return nil, nil
}

func (p *Plugin) close(_ context.Context, _ sdk.Host, params []any) (any, error) {
	var bufnr int64
	if err := sdk.DecodeArgs(params, &bufnr); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.buffers[bufnr]; ok {
		delete(p.buffers, bufnr)
		p.srv.Remove(bufnr)
	}
	return nil, nil
}

func (p *Plugin) url(_ context.Context, _ sdk.Host, params []any) (any, error) {
	var bufnr int64
	if err := sdk.DecodeArgs(params, &bufnr); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.buffers[bufnr]; !ok {
		return "", nil
	}
	return p.srv.URL(bufnr), nil
}

// Close is sdk.Closer: the server goes with the binary.
func (p *Plugin) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv == nil {
		return nil
	}
	return p.srv.Close(ctx)
}
