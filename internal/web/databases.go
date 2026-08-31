package web

import (
	"net/http"

	"cnpg-manager/internal/pg"
)

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
	// Guard system databases at the API boundary.
	if pg.IsSystemDB(body.Name) {
		writeErr(w, http.StatusBadRequest, body.Name+" is a system database")
		return
	}
	if err := h.cs.CreateDatabase(r.Context(), cl, body.Name, body.Owner, body.Template, body.Encoding); err != nil {
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
	if err := h.cs.DeleteDatabase(r.Context(), cl, r.PathValue("db")); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"dropped": r.PathValue("db")})
}
