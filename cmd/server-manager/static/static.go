// Package static embeds the compiled frontend assets (JS, CSS, images,
// favicon) so they ship inside the server-manager binary.
package static

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed css img js favicon.ico
var embedded embed.FS

// FS returns the static assets as an http.FileSystem. When useLocal is true,
// files are served from the on-disk "static" directory relative to the
// working directory — used by the FILESYSTEM_HTML=true dev flow.
func FS(useLocal bool) http.FileSystem {
	if useLocal {
		return http.Dir("static")
	}
	return http.FS(embedded)
}

// FSMustByte returns the contents of the embedded file at name, or panics if
// it cannot be read. When useLocal is true the file is read from the on-disk
// "static" directory. The leading slash on name is optional.
func FSMustByte(useLocal bool, name string) []byte {
	b, err := fsByte(useLocal, name)
	if err != nil {
		panic(err)
	}
	return b
}

func fsByte(useLocal bool, name string) ([]byte, error) {
	if useLocal {
		return os.ReadFile(filepath.Join("static", filepath.FromSlash(name)))
	}
	// embed.FS uses slash-separated paths without a leading slash.
	return fs.ReadFile(embedded, stripLeadingSlash(name))
}

func stripLeadingSlash(name string) string {
	if len(name) > 0 && name[0] == '/' {
		return name[1:]
	}
	return name
}
