// Package render writes HTTP responses: JSON for the API, HTML pages and
// fragments from templates, and errors. Every error of the application leaves through
// Renderer.Fail, so the format and the status codes are decided in one place.
package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

type Renderer struct {
	log       *slog.Logger
	dev       bool // true exposes technical error details in responses
	templates fs.FS
	views     views

	// errorPage builds the data of the "error" page for someone browsing.
	// It is set by the layer that knows what a page needs (who is logged
	// in, the CSRF token); without it, errors are answered in JSON.
	errorPage func(r *http.Request, status int, message string) any
}

// UseErrorPage makes errors of page navigations render the "error" page,
// with the data that build returns.
func (rd *Renderer) UseErrorPage(build func(r *http.Request, status int, message string) any) {
	rd.errorPage = build
}

// views holds the parsed templates: one set per page, and one set with the
// partials alone, from which HTML fragments are rendered for HTMX.
type views struct {
	pages    map[string]*template.Template
	partials *template.Template
}

func New(log *slog.Logger, dev bool) *Renderer {
	return &Renderer{log: log, dev: dev}
}

// LoadTemplates parses the HTML templates found in fsys, which must hold the
// directories layouts/, partials/ and pages/. Each file in pages/ becomes a
// page named after the file: pages/map.html is rendered with HTML(..., "map").
// Every {{define}} in partials/ can be rendered alone with Fragment.
func (rd *Renderer) LoadTemplates(fsys fs.FS) error {
	parsed, err := parseViews(fsys)
	if err != nil {
		return err
	}
	rd.templates = fsys
	rd.views = parsed
	return nil
}

// parseViews builds one template set per page: the layouts and partials are
// shared, and the page fills the blocks the layout declares.
func parseViews(fsys fs.FS) (views, error) {
	files, err := fs.Glob(fsys, "pages/*.html")
	if err != nil {
		return views{}, fmt.Errorf("find pages: %w", err)
	}
	if len(files) == 0 {
		return views{}, errors.New("no templates found in pages/")
	}

	parsed := views{pages: make(map[string]*template.Template, len(files))}
	for _, file := range files {
		tmpl, err := template.ParseFS(fsys, "layouts/*.html", "partials/*.html", file)
		if err != nil {
			return views{}, fmt.Errorf("parse %s: %w", file, err)
		}
		parsed.pages[strings.TrimSuffix(path.Base(file), ".html")] = tmpl
	}

	parsed.partials, err = template.ParseFS(fsys, "partials/*.html")
	if err != nil {
		return views{}, fmt.Errorf("parse partials: %w", err)
	}
	return parsed, nil
}

// current returns the templates to render with. In development they are
// parsed again on every request, so an edit shows up on reload without
// restarting the server.
func (rd *Renderer) current() (views, error) {
	if rd.dev && rd.templates != nil {
		return parseViews(rd.templates)
	}
	return rd.views, nil
}

// HTML renders a page inside the base layout. html/template escapes every
// value in data according to where it lands (text, attribute, URL), which is
// the protection against XSS in server-rendered pages.
func (rd *Renderer) HTML(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	current, err := rd.current()
	if err != nil {
		rd.Fail(w, r, err)
		return
	}
	tmpl, ok := current.pages[page]
	if !ok {
		rd.Fail(w, r, fmt.Errorf("template %q not found", page))
		return
	}
	rd.execute(w, r, status, tmpl, "base", data)
}

// Fragment renders one named template of partials/ without the layout: the
// piece of HTML that HTMX swaps into a page already open in the browser.
func (rd *Renderer) Fragment(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	current, err := rd.current()
	if err != nil {
		rd.Fail(w, r, err)
		return
	}
	if current.partials == nil || current.partials.Lookup(name) == nil {
		rd.Fail(w, r, fmt.Errorf("fragment %q not found", name))
		return
	}
	rd.execute(w, r, status, current.partials, name, data)
}

