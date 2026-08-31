package kube

import (
	"context"
	"testing"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func TestCRDWhitelist(t *testing.T) {
	for _, k := range []string{"Cluster", "Backup", "Database", "DatabaseRole", "Pooler",
		"ScheduledBackup", "ImageCatalog", "ClusterImageCatalog", "Publication", "Subscription"} {
		if !CRDNamespaced(k) && k != "ClusterImageCatalog" {
			t.Fatalf("expected %s namespaced", k)
		}
		if CRDNamespaced("ClusterImageCatalog") {
			t.Fatal("ClusterImageCatalog should be cluster-scoped")
		}
	}
	if CRDNamespaced("Bogus") {
		t.Fatal("bogus kind should not be namespaced-aware / should be absent")
	}
}

func TestListCRDRoundTrip(t *testing.T) {
	c := schemeBuilder()
	k := &Client{c: c}
	ctx := context.Background()
	for _, name := range []string{"b1", "b2"} {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Backup"})
		obj.SetName(name)
		obj.SetNamespace("db")
		if err := k.CreateCRD(ctx, "Backup", "db", obj); err != nil {
			t.Fatal(err)
		}
	}
	list, err := k.ListCRD(ctx, "Backup", "db")
	if err != nil || len(list) != 2 {
		t.Fatalf("list %d err %v", len(list), err)
	}
	one, err := k.GetCRD(ctx, "Backup", "db", "b1")
	if err != nil || one.GetName() != "b1" {
		t.Fatalf("get err %v", err)
	}
	one.Object["spec"] = map[string]any{"method": "barmanObjectStore"}
	if err := k.UpdateCRD(ctx, "Backup", "db", one); err != nil {
		t.Fatal(err)
	}
	got, _ := k.GetCRD(ctx, "Backup", "db", "b1")
	if got.Object["spec"].(map[string]any)["method"] != "barmanObjectStore" {
		t.Fatal("update did not persist spec")
	}
	if err := k.DeleteCRD(ctx, "Backup", "db", "b1"); err != nil {
		t.Fatal(err)
	}
	after, _ := k.ListCRD(ctx, "Backup", "db")
	if len(after) != 1 {
		t.Fatalf("expected 1 after delete, got %d", len(after))
	}
}

func TestListCRDClusterScoped(t *testing.T) {
	c := schemeBuilder()
	k := &Client{c: c}
	ctx := context.Background()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "ClusterImageCatalog"})
	obj.SetName("cat")
	// Default namespaced client: force all-namespaces list just exercises ns "".
	if err := k.CreateCRD(ctx, "ClusterImageCatalog", "", obj); err != nil {
		t.Fatal(err)
	}
	list, err := k.ListCRD(ctx, "ClusterImageCatalog", "")
	if err != nil || len(list) != 1 {
		t.Fatalf("cluster-scoped list %d err %v", len(list), err)
	}
}

func TestListCRDInvalidKind(t *testing.T) {
	c := schemeBuilder()
	k := &Client{c: c}
	if _, err := k.ListCRD(context.Background(), "Bogus", "db"); err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

func TestCreateAndDeleteDatabase(t *testing.T) {
	cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}}
	k := &Client{c: schemeBuilder()}
	if err := k.CreateDatabase(context.Background(), cl, "app", "owner1", "template0", ""); err != nil {
		t.Fatal(err)
	}
	got, err := k.GetCRD(context.Background(), "Database", "db", "app")
	if err != nil {
		t.Fatal(err)
	}
	if spec, _, _ := unstructured.NestedString(got.Object, "spec", "name"); spec != "app" {
		t.Fatalf("spec.name = %q", spec)
	}
	if p, _, _ := unstructured.NestedString(got.Object, "spec", "databaseReclaimPolicy"); p != "delete" {
		t.Fatalf("reclaim policy = %q, want delete", p)
	}
	if err := k.DeleteDatabase(context.Background(), cl, "app"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.GetCRD(context.Background(), "Database", "db", "app"); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestCreateAndDropDatabaseRole(t *testing.T) {
	cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}}
	k := &Client{c: schemeBuilder()}
	ctx := context.Background()
	if err := k.CreateManagedRole(ctx, cl, "app", "pg1-app", false, true); err != nil {
		t.Fatal(err)
	}
	got, err := k.GetCRD(ctx, "DatabaseRole", "db", "app")
	if err != nil {
		t.Fatal(err)
	}
	if spec, _, _ := unstructured.NestedString(got.Object, "spec", "name"); spec != "app" {
		t.Fatalf("spec.name = %q", spec)
	}
	if l, _, _ := unstructured.NestedBool(got.Object, "spec", "login"); !l {
		t.Fatal("login should be true")
	}
	if cdb, _, _ := unstructured.NestedBool(got.Object, "spec", "createdb"); !cdb {
		t.Fatal("createdb should be true")
	}
	if su, _, _ := unstructured.NestedBool(got.Object, "spec", "superuser"); su {
		t.Fatal("superuser should be false")
	}
	if p, _, _ := unstructured.NestedString(got.Object, "spec", "databaseRoleReclaimPolicy"); p != "delete" {
		t.Fatalf("reclaim policy = %q, want delete", p)
	}
	if sn, _, _ := unstructured.NestedString(got.Object, "spec", "passwordSecret", "name"); sn != "pg1-app" {
		t.Fatalf("passwordSecret.name = %q", sn)
	}
	if err := k.DropManagedRole(ctx, cl, "app"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.GetCRD(ctx, "DatabaseRole", "db", "app"); err == nil {
		t.Fatal("expected not found after drop")
	}
}

func TestPatchCRD(t *testing.T) {
	c := schemeBuilder()
	k := &Client{c: c}
	ctx := context.Background()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Cluster"})
	obj.SetName("pg")
	obj.SetNamespace("db")
	obj.Object["spec"] = map[string]any{"instances": int64(1), "imageName": "img:17"}
	if err := k.CreateCRD(ctx, "Cluster", "db", obj); err != nil {
		t.Fatal(err)
	}
	if err := k.PatchCRD(ctx, "Cluster", "db", "pg", map[string]any{"spec": map[string]any{"instances": int64(3)}}); err != nil {
		t.Fatal(err)
	}
	got, _ := k.GetCRD(ctx, "Cluster", "db", "pg")
	spec := got.Object["spec"].(map[string]any)
	if spec["instances"] != int64(3) {
		t.Fatalf("patch did not set instances: %v", spec["instances"])
	}
	if spec["imageName"] != "img:17" {
		t.Fatalf("merge patch clobbered sibling field: %v", spec)
	}
}
