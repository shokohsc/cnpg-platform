package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"

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

	// Configured spec values surfaced for the Overview edit panel.
	ImageName string        `json:"imageName,omitempty"`
	Storage   *storageView  `json:"storage,omitempty"`
	Resources *resourcesView `json:"resources,omitempty"`
	Postgres  *postgresView `json:"postgresql,omitempty"`
}

type storageView struct {
	Size string `json:"size,omitempty"`
}
type resourcesView struct {
	Requests resourcesValues `json:"requests,omitempty"`
}
type resourcesValues struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}
type postgresView struct {
	Parameters map[string]string `json:"parameters,omitempty"`
}

func quantityString(q corev1.ResourceList, n corev1.ResourceName) string {
	v, ok := q[n]
	if !ok {
		return ""
	}
	return v.String()
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
		ImageName: cl.Spec.ImageName,
	}
	if cl.Spec.StorageConfiguration.Size != "" {
		v.Storage = &storageView{Size: cl.Spec.StorageConfiguration.Size}
	}
	if len(cl.Spec.Resources.Requests) > 0 {
		v.Resources = &resourcesView{Requests: resourcesValues{
			CPU:    quantityString(cl.Spec.Resources.Requests, corev1.ResourceCPU),
			Memory: quantityString(cl.Spec.Resources.Requests, corev1.ResourceMemory),
		}}
	}
	if len(cl.Spec.PostgresConfiguration.Parameters) > 0 {
		v.Postgres = &postgresView{Parameters: cl.Spec.PostgresConfiguration.Parameters}
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

type scaleReq struct {
	Instances int `json:"instances"`
}

func (h *api) scaleCluster(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var in scaleReq
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if in.Instances < 1 {
		writeErr(w, http.StatusBadRequest, "instances must be >= 1")
		return
	}
	if err := h.cs.PatchCRD(r.Context(), "Cluster", cl.Namespace, cl.Name,
		map[string]any{"spec": map[string]any{"instances": in.Instances}}); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": in.Instances})
}

func (h *api) editClusterConfig(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var spec map[string]any
	if err := decode(r, &spec); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.cs.PatchCRD(r.Context(), "Cluster", cl.Namespace, cl.Name,
		map[string]any{"spec": spec}); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": cl.Name})
}
