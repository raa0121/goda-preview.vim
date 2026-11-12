// Package server serves the preview pages to the browser and pushes their
// updates over server-sent events.
package server

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed assets
var assets embed.FS

var pageTemplate = template.Must(template.ParseFS(assets, "assets/page.html"))

type Config struct {
	// CustomCSS is the path of a stylesheet loaded after the default one,
	// reloaded in the page whenever the file changes.
	CustomCSS         string
	DisableDefaultCSS bool
	// MermaidURL is the script of mermaid, loaded only by pages that have
	// a diagram.
	MermaidURL string
	// KaTeXURL is the directory of katex.min.js and katex.min.css, ending
	// with a slash, loaded only by pages that have math.
	KaTeXURL string
	// AsciidoctorURL is the ES module of asciidoctor.js, loaded only by
	// pages of AsciiDoc.
	AsciidoctorURL string
	// HighlightCSS is the stylesheet of the highlighted code; empty when
	// the code is not highlighted.
	HighlightCSS string
	// Highlight colours code that the page converts itself, for the
	// stylesheet HighlightCSS; false when it does not know lang. Nil when
	// the code is not highlighted.
	Highlight func(lang, code string) (string, bool)
}

// Page is what a preview shows.
type Page struct {
	Title string
	// Dir is the directory the relative URLs of the document (images)
	// resolve against; nothing outside it is served.
	Dir  string
	HTML string
	// AsciiDoc is a document the page converts, in place of HTML.
	AsciiDoc string
	// Lines tells the page that HTML carries data-line attributes.
	Lines bool
}

type Server struct {
	ln     net.Listener
	srv    *http.Server
	base   string
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	cfg    Config
	cssVer int64
	pages  map[int64]*page
}

type page struct {
	Page
	renderVer   int64
	line, total int64
	cursorVer   int64
	closed      bool
	subs        map[chan struct{}]struct{}
}

// Start listens on a random port of the loopback interface.
//
// Every URL carries a random token, and requests must name the listening
// address as their Host: other pages in the browser cannot guess the URLs,
// and DNS rebinding cannot reach the server under another name.
func Start() (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	token := rand.Text()
	s := &Server{
		ln:     ln,
		base:   "http://" + ln.Addr().String() + "/" + token + "/",
		ctx:    ctx,
		cancel: cancel,
		pages:  map[int64]*page{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /p/{id}/{$}", s.servePage)
	mux.HandleFunc("GET /p/{id}/events", s.serveEvents)
	mux.HandleFunc("GET /p/{id}/files/{path...}", s.serveFile)
	mux.HandleFunc("GET /assets/{name}", s.serveAsset)
	mux.HandleFunc("GET /custom.css", s.serveCustomCSS)
	mux.HandleFunc("GET /highlight.css", s.serveHighlightCSS)
	mux.HandleFunc("POST /highlight", s.serveHighlight)
	s.srv = &http.Server{
		Handler: s.guard("/"+token, mux),
		// The event streams never go idle, so Shutdown alone would wait
		// for its deadline: cancelling this context ends them.
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = s.srv.Serve(ln) }()
	go s.watchCSS()
	return s, nil
}

func (s *Server) guard(prefix string, next http.Handler) http.Handler {
	host := s.ln.Addr().String()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			http.NotFound(w, r)
			return
		}
		http.StripPrefix(prefix, next).ServeHTTP(w, r)
	})
}

func (s *Server) URL(id int64) string {
	return s.base + "p/" + strconv.FormatInt(id, 10) + "/"
}

func (s *Server) SetConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

// Update sets the page of id, creating it, and pushes it to its browsers.
func (s *Server) Update(id int64, p Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pg := s.pages[id]
	if pg == nil {
		pg = &page{subs: map[chan struct{}]struct{}{}}
		s.pages[id] = pg
	}
	pg.Page = p
	pg.renderVer++
	pg.wake()
}

// SetCursor scrolls the page of id to line of total.
func (s *Server) SetCursor(id, line, total int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pg := s.pages[id]; pg != nil {
		pg.line, pg.total = line, total
		pg.cursorVer++
		pg.wake()
	}
}

// Remove tells the browsers of id that the preview is closed.
func (s *Server) Remove(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pg := s.pages[id]; pg != nil {
		pg.closed = true
		pg.wake()
		delete(s.pages, id)
	}
}

func (s *Server) Close(ctx context.Context) error {
	s.cancel()
	return s.srv.Shutdown(ctx)
}

// wake never blocks: a subscriber that is behind is woken once and sends
// only the latest state.
func (p *page) wake() {
	for ch := range p.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Server) lookup(r *http.Request) (*page, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pg, ok := s.pages[id]
	return pg, ok
}

