package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"cnpg-manager/internal/kube"
	"cnpg-manager/internal/pg"
)

func (h *api) listRoles(w http.ResponseWriter, r *http.Request) {
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
	roles, err := p.ListRoles(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func randomPassword(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *api) createRole(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Super    bool   `json:"super"`
		CreateDB bool   `json:"createDB"`
		GrantDB  string `json:"grantDB"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Password == "" {
		p, err := randomPassword(16)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to generate password")
			return
		}
		body.Password = p
	}
	p, err := h.connectPG(r.Context(), cl)
	if err != nil {
		h.writeError(w, err)
		return
	}
	defer p.Close()
	if err := p.CreateRole(r.Context(), body.Name, body.Password,
		pg.CreateRoleOptions{Super: body.Super, CreateDB: body.CreateDB, GrantDB: body.GrantDB}); err != nil {
		h.writeError(w, err)
		return
	}
	if err := h.cs.UpsertSecret(r.Context(), cl.Namespace,
		kube.RoleSecret(cl, body.Name), map[string]string{"username": body.Name, "password": body.Password}); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": body.Name, "password": body.Password})
}

func (h *api) dropRole(w http.ResponseWriter, r *http.Request) {
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
	if err := p.DropRole(r.Context(), r.PathValue("role")); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"dropped": r.PathValue("role")})
}
