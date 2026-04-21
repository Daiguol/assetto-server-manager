// Package views embeds the HTML templates (layouts, pages, partials) used to
// render the web UI.
package views

import "embed"

//go:embed layout pages partials
var embedded embed.FS
