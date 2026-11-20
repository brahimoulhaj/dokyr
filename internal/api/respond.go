package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"panel/internal/i18n"
)

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError sends {"error": {"code": ..., "message": ...}}. The message is written in
// English and sent in the language of the request when the catalog has it (internal/i18n);
// the code never changes.
func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	if l, ok := w.(interface{ Language() string }); ok {
		message = i18n.Translate(l.Language(), message)
	}
	body.Error.Code, body.Error.Message = code, message
	writeJSON(w, status, body)
}

// readJSON decodes a JSON body of at most 1 MiB, rejecting unknown fields and trailing data.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "request body must be valid JSON")
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "request body must hold a single JSON object")
		return false
	}
	return true
}

// tr is message in lang (internal/i18n), for text sent outside an error body.
func tr(lang, message string) string { return i18n.Translate(lang, message) }

// logLine is one line of a deployment log in lang. The panel's own lines are translated;
// what a build or a container printed (the "  | " lines) is left as it came.
func logLine(lang, text string) string {
	if lang == i18n.Default || strings.HasPrefix(text, "  | ") {
		return text
	}
	return i18n.Translate(lang, text)
}
