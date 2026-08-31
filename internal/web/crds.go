package web

import (
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"cnpg-manager/internal/kube"
)

// validCRDKind rejects unknown kinds (400). ClusterImageCatalog is cluster-scoped
// yet still a valid kind; CRDGVR's whitelist covers it, so this is the single guard.
func validCRDKind(w http.ResponseWriter, kind string) (schema.GroupVersionResource, bool) {
	gvr, err := kube.CRDGVR(kind)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return schema.GroupVersionResource{}, false
	}
	return gvr, true
}

func (h *api) listCRDs(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := validCRDKind(w, kind); !ok {
		return
	}
	items, err := h.cs.ListCRD(r.Context(), kind, r.URL.Query().Get("ns"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, len(items))
	for i := range items {
		out[i] = items[i].Object
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *api) getCRD(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := validCRDKind(w, kind); !ok {
		return
	}
	obj, err := h.cs.GetCRD(r.Context(), kind, r.URL.Query().Get("ns"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, obj.Object)
}

type crdCreateReq struct {
	Name string         `json:"name"`
	Spec map[string]any `json:"spec"`
}

func (h *api) createCRD(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	gvr, ok := validCRDKind(w, kind)
	if !ok {
		return
	}
	if kind == "Cluster" {
		writeErr(w, http.StatusBadRequest, "Cluster create is not allowed; use the cluster endpoints")
		return
	}
	var in crdCreateReq
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	obj := &unstructured.Unstructured{}
	obj.Object = map[string]any{"metadata": map[string]any{"name": in.Name}}
	if in.Spec != nil {
		obj.Object["spec"] = in.Spec
	}
	gv := schema.GroupVersion{Group: gvr.Group, Version: gvr.Version}
	obj.SetGroupVersionKind(gv.WithKind(kind))
	if err := h.cs.CreateCRD(r.Context(), kind, r.URL.Query().Get("ns"), obj); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"created": in.Name})
}

func (h *api) updateCRD(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := validCRDKind(w, kind); !ok {
		return
	}
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	obj := &unstructured.Unstructured{Object: body}
	if err := h.cs.UpdateCRD(r.Context(), kind, r.URL.Query().Get("ns"), obj); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": r.PathValue("name")})
}

func (h *api) patchCRD(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := validCRDKind(w, kind); !ok {
		return
	}
	var patch map[string]any
	if err := decode(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.cs.PatchCRD(r.Context(), kind, r.URL.Query().Get("ns"), r.PathValue("name"), patch); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"patched": r.PathValue("name")})
}

func (h *api) deleteCRD(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, ok := validCRDKind(w, kind); !ok {
		return
	}
	if kind == "Cluster" {
		writeErr(w, http.StatusBadRequest, "Cluster delete is not allowed")
		return
	}
	if err := h.cs.DeleteCRD(r.Context(), kind, r.URL.Query().Get("ns"), r.PathValue("name")); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("name")})
}
