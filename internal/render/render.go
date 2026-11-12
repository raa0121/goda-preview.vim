// Package render converts a document into an HTML fragment for the preview.
package render

import (
	"fmt"
	"slices"
)

type Options struct {
	// Unsafe lets raw HTML in the document through (Markdown's inline HTML,
	// reStructuredText's raw directive, unrestricted Textile).
	Unsafe bool
	// HardWraps turns every newline of a Markdown paragraph into <br>.
	HardWraps bool
	// PlantUMLServer is the base URL of a PlantUML server. Empty leaves
	// plantuml code blocks as code, so that nothing leaves the machine
	// unless asked to.
	PlantUMLServer string
	// Highlight colours the code blocks of Markdown with chroma's classes,
	// which HighlightCSS styles.
	Highlight bool
}

type Result struct {
	HTML string
	// AsciiDoc, when set instead of HTML, is a document for the page to
	// convert itself.
	AsciiDoc string
	// Lines reports whether the block elements carry data-line attributes
	// (the 1-based source line they start at); without them the page can
	// only scroll in proportion to the cursor.
	Lines bool
}

var renderers = map[string]func(source string, opts Options) (Result, error){
	// Vim's own filetype, and that of vim-asciidoctor.
	"asciidoc":    renderAsciiDoc,
	"asciidoctor": renderAsciiDoc,
	"markdown":    renderMarkdown,
	"rst":         renderRST,
	"textile":     renderTextile,
}

// Filetypes are the Vim filetypes Render accepts, sorted.
func Filetypes() []string {
	names := make([]string, 0, len(renderers))
	for name := range renderers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func Supported(filetype string) bool {
	_, ok := renderers[filetype]
	return ok
}

func Render(filetype, source string, opts Options) (Result, error) {
	r, ok := renderers[filetype]
	if !ok {
		return Result{}, fmt.Errorf("unsupported filetype %q (supported: %v)", filetype, Filetypes())
	}
	return r(source, opts)
}
