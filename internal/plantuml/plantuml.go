// Package plantuml builds the URLs a PlantUML server renders diagrams from.
package plantuml

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"strings"
)

var encoding = base64.NewEncoding("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-_").WithPadding(base64.NoPadding)

// Encode is PlantUML's text encoding: raw deflate, then its own base64.
func Encode(source string) string {
	var buf bytes.Buffer
	zw, _ := flate.NewWriter(&buf, flate.BestCompression)
	_, _ = zw.Write([]byte(source))
	_ = zw.Close()
	b := buf.Bytes()
	// The reference encoder pads the last group with zero bytes instead of
	// writing a short one; servers are only known to accept that form.
	for len(b)%3 != 0 {
		b = append(b, 0)
	}
	return encoding.EncodeToString(b)
}

// URL is the SVG of source on the server at base.
func URL(base, source string) string {
	return strings.TrimRight(base, "/") + "/svg/" + Encode(source)
}
