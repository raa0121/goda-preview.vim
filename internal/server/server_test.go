package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func start(t *testing.T) *Server {
	t.Helper()
	s, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

// get resolves "../" in ref as the browser does: the client sends it as is.
func get(t *testing.T, ref string) (*http.Response, string) {
	t.Helper()
	u, err := url.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(u.ResolveReference(&url.URL{}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestPage(t *testing.T) {
	s := start(t)
	s.SetConfig(Config{MermaidURL: "https://cdn.example.com/mermaid.js"})
	s.Update(3, Page{Title: "a<b>.md", HTML: "<p>x</p>"})

	res, body := get(t, s.URL(3))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self' https://cdn.example.com;") {
		t.Errorf("CSP = %q", csp)
	}
	for _, want := range []string{"<title>a&lt;b&gt;.md</title>", "../../assets/default.css", `data-mermaid="https://cdn.example.com/mermaid.js"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in\n%s", want, body)
		}
	}
	for _, name := range []string{"custom.css", "highlight.css"} {
		if strings.Contains(body, name) {
			t.Errorf("%s linked without one configured", name)
		}
	}
	if res, _ := get(t, s.URL(3)+"../../highlight.css"); res.StatusCode != http.StatusNotFound {
		t.Errorf("highlight.css without one: status %d", res.StatusCode)
	}

	if res, _ := get(t, s.URL(4)); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown page: status %d", res.StatusCode)
	}
	if res, _ := get(t, s.URL(3)+"../../../wrongtoken/p/3/"); res.StatusCode != http.StatusNotFound {
		t.Errorf("wrong token: status %d", res.StatusCode)
	}
	if res, body := get(t, s.URL(3)+"../../assets/preview.js"); res.StatusCode != http.StatusOK || !strings.Contains(body, "EventSource") {
		t.Errorf("preview.js: status %d", res.StatusCode)
	}
}

func TestKaTeXAndHighlight(t *testing.T) {
	s := start(t)
	s.SetConfig(Config{
		MermaidURL:   "https://cdn.example.com/mermaid.js",
		KaTeXURL:     "https://cdn.example.com/katex/",
		HighlightCSS: ".chroma .kd { color: red }",
	})
	s.Update(1, Page{HTML: "<p>x</p>"})

	res, body := get(t, s.URL(1))
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self' https://cdn.example.com;") {
		t.Errorf("CSP = %q", csp)
	}
	for _, want := range []string{`data-katex="https://cdn.example.com/katex/"`, `href="../../highlight.css"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in\n%s", want, body)
		}
	}
	res, body = get(t, s.URL(1)+"../../highlight.css")
	if res.StatusCode != http.StatusOK || body != ".chroma .kd { color: red }" || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/css") {
		t.Errorf("highlight.css: status %d, %s, %q", res.StatusCode, res.Header.Get("Content-Type"), body)
	}

	s.SetConfig(Config{MermaidURL: "https://a.example.com/m.js", KaTeXURL: "https://b.example.com/k/", AsciidoctorURL: "https://c.example.com/a.js"})
	res, body = get(t, s.URL(1))
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self' https://a.example.com https://b.example.com https://c.example.com;") {
		t.Errorf("CSP = %q", csp)
	}
	if !strings.Contains(body, `data-asciidoctor="https://c.example.com/a.js"`) {
		t.Errorf("no asciidoctor.js in\n%s", body)
	}
}

func TestHighlight(t *testing.T) {
	s := start(t)
	post := func() (int, []string) {
		t.Helper()
		u, _ := url.Parse(s.URL(1) + "../../highlight")
		res, err := http.Post(u.ResolveReference(&url.URL{}).String(), "application/json",
			strings.NewReader(`[{"lang":"go","code":"x"},{"lang":"none","code":"<y>"}]`))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out []string
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}

	if code, out := post(); code != http.StatusOK || len(out) != 2 || out[0] != "" || out[1] != "" {
		t.Errorf("without Highlight: %d %q", code, out)
	}
	s.SetConfig(Config{Highlight: func(lang, code string) (string, bool) {
		return "<b>" + code + "</b>", lang == "go"
	}})
	if code, out := post(); code != http.StatusOK || len(out) != 2 || out[0] != "<b>x</b>" || out[1] != "" {
		t.Errorf("with Highlight: %d %q", code, out)
	}
}