func (s *Server) servePage(w http.ResponseWriter, r *http.Request) {
	pg, ok := s.lookup(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	cfg, title := s.cfg, pg.Title
	s.mu.Unlock()

	scripts := "'self'"
	for _, u := range []string{cfg.MermaidURL, cfg.KaTeXURL, cfg.AsciidoctorURL} {
		if origin := originOf(u); origin != "" && !strings.Contains(scripts, " "+origin) {
			scripts += " " + origin
		}
	}
	// Images and stylesheets may come from anywhere, as in any Markdown
	// viewer; scripts only from here, mermaid, KaTeX and asciidoctor.js,
	// and inline ones never, so a document cannot run code even with raw
	// HTML allowed.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src "+scripts+
		"; style-src * 'unsafe-inline'; img-src * data: blob:; font-src * data:; connect-src 'self'; object-src 'none'; base-uri 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pageTemplate.Execute(w, map[string]any{
		"Title":          title,
		"DefaultCSS":     !cfg.DisableDefaultCSS,
		"CustomCSS":      cfg.CustomCSS != "",
		"HighlightCSS":   cfg.HighlightCSS != "",
		"MermaidURL":     cfg.MermaidURL,
		"KaTeXURL":       cfg.KaTeXURL,
		"AsciidoctorURL": cfg.AsciidoctorURL,
	})
}

func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	pg, ok := s.lookup(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	pg.subs[ch] = struct{}{}
	// The page was loaded with the current stylesheet.
	sentCSS := s.cssVer
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(pg.subs, ch)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	rc := http.NewResponseController(w)
	var sentRender, sentCursor int64
	for {
		s.mu.Lock()
		closed := pg.closed
		renderVer, cursorVer, cssVer := pg.renderVer, pg.cursorVer, s.cssVer
		p, line, total := pg.Page, pg.line, pg.total
		s.mu.Unlock()

		var err error
		if closed {
			err = writeEvent(w, "close", struct{}{})
			_ = rc.Flush()
			return
		}
		if renderVer != sentRender {
			ev := map[string]any{"html": p.HTML, "lines": p.Lines, "title": p.Title}
			if p.AsciiDoc != "" {
				ev["asciidoc"] = p.AsciiDoc
			}
			err = errors.Join(err, writeEvent(w, "render", ev))
			sentRender = renderVer
		}
		if cursorVer != sentCursor {
			err = errors.Join(err, writeEvent(w, "cursor", map[string]any{"line": line, "total": total}))
			sentCursor = cursorVer
		}
		if cssVer != sentCSS {
			err = errors.Join(err, writeEvent(w, "css", struct{}{}))
			sentCSS = cssVer
		}
		if err != nil || rc.Flush() != nil {
			return
		}
		select {
		case <-ch:
		case <-r.Context().Done():
			return
		}
	}
}

func writeEvent(w http.ResponseWriter, name string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
	return err
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	pg, ok := s.lookup(r)
	name := r.PathValue("path")
	if !ok || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	dir := pg.Dir
	s.mu.Unlock()
	if dir == "" {
		http.NotFound(w, r)
		return
	}
	// os.Root also refuses symlinks that lead out of dir.
	root, err := os.OpenRoot(dir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	if fi, err := root.Stat(name); err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, root.FS(), name)
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != "preview.js" && name != "default.css" {
		http.NotFound(w, r)
		return
	}
	// The binary may be rebuilt with other assets under the same URL.
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFileFS(w, r, assets, "assets/"+name)
}

func (s *Server) serveCustomCSS(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	path := s.cfg.CustomCSS
	s.mu.Unlock()
	if path == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

type codeBlock struct {
	Lang string `json:"lang"`
	Code string `json:"code"`
}

// serveHighlight takes the code blocks of a page as [{lang, code}] and
// answers with their HTML in the same order, "" for those left as they are.
func (s *Server) serveHighlight(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	highlight := s.cfg.Highlight
	s.mu.Unlock()
	var blocks []codeBlock
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&blocks); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := make([]string, len(blocks))
	if highlight != nil {
		for i, b := range blocks {
			if h, ok := highlight(b.Lang, b.Code); ok {
				out[i] = h
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) serveHighlightCSS(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	css := s.cfg.HighlightCSS
	s.mu.Unlock()
	if css == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(w, css)
}

// watchCSS polls rather than relying on the editor, so that a stylesheet
// saved by another program is reloaded too.
func (s *Server) watchCSS() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var path string
	var stamp string
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		s.mu.Lock()
		cur := s.cfg.CustomCSS
		s.mu.Unlock()
		next := ""
		if fi, err := os.Stat(cur); cur != "" && err == nil {
			next = fi.ModTime().String() + "/" + strconv.FormatInt(fi.Size(), 10)
		}
		if cur == path && next != stamp {
			s.mu.Lock()
			s.cssVer++
			for _, pg := range s.pages {
				pg.wake()
			}
			s.mu.Unlock()
		}
		path, stamp = cur, next
	}
}
