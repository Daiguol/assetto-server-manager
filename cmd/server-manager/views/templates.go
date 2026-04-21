package views

import (
	"bytes"
	"html/template"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
)

type TemplateLoader struct {
	pages, partials []string
}

func (t *TemplateLoader) Init() error {
	return fs.WalkDir(embedded, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		key := "/" + filepath.ToSlash(p)
		if strings.HasPrefix(key, "/pages/") {
			t.pages = append(t.pages, key)
		} else if strings.HasPrefix(key, "/partials/") {
			t.partials = append(t.partials, key)
		}
		return nil
	})
}

func (t *TemplateLoader) fileContents(name string) (string, error) {
	f, err := embedded.Open(strings.TrimPrefix(name, "/"))
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := new(bytes.Buffer)

	_, err = io.Copy(buf, f)

	if err != nil {
		return "", nil
	}

	return buf.String(), nil
}

func (t *TemplateLoader) Templates(funcs template.FuncMap) (map[string]*template.Template, error) {
	templates := make(map[string]*template.Template)

	templateData, err := t.fileContents("/layout/base.html")

	if err != nil {
		return nil, err
	}

	for _, partial := range t.partials {
		contents, err := t.fileContents(partial)

		if err != nil {
			return nil, err
		}

		templateData += contents
	}

	for _, page := range t.pages {
		pageData := templateData

		pageText, err := t.fileContents(page)

		if err != nil {
			return nil, err
		}

		pageData += pageText

		t, err := template.New(page).Funcs(funcs).Parse(pageData)

		if err != nil {
			return nil, err
		}

		templates[strings.TrimPrefix(filepath.ToSlash(page), "/pages/")] = t
	}

	return templates, nil
}
