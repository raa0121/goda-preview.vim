package render

// renderAsciiDoc leaves the conversion to asciidoctor.js in the page: no Go
// implementation of AsciiDoc is complete and maintained. The page also puts
// the data-line attributes on the blocks, from asciidoctor.js's source map.
func renderAsciiDoc(source string, _ Options) (Result, error) {
	return Result{AsciiDoc: source, Lines: true}, nil
}
