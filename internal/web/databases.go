package web

import (
	"net/http"
)

var systemDatabases = map[string]bool{"postgres": true, "template0": true, "template1": true}

func isSystemDatabase(name string) bool { return systemDatabases[name] }

func (h *api) createDatabase(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body struct {
		Name     string `json:"name"`
		Owner    string `json:"owner"`
		Template string `json:"template"`
		Encoding string `json:"encoding"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	// Guard system databases at the API boundary (pg.Server also rejects them).
	if isSystemDatabase(body.Name) {
		writeErr(w, http.StatusBadRequest, body.Name+" is a system database")
		return
	}
	p, err := h.connectPG(r.Context(), cl)
	if err != nil {
		h.writeError(w, err)
		return
	}
	defer p.Close()
	if err := p.CreateDatabase(r.Context(), body.Name, body.Owner, body.Template, body.Encoding); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"created": body.Name})
}

func (h *api) dropDatabase(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	p, err := h.connectPG(r.Context(), cl)
	if err != nil {
		h.writeError(w, err)
		return
	}
	defer p.Close()
	if err := p.DropDatabase(r.Context(), r.PathValue("db")); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"dropped": r.PathValue("db")})
}
