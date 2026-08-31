package web

import (
	"fmt"
	"net/http"
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	"cnpg-manager/internal/kube"
)

type backupView struct {
	Name       string `json:"name"`
	Method     string `json:"method"`
	Phase      string `json:"phase"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

func toBackupView(b apiv1.Backup) backupView {
	v := backupView{Name: b.Name, Method: string(b.Spec.Method), Phase: string(b.Status.Phase)}
	if b.Status.StartedAt != nil && !b.Status.StartedAt.Time.IsZero() {
		v.StartedAt = b.Status.StartedAt.Format("2006-01-02 15:04:05Z07:00")
	}
	if b.Status.StoppedAt != nil && !b.Status.StoppedAt.Time.IsZero() {
		v.FinishedAt = b.Status.StoppedAt.Format("2006-01-02 15:04:05Z07:00")
	}
	return v
}

func (h *api) listBackups(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	backups, err := h.cs.ListBackups(r.Context(), cl.Namespace, cl.Name)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]backupView, 0, len(backups))
	for i := range backups {
		out = append(out, toBackupView(backups[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *api) createBackup(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	name := fmt.Sprintf("%s-backup-%d", cl.Name, time.Now().Unix())
	b := kube.BackupFor(cl, name)
	if err := h.cs.CreateBackup(r.Context(), b); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"created": name})
}
