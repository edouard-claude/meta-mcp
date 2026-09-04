// Package web holds the minimal HTML pages the OAuth flow needs, embedded in
// the binary so there is nothing to deploy alongside it.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/http"
)

//go:embed *.html
var files embed.FS

// Page names, matching the template files.
const (
	PageLogin       = "login.html"
	PageError       = "error.html"
	PageDeauthorize = "deauthorize.html"
	PagePrivacy     = "privacy.html"
)

// LoginData fills the "connect your Facebook account" page.
type LoginData struct {
	Title        string
	AuthorizeURL template.URL
	PrivacyURL   string
}

// ErrorData fills the error page. Detail is optional.
type ErrorData struct {
	Title   string
	Message string
	Detail  string
}

// DeauthorizeData fills the data deletion confirmation page.
type DeauthorizeData struct {
	Title            string
	ConfirmationCode string
}

// PrivacyData fills the privacy policy page.
type PrivacyData struct {
	Title string
}

// pages holds every page pre-parsed with the shared layout. Each page file
// defines its own "content" block, so they are parsed separately rather than
// all into one template set.
var pages = func() map[string]*template.Template {
	out := map[string]*template.Template{}
	for _, name := range []string{PageLogin, PageError, PageDeauthorize, PagePrivacy} {
		out[name] = template.Must(template.ParseFS(files, "layout.html", name))
	}
	return out
}()

// Render writes a page. It buffers the render so a template failure cannot
// produce a half written response.
func Render(w http.ResponseWriter, status int, page string, data any) error {
	tmpl, ok := pages[page]
	if !ok {
		return fmt.Errorf("page inconnue: %s", page)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		return fmt.Errorf("render %s: %w", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}
