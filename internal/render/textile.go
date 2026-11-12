package render

import gotextile "github.com/rcarmo/go-textile"

func renderTextile(source string, opts Options) (Result, error) {
	o := gotextile.DefaultOptions()
	o.Restricted = !opts.Unsafe
	o.HTML5 = true
	s, err := gotextile.TextileToHtmlWithOptions(source, o)
	if err != nil {
		return Result{}, err
	}
	return Result{HTML: s}, nil
}
