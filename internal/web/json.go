package web

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

type errNotFound struct{ name string }

func (e errNotFound) Error() string { return "cluster or object not found: " + e.name }

type errAmbiguous struct{ name string }

func (e errAmbiguous) Error() string { return "name " + e.name + " is ambiguous, pass ?ns=" }