func TestHostCheck(t *testing.T) {
	s := start(t)
	s.Update(1, Page{})
	req, _ := http.NewRequest("GET", s.URL(1), nil)
	req.Host = "evil.example.com"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("status %d, want 403", res.StatusCode)
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "img.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(secret) })

	s := start(t)
	s.Update(1, Page{Dir: dir})
	if res, body := get(t, s.URL(1)+"files/img.png"); res.StatusCode != http.StatusOK || body != "png" {
		t.Errorf("img.png: status %d body %q", res.StatusCode, body)
	}
	for _, path := range []string{"files/sub", "files/..%2fsecret.txt", "files/%2e%2e/secret.txt", "files/missing.png"} {
		if res, body := get(t, s.URL(1)+path); res.StatusCode == http.StatusOK {
			t.Errorf("%s served: %q", path, body)
		}
	}
}

type stream struct {
	r *bufio.Reader
}

func (s stream) next(t *testing.T) (string, map[string]any) {
	t.Helper()
	var name, data string
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && name != "":
			var m map[string]any
			if err := json.Unmarshal([]byte(data), &m); err != nil {
				t.Fatalf("event %s: data %q: %v", name, data, err)
			}
			return name, m
		}
	}
}

func events(t *testing.T, s *Server, id int64) stream {
	t.Helper()
	res, err := http.Get(s.URL(id) + "events")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	return stream{bufio.NewReader(res.Body)}
}

func TestEvents(t *testing.T) {
	s := start(t)
	s.Update(1, Page{Title: "t", HTML: "<p>one</p>", Lines: true})
	s.SetCursor(1, 5, 10)
	ev := events(t, s, 1)

	if name, d := ev.next(t); name != "render" || d["html"] != "<p>one</p>" || d["lines"] != true || d["title"] != "t" {
		t.Errorf("first event %s %v", name, d)
	}
	if name, d := ev.next(t); name != "cursor" || d["line"] != 5.0 || d["total"] != 10.0 {
		t.Errorf("second event %s %v", name, d)
	}

	s.Update(1, Page{HTML: "two"})
	if name, d := ev.next(t); name != "render" || d["html"] != "two" {
		t.Errorf("after Update: %s %v", name, d)
	}

	s.Remove(1)
	if name, _ := ev.next(t); name != "close" {
		t.Errorf("after Remove: %s", name)
	}
}

func TestCustomCSS(t *testing.T) {
	css := filepath.Join(t.TempDir(), "my.css")
	if err := os.WriteFile(css, []byte("body{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := start(t)
	s.SetConfig(Config{CustomCSS: css, DisableDefaultCSS: true})
	s.Update(1, Page{})

	_, body := get(t, s.URL(1))
	if strings.Contains(body, "default.css") || !strings.Contains(body, `id="custom-css"`) {
		t.Errorf("stylesheets wrong:\n%s", body)
	}
	if res, body := get(t, s.URL(1)+"../../custom.css"); res.StatusCode != http.StatusOK || body != "body{}" {
		t.Errorf("custom.css: status %d body %q", res.StatusCode, body)
	}

	ev := events(t, s, 1)
	if name, _ := ev.next(t); name != "render" {
		t.Fatalf("first event %s", name)
	}
	// Let the watcher see the file once before it changes.
	time.Sleep(1500 * time.Millisecond)
	if err := os.WriteFile(css, []byte("body{color:red}"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		name, _ := ev.next(t)
		done <- name
	}()
	select {
	case name := <-done:
		if name != "css" {
			t.Errorf("after the change: %s", name)
		}
	case <-time.After(5 * time.Second):
		t.Error("no css event")
	}
}

func TestCloseEndsStreams(t *testing.T) {
	s, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	s.Update(1, Page{})
	ev := events(t, s, 1)
	ev.next(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Errorf("Close with an open stream: %v", err)
	}
}
