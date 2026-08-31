package web

import (
	"net/http"
)

func (h *api) runSQL(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body struct {
		DB        string `json:"db"`
		Statement string `json:"statement"`
		ReadOnly  bool   `json:"readOnly"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.DB == "" || body.Statement == "" {
		writeErr(w, http.StatusBadRequest, "db and statement are required")
		return
	}
	p, err := h.connectPG(r.Context(), cl)
	if err != nil {
		h.writeError(w, err)
		return
	}
	defer p.Close()
	res, err := p.RunSQL(r.Context(), body.DB, body.Statement, body.ReadOnly)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
