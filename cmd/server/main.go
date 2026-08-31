package main

import (
	"context"
	"net/http"
	"os"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	"cnpg-manager/internal/kube"
	"cnpg-manager/internal/pg"
	"cnpg-manager/internal/web"
)

func main() {
	k, err := kube.New()
	if err != nil {
		// kube.New already falls back; a hard failure means no config at all
		println("fatal: no kubernetes config:", err.Error())
		os.Exit(1)
	}

	connectPG := func(ctx context.Context, cl *apiv1.Cluster) (web.PG, error) {
		sec, err := k.GetSecret(ctx, cl.Namespace, kube.SuperuserSecret(cl))
		if err != nil {
			return nil, err
		}
		ca, _ := k.GetSecret(ctx, cl.Namespace, kube.CASecret(cl))
		meta := pg.Meta{
			Name:      cl.Name,
			Namespace: cl.Namespace,
			Host:      kube.RWService(cl),
			Port:      kube.ClusterPort(cl),
			Superuser: string(sec["username"]),
			Password:  string(sec["password"]),
			CA:        ca["ca.crt"],
		}
		return pg.Connect(ctx, meta)
	}

	h := web.New(k, connectPG)
	addr := ":" + envOr("PORT", "8080")
	println("cnpg-manager listening on", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		println("fatal:", err.Error())
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
