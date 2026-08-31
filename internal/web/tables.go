package web

import (
	"net/http"
	"strconv"
)

func (h *api) listTables(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	db := r.URL.Query().Get("db")
	if db == "" {
		writeErr(w, http.StatusBadRequest, "db query param is required")
		return
	}
	p, err := h.connectPG(r.Context(), cl)
	if err != nil {
		h.writeError(w, err)
		return
	}
	defer p.Close()
	out, err := p.ListTables(r.Context(), db)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *api) listRows(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	p, err := h.connectPG(r.Context(), cl)
	if err != nil {
		h.writeError(w, err)
		return
	}
	defer p.Close()
	out, err := p.ListRows(r.Context(), q.Get("db"), q.Get("schema"), q.Get("table"), limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
