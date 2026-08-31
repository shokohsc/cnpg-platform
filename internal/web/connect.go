package web

import (
	"net/http"

	"cnpg-manager/internal/kube"
	"cnpg-manager/internal/pg"
)

type connectInfo struct {
	User          string `json:"user"`
	DB            string `json:"db"`
	Host          string `json:"host"`
	Port          int32  `json:"port"`
	Password      string `json:"password"`
	URLDirect     string `json:"urlDirect"`
	URLVerifyFull string `json:"urlVerifyFull"`
}

func (h *api) connInfo(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	db := r.URL.Query().Get("db")
	role := r.URL.Query().Get("role")
	if db == "" {
		writeErr(w, http.StatusBadRequest, "db query param is required")
		return
	}
	sec, err := h.cs.GetSecret(r.Context(), cl.Namespace, kube.SuperuserSecret(cl))
	if err != nil {
		h.writeError(w, err)
		return
	}
	superUser := string(sec["username"])
	var user, password string
	if role == "" {
		role = superUser
	}
	if role == superUser {
		user, password = superUser, string(sec["password"])
	} else {
		rs, err := h.cs.GetSecret(r.Context(), cl.Namespace, kube.RoleSecret(cl, role))
		if err != nil {
			h.writeError(w, err)
			return
		}
		user, password = string(rs["username"]), string(rs["password"])
	}
	parts := pg.URLParts{User: user, Password: password, Host: kube.RWService(cl),
		Port: kube.ClusterPort(cl), DB: db, SSLMode: "require"}
	out := connectInfo{
		User: user, DB: db, Host: parts.Host, Port: parts.Port, Password: password,
		URLDirect: pg.ConnectURL(parts),
	}
	parts.SSLMode = "verify-full"
	out.URLVerifyFull = pg.ConnectURL(parts)
	writeJSON(w, http.StatusOK, out)
}
