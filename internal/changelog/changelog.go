package changelog

import (
	"html/template"

	"github.com/russross/blackfriday"
)

// Render converts the raw markdown changelog into safe HTML for display in
// the admin UI. The bytes typically come from the embedded CHANGELOG.md in
// the root servermanager package.
func Render(src []byte) template.HTML {
	return template.HTML(blackfriday.Run(src))
}
