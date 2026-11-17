package handler

import (
	"net/http"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/server/render"
)

// showFormError answers a refused form. HTMX gets the error as a fragment,
// which the page puts in the notice area of the form; a plain form post
// gets the whole page again, with the message, the input it is about and
// what had been typed (in data.Form). The status code is the same both
// ways. An internal error is never shown inside the form.
func showFormError(rd *render.Renderer, w http.ResponseWriter, r *http.Request, page string, data PageData, err error) {
	if r.Header.Get("HX-Request") == "true" {
		rd.Fail(w, r, err)
		return
	}
	status, _, message, field := rd.Describe(err)
	if status == http.StatusInternalServerError {
		rd.Fail(w, r, err)
		return
	}
	data.FormError = message
	data.FormField = field
	rd.HTML(w, r, status, page, data)
}

// redirectTo sends the browser to another page after a successful form.
// HTMX follows the HX-Redirect header with a full page load; a plain form
// post gets 303, which makes the browser fetch the target with GET.
func redirectTo(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
