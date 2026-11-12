// Command goda-preview is the binary of goda-preview.vim. Build it with
// :call goda_preview#build().
package main

import (
	"github.com/Shougo/goda.vim/host"

	"github.com/raa0121/goda-preview.vim/internal/preview"
)

func main() { host.Main(preview.New()) }
