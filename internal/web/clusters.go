package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	"cnpg-manager/internal/kube"
)

type clusterView struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Version    int32  `json:"version"`
	Phase      string `json:"phase"`
	Ready      int    `json:"readyInstances"`
	Total      int    `json:"instances"`
	Port       int32  `json:"port"`
	Databases  int    `json:"databases"`
	Roles      int    `json:"roles"`
	LastBackup string `json:"lastBackup,omitempty"`
	DBError    string `json:"dbError,omitempty"`
}

// pgMajorVersion extracts the PostgreSQL major version from the image reference
// (e.g. ".../postgresql:17.4" -> 17). CNPG models the version only via the
// container image, not as a structured spec field.
func pgMajorVersion(cl *apiv1.Cluster) int32 {
	img := cl.Status.Image
	if img == "" {
		img = cl.Spec.ImageName
	}
	i := strings.LastIndex(img, ":")
	if i < 0 {
		return 0
	}
	tag := img[i+1:]
	end := strings.IndexAny(tag, ".")
	if end < 0 {
		end = len(tag)
	}
	v, err := strconv.Atoi(tag[:end])
	if err != nil {
		return 0
	}
	return int32(v)
}

func enrich(ctx context.Context, h *api, cl *apiv1.Cluster) clusterView {
	v := clusterView{
		Name: cl.Name, Namespace: cl.Namespace, Phase: cl.Status.Phase,
		Ready: cl.Status.ReadyInstances, Total: cl.Status.Instances,
		Port: kube.ClusterPort(cl), Version: pgMajorVersion(cl), Databases: -1, Roles: -1,
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	p, err := h.connectPG(pctx, cl)
	if err != nil {
		v.DBError = "unreachable: " + err.Error()
		return v
	}
	defer p.Close()
	dbs, err := p.ListDatabases(pctx)
	if err != nil {
		v.DBError = err.Error()
		return v
	}
	v.Databases = len(dbs)
	roles, err := p.ListRoles(pctx)
	if err != nil {
		v.DBError = err.Error()
		return v
	}
	v.Roles = len(roles)
	if backups, err := h.cs.ListBackups(pctx, cl.Namespace, cl.Name); err == nil && len(backups) > 0 {
		var t time.Time
		for _, b := range backups {
			if b.Status.Phase == apiv1.BackupPhaseCompleted && b.Status.StoppedAt != nil && b.Status.StoppedAt.Time.After(t) {
				t = b.Status.StoppedAt.Time
			}
		}
		if !t.IsZero() {
			v.LastBackup = t.Format(time.RFC3339)
		}
	}
	return v
}

func (h *api) listClusters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clusters, err := h.cs.ListClusters(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]clusterView, len(clusters))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range clusters {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = enrich(ctx, h, &clusters[i])
		}(i)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, out)
}

func (h *api) getCluster(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, enrich(r.Context(), h, cl))
}
