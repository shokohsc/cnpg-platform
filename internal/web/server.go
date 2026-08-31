package web

import (
	"bytes"
	"context"
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"cnpg-manager/internal/kube"
	"cnpg-manager/internal/pg"
)

// Compile-time guards: the concrete types must satisfy the web interfaces.
var (
	_ ClusterStore = (*kube.Client)(nil)
	_ PG           = (*pg.Server)(nil)
)

//go:embed all:dist
var dist embed.FS

// ClusterStore is the subset of kube.Client the web layer needs.
type ClusterStore interface {
	ListClusters(ctx context.Context) ([]apiv1.Cluster, error)
	GetCluster(ctx context.Context, ns, name string) (*apiv1.Cluster, error)
	ListBackups(ctx context.Context, ns, cluster string) ([]apiv1.Backup, error)
	CreateBackup(ctx context.Context, b *apiv1.Backup) error
	GetSecret(ctx context.Context, ns, name string) (map[string][]byte, error)
	UpsertSecret(ctx context.Context, ns, name string, data map[string]string) error
	DeleteSecret(ctx context.Context, ns, name string) error
	ListCRD(ctx context.Context, kind, ns string) ([]unstructured.Unstructured, error)
	GetCRD(ctx context.Context, kind, ns, name string) (*unstructured.Unstructured, error)
	CreateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error
	UpdateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error
	PatchCRD(ctx context.Context, kind, ns, name string, patch map[string]any) error
	DeleteCRD(ctx context.Context, kind, ns, name string) error
}

// PG is the slice of pg.Server used by the API (fake-able in tests).
type PG interface {
	ListDatabases(ctx context.Context) ([]pg.DBInfo, error)
	CreateDatabase(ctx context.Context, name, owner, template, encoding string) error
	DropDatabase(ctx context.Context, name string) error
	ListRoles(ctx context.Context) ([]pg.RoleInfo, error)
	CreateRole(ctx context.Context, name, password string, opts pg.CreateRoleOptions) error
	DropRole(ctx context.Context, name string) error
	RolePassword(ctx context.Context, name string) (string, error)
	RunSQL(ctx context.Context, db, stmt string, readOnly bool) (*pg.SQLResult, error)
	ListTables(ctx context.Context, db string) ([]pg.SchemaInfo, error)
	ListRows(ctx context.Context, db, schema, table string, limit, offset int) (*pg.TableResult, error)
	Close()
}

// PGFunc builds a PG connection for one cluster.
type PGFunc func(ctx context.Context, cl *apiv1.Cluster) (PG, error)

type api struct {
	cs        ClusterStore
	connectPG PGFunc
}

func New(cs ClusterStore, connectPG PGFunc) http.Handler {
	h := &api{cs: cs, connectPG: connectPG}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/clusters", h.listClusters)
	mux.HandleFunc("GET /api/clusters/{cluster}", h.getCluster)
	mux.HandleFunc("POST /api/clusters/{cluster}/databases", h.createDatabase)
	mux.HandleFunc("DELETE /api/clusters/{cluster}/databases/{db}", h.dropDatabase)
	mux.HandleFunc("GET /api/clusters/{cluster}/roles", h.listRoles)
	mux.HandleFunc("POST /api/clusters/{cluster}/roles", h.createRole)
	mux.HandleFunc("DELETE /api/clusters/{cluster}/roles/{role}", h.dropRole)
	mux.HandleFunc("POST /api/clusters/{cluster}/sql", h.runSQL)
	mux.HandleFunc("GET /api/clusters/{cluster}/tables", h.listTables)
	mux.HandleFunc("GET /api/clusters/{cluster}/tables/{schema}/{table}/rows", h.listRows)
	mux.HandleFunc("GET /api/clusters/{cluster}/backups", h.listBackups)
	mux.HandleFunc("POST /api/clusters/{cluster}/backups", h.createBackup)
	mux.HandleFunc("GET /api/clusters/{cluster}/connect", h.connInfo)
	mux.HandleFunc("PATCH /api/clusters/{cluster}/scale", h.scaleCluster)
	mux.HandleFunc("PATCH /api/clusters/{cluster}/config", h.editClusterConfig)
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /api/crds/{kind}", h.listCRDs)
	mux.HandleFunc("POST /api/crds/{kind}", h.createCRD)
	mux.HandleFunc("GET /api/crds/{kind}/{name}", h.getCRD)
	mux.HandleFunc("PUT /api/crds/{kind}/{name}", h.updateCRD)
	mux.HandleFunc("PATCH /api/crds/{kind}/{name}", h.patchCRD)
	mux.HandleFunc("DELETE /api/crds/{kind}/{name}", h.deleteCRD)
	// Catch-all: unmatched /api/* returns JSON (not the SPA fallback). Task 7
	// registers more-specific routes that take precedence over this pattern.
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not found")
	}))
	mux.Handle("/", spaFS(dist))
	return withLogging(mux)
}

// healthz reports liveness. The server is healthy whenever it can serve.
func (h *api) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// resolveCluster finds a cluster by name, disambiguated by ?ns=.
func (h *api) resolveCluster(ctx context.Context, r *http.Request) (*apiv1.Cluster, error) {
	name := r.PathValue("cluster")
	ns := r.URL.Query().Get("ns")
	if ns == "" {
		clusters, err := h.cs.ListClusters(ctx)
		if err != nil {
			return nil, err
		}
		var match *apiv1.Cluster
		for i := range clusters {
			if clusters[i].Name == name {
				if match != nil {
					return nil, errAmbiguous{name}
				}
				match = &clusters[i]
			}
		}
		if match == nil {
			return nil, errNotFound{name}
		}
		return match, nil
	}
	return h.cs.GetCluster(ctx, ns, name)
}

// spaFS serves embedded files, falling back to index.html for unknown paths.
func spaFS(assets fs.FS) http.Handler {
	sub, _ := fs.Sub(assets, "dist")
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api") {
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "" {
				name = "index.html"
			}
			if _, err := fs.Stat(sub, name); err != nil {
				index, err := sub.Open("index.html")
				if err == nil {
					defer index.Close()
					body, _ := io.ReadAll(index)
					http.ServeContent(w, r, "index.html", time.Now(), bytes.NewReader(body))
					return
				}
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

// writeError maps handler errors to HTTP status + JSON body.
func (h *api) writeError(w http.ResponseWriter, err error) {
	switch e := err.(type) {
	case errNotFound:
		writeErr(w, http.StatusNotFound, e.Error())
	case errAmbiguous:
		writeErr(w, http.StatusBadRequest, e.Error())
	case *pg.PGError:
		// 3D000 = invalid_catalog_name (cluster db doesn't exist) -> 404; DB errors -> 400.
		if e.Code == "3D000" {
			writeJSON(w, http.StatusNotFound, e)
			return
		}
		writeJSON(w, http.StatusBadRequest, e)
	default:
		if apierrors.IsNotFound(err) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if apierrors.IsForbidden(err) {
			writeErr(w, http.StatusForbidden, "RBAC forbids this operation")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// withLogging logs method, path, status and latency. Wraps the response to
// capture the status code. Never logs request bodies (they may contain secrets).
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &logWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, lw.code, time.Since(start))
	})
}

type logWriter struct {
	http.ResponseWriter
	code int
}

func (l *logWriter) WriteHeader(code int) {
	l.code = code
	l.ResponseWriter.WriteHeader(code)
}
