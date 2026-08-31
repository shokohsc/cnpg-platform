package kube

import (
	"context"
	"testing"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func schemeBuilder() client.Client {
	s := newScheme()
	c := fake.NewClientBuilder().WithScheme(s).Build()
	return c
}

func TestClusterPortDefault(t *testing.T) {
	cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "db"}}
	if p := ClusterPort(cl); p != 5432 {
		t.Fatalf("expected 5432, got %d", p)
	}
}

func TestClusterPortAnnotation(t *testing.T) {
	cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "db",
		Annotations: map[string]string{PortAnnotation: "15432"}}}
	if p := ClusterPort(cl); p != 15432 {
		t.Fatalf("expected 15432, got %d", p)
	}
}

func TestServiceNames(t *testing.T) {
	cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "db"}}
	if RWService(cl) != "pg-rw.db.svc" {
		t.Fatalf("rw service wrong: %s", RWService(cl))
	}
	if SuperuserSecret(cl) != "pg-superuser" || CASecret(cl) != "pg-ca" {
		t.Fatalf("secret names wrong: %s %s", SuperuserSecret(cl), CASecret(cl))
	}
}

func TestUpsertSecret(t *testing.T) {
	c := schemeBuilder()
	k := &Client{c: c}
	ctx := context.Background()
	if err := k.UpsertSecret(ctx, "db", "pg-role", map[string]string{"username": "app", "password": "x"}); err != nil {
		t.Fatal(err)
	}
	var sec corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: "db", Name: "pg-role"}, &sec); err != nil {
		t.Fatal(err)
	}
	if string(sec.Data["password"]) != "x" {
		t.Fatalf("password not stored")
	}
	if err := k.UpsertSecret(ctx, "db", "pg-role", map[string]string{"password": "y"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "db", Name: "pg-role"}, &sec); err != nil {
		t.Fatal(err)
	}
	if string(sec.Data["password"]) != "y" {
		t.Fatalf("upsert did not update")
	}
}

func TestListBackupsFilter(t *testing.T) {
	ctx := context.Background()
	mk := func(n string, cname string) *apiv1.Backup {
		b := &apiv1.Backup{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "db"},
			Spec: apiv1.BackupSpec{Cluster: apiv1.LocalObjectReference{Name: cname}}}
		b.Labels = map[string]string{ClusterLabelKey: cname}
		return b
	}
	s := newScheme()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(mk("b1", "pg"), mk("b2", "other"), mk("b3", "pg")).
		Build()
	k := &Client{c: c}
	got, err := k.ListBackups(ctx, "db", "pg")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 backups, got %d", len(got))
	}
}