// execute renders to a buffer first, so a template error becomes a clean 500
// instead of half a page with status 200.
func (rd *Renderer) execute(w http.ResponseWriter, r *http.Request, status int, tmpl *template.Template, name string, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		rd.Fail(w, r, fmt.Errorf("render %s: %w", name, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// errorFragment is how an error reaches a page through HTMX: a paragraph
// that takes the place of the content that could not be loaded. data-field
// names the input the error is about, so the page can mark it.
var errorFragment = template.Must(template.New("error").Parse(
	`<p class="request-error" role="alert"{{if .Field}} data-field="{{.Field}}"{{end}}>{{.Message}}{{if .Detail}} <code>{{.Detail}}</code>{{end}}</p>`,
))

// errorBody is the single error format of the API:
//
//	{"error": {"code": "not_found", "message": "...", "detail": "..."}}
//
// Detail carries the technical cause and is only filled in development.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// JSON writes body with the given status code.
func (rd *Renderer) JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// BadRequest answers a request that could not even be parsed, such as a
// query parameter that is not a number.
func (rd *Renderer) BadRequest(w http.ResponseWriter, r *http.Request, field, message string) {
	rd.writeError(w, r, http.StatusBadRequest, errorBody{
		Code: "invalid_parameter", Message: message, Field: field,
	})
}

// Reject answers with a status and message chosen by the caller, for
// refusals that are decided outside the inner layers (a failed CSRF check).
func (rd *Renderer) Reject(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	rd.writeError(w, r, status, errorBody{Code: code, Message: message})
}

// Describe translates an error from the inner layers into what a client is
// told: the HTTP status, a stable code and a message for the end user. It is
// the single table of that translation; Fail uses it, and so do the handlers
// that show the message inside a page. Anything it does not recognize is an
// internal error, whose cause is never part of the message.
func (rd *Renderer) Describe(err error) (status int, code, message, field string) {
	var (
		invalid *domain.ValidationError
		tooSoon *domain.TooSoonError
	)
	switch {
	case errors.As(err, &invalid):
		return http.StatusBadRequest, "invalid_parameter", invalid.Message, invalid.Field
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not_found", "Recurso não encontrado.", ""
	case errors.Is(err, domain.ErrInvalidCredentials):
		return http.StatusUnauthorized, "invalid_credentials", "E-mail ou senha incorretos.", ""
	case errors.Is(err, domain.ErrUnauthenticated):
		return http.StatusUnauthorized, "unauthenticated", "Entre na sua conta para continuar.", ""
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "forbidden", "Você não tem permissão para acessar isto.", ""
	case errors.Is(err, domain.ErrEmailTaken):
		return http.StatusConflict, "email_taken", "Já existe uma conta com este e-mail.", ""
	case errors.Is(err, domain.ErrOfficialStation):
		return http.StatusConflict, "official_station",
			"Esta estação é coletada de um instituto: os dados vêm da fonte. Ela só pode ser ativada ou desativada.", ""
	case errors.Is(err, domain.ErrStationCodeTaken):
		return http.StatusConflict, "station_code_taken", "Já existe uma estação particular com este código.", "external_code"
	case errors.Is(err, domain.ErrKeyTaken):
		return http.StatusConflict, "key_taken", "Esta chave já está em uso por outra estação.", "public_key"
	case errors.Is(err, domain.ErrInvalidSignature):
		return http.StatusUnauthorized, "invalid_signature", "Assinatura ausente ou inválida para a chave desta estação.", ""
	case errors.Is(err, domain.ErrUplinkDisabled):
		return http.StatusForbidden, "uplink_disabled", "Esta estação não está configurada para enviar leituras.", ""
	case errors.As(err, &tooSoon):
		return http.StatusTooManyRequests, "too_soon", "Envio antes do fim do intervalo combinado para esta estação.", ""
	case errors.Is(err, domain.ErrTooManyRequests):
		return http.StatusTooManyRequests, "too_many_requests", "Muitas tentativas. Aguarde um minuto e tente de novo.", ""
	default:
		return http.StatusInternalServerError, "internal_error", "Erro interno do servidor.", ""
	}
}

// Fail translates an error from the inner layers into an HTTP response.
// Unknown errors become 500: they are logged with their cause, which
// reaches the client only in development.
func (rd *Renderer) Fail(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message, field := rd.Describe(err)
	body := errorBody{Code: code, Message: message, Field: field}
	if status == http.StatusInternalServerError {
		rd.log.Error("internal error", "method", r.Method, "path", r.URL.Path, "error", err)
		if rd.dev {
			body.Detail = err.Error()
		}
	}
	rd.writeError(w, r, status, body)
}

// writeError answers in the format the caller can use, always with the same
// status code: an HTML fragment for a request made by HTMX (it sends the
// HX-Request header), the error page for someone browsing (the request asks
// for text/html), and JSON for everything else, which is the API.
func (rd *Renderer) writeError(w http.ResponseWriter, r *http.Request, status int, body errorBody) {
	switch {
	case r.Header.Get("HX-Request") == "true":
		var buf bytes.Buffer
		if err := errorFragment.Execute(&buf, body); err != nil {
			http.Error(w, body.Message, status)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = buf.WriteTo(w)

	case rd.errorPage != nil && r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html"):
		current, err := rd.current()
		tmpl, ok := current.pages["error"]
		if err != nil || !ok {
			rd.JSON(w, status, map[string]errorBody{"error": body})
			return
		}
		// Rendered by hand rather than through HTML: a failure here must
		// not come back to writeError and loop.
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "base", rd.errorPage(r, status, body.Message)); err != nil {
			rd.log.Error("internal error", "method", r.Method, "path", r.URL.Path, "error", err)
			rd.JSON(w, status, map[string]errorBody{"error": body})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = buf.WriteTo(w)

	default:
		rd.JSON(w, status, map[string]errorBody{"error": body})
	}
}
