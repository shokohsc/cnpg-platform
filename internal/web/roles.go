package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"cnpg-manager/internal/kube"
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
	if body.Name == "superuser" {
		writeErr(w, http.StatusBadRequest, "role name 'superuser' collides with the cluster superuser secret")
		return
	}
	// Persist credentials first so CNPG can read the password secret when it
	// reconciles the managed role. If the managed-role write fails, undo the
	// secret so we don't leak a dangling credential.
	secretName := kube.RoleSecret(cl, body.Name)
	if err := h.cs.UpsertSecret(r.Context(), cl.Namespace,
		secretName, map[string]string{"username": body.Name, "password": body.Password}); err != nil {
		h.writeError(w, err)
		return
	}
	if err := h.cs.CreateManagedRole(r.Context(), cl, body.Name, secretName, body.Super, body.CreateDB); err != nil {
		_ = h.cs.DeleteSecret(r.Context(), cl.Namespace, secretName)
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
	if err := h.cs.DropManagedRole(r.Context(), cl, r.PathValue("role")); err != nil {
		h.writeError(w, err)
		return
	}
	if err := h.cs.DeleteSecret(r.Context(), cl.Namespace, kube.RoleSecret(cl, r.PathValue("role"))); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"dropped": r.PathValue("role")})
}
