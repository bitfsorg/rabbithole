package main

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"time"

	"github.com/bitfsorg/libbitfs-go/metanet"
)

//go:embed templates
var templateFS embed.FS

// tmplFuncs are custom template functions.
var tmplFuncs = template.FuncMap{
	"formatTime": func(unix int64) string {
		if unix == 0 {
			return "-"
		}
		return time.Unix(unix, 0).Format("2006-01-02 15:04:05")
	},
	"formatSatoshis": func(sat int64) string {
		return formatSat(sat)
	},
	"truncHash": func(h string) string {
		if len(h) > 16 {
			return h[:8] + "..." + h[len(h)-8:]
		}
		return h
	},
	"accessName": func(a metanet.AccessLevel) string {
		switch a {
		case metanet.AccessPrivate:
			return "PRIVATE"
		case metanet.AccessFree:
			return "FREE"
		case metanet.AccessPaid:
			return "PAID"
		default:
			return fmt.Sprintf("UNKNOWN(%d)", a)
		}
	},
}

// Templates holds all parsed templates.
type Templates struct {
	pages map[string]*template.Template
}

// LoadTemplates parses all embedded templates.
// Each page template is combined with the base layout.
func LoadTemplates() (*Templates, error) {
	base, err := template.New("base.html").Funcs(tmplFuncs).ParseFS(templateFS, "templates/base.html")
	if err != nil {
		return nil, fmt.Errorf("parse base: %w", err)
	}

	pages := []string{"home.html", "block.html", "tx.html", "address.html", "search.html", "metanet.html", "spv.html", "method42.html"}
	t := &Templates{pages: make(map[string]*template.Template)}

	for _, page := range pages {
		clone, err := base.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone base for %s: %w", page, err)
		}
		_, err = clone.ParseFS(templateFS, "templates/"+page)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", page, err)
		}
		t.pages[page] = clone
	}
	return t, nil
}

// Render renders a page template into the writer.
func (t *Templates) Render(w io.Writer, page string, data interface{}) error {
	tmpl, ok := t.pages[page]
	if !ok {
		return fmt.Errorf("template not found: %s", page)
	}
	return tmpl.ExecuteTemplate(w, "base.html", data)
}
