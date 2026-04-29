package changelog

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
)

// Render converts the raw markdown changelog into safe HTML for display in
// the admin UI. The bytes typically come from the embedded CHANGELOG.md in
// the root servermanager package.
func Render(src []byte) template.HTML {
	var buf bytes.Buffer
	if err := goldmark.Convert(src, &buf); err != nil {
		return template.HTML("")
	}
	return template.HTML(buf.String())
}
