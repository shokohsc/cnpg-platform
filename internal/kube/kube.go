package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	PortAnnotation  = "cnpg-manager.io/port"
	ClusterLabelKey = "cnpg.io/cluster"
)

type Client struct {
	c client.Client
}

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = apiv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

func New() (*Client, error) {
	var cfg *rest.Config
	c, err := config.GetConfig()
	if err != nil {
		// kubeconfig fallback for local dev
		rc, kerr := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(), nil).ClientConfig()
		if kerr != nil {
			return nil, fmt.Errorf("cluster config: %w", kerr)
		}
		cfg = rc
	} else {
		cfg = c
	}
	kc, err := client.New(cfg, client.Options{Scheme: newScheme()})
	if err != nil {
		return nil, err
	}
	return &Client{c: kc}, nil
}

func (k *Client) ListClusters(ctx context.Context) ([]apiv1.Cluster, error) {
	var list apiv1.ClusterList
	if err := k.c.List(ctx, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (k *Client) GetCluster(ctx context.Context, ns, name string) (*apiv1.Cluster, error) {
	cl := &apiv1.Cluster{}
	if err := k.c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, cl); err != nil {
		return nil, err
	}
	return cl, nil
}

func (k *Client) ListBackups(ctx context.Context, ns, cluster string) ([]apiv1.Backup, error) {
	var list apiv1.BackupList
	if err := k.c.List(ctx, &list, client.InNamespace(ns),
		client.MatchingLabels{ClusterLabelKey: cluster}); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (k *Client) CreateBackup(ctx context.Context, b *apiv1.Backup) error {
	return k.c.Create(ctx, b)
}

func (k *Client) GetSecret(ctx context.Context, ns, name string) (map[string][]byte, error) {
	sec := &corev1.Secret{}
	if err := k.c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, sec); err != nil {
		return nil, err
	}
	return sec.Data, nil
}

func (k *Client) UpsertSecret(ctx context.Context, ns, name string, data map[string]string) error {
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: ns, Name: name, Labels: map[string]string{"app": "cnpg-manager"}}}
	sec.Data = make(map[string][]byte, len(data))
	for key, v := range data {
		sec.Data[key] = []byte(v)
	}
	err := k.c.Create(ctx, sec)
	if err != nil && apierrors.IsAlreadyExists(err) {
		var cur corev1.Secret
		if gerr := k.c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &cur); gerr == nil {
			cur.Data = sec.Data
			return k.c.Update(ctx, &cur)
		}
	}
	return err
}

func (k *Client) DeleteSecret(ctx context.Context, ns, name string) error {
	err := k.c.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}})
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

const crdGroup = "postgresql.cnpg.io"
const crdVersion = "v1"

// CRD kinds supported by the generic layer, and whether each is cluster-scoped.
var crdScoped = map[string]bool{
	"ClusterImageCatalog": true,
}

func CRDNamespaced(kind string) bool {
	_, ok := crdKinds[kind]
	if !ok {
		return false
	}
	return !crdScoped[kind]
}

var crdKinds = map[string]struct{}{
	"Cluster": {}, "Backup": {}, "Database": {}, "DatabaseRole": {},
	"Pooler": {}, "ScheduledBackup": {}, "ImageCatalog": {},
	"ClusterImageCatalog": {}, "Publication": {}, "Subscription": {},
}

func CRDGVR(kind string) (schema.GroupVersionResource, error) {
	if _, ok := crdKinds[kind]; !ok {
		return schema.GroupVersionResource{}, fmt.Errorf("unsupported CRD kind: %s", kind)
	}
	return schema.GroupVersionResource{Group: crdGroup, Version: crdVersion, Resource: plural(kind)}, nil
}

func plural(kind string) string {
	return strings.ToLower(kind) + "s"
}

// nsFor returns the namespace arg to pass to kube calls (empty for cluster-scoped kinds).
func nsFor(kind, ns string) string {
	if !CRDNamespaced(kind) {
		return ""
	}
	return ns
}

func (k *Client) ListCRD(ctx context.Context, kind, ns string) ([]unstructured.Unstructured, error) {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return nil, err
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind + "List"})
	opts := []client.ListOption{}
	if n := nsFor(kind, ns); n != "" {
		opts = append(opts, client.InNamespace(n))
	}
	if err := k.c.List(ctx, list, opts...); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (k *Client) GetCRD(ctx context.Context, kind, ns, name string) (*unstructured.Unstructured, error) {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: nsFor(kind, ns), Name: name}, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (k *Client) CreateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return err
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	if n := nsFor(kind, ns); n != "" {
		obj.SetNamespace(n)
	} else {
		obj.SetNamespace("")
	}
	return k.c.Create(ctx, obj)
}

func (k *Client) UpdateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return err
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	if n := nsFor(kind, ns); n != "" {
		obj.SetNamespace(n)
	} else {
		obj.SetNamespace("")
	}
	return k.c.Update(ctx, obj)
}

func (k *Client) DeleteCRD(ctx context.Context, kind, ns, name string) error {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	obj.SetNamespace(nsFor(kind, ns))
	obj.SetName(name)
	err = k.c.Delete(ctx, obj)
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (k *Client) PatchCRD(ctx context.Context, kind, ns, name string, patch map[string]any) error {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return err
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	obj.SetName(name)
	if n := nsFor(kind, ns); n != "" {
		obj.SetNamespace(n)
	}
	return k.c.Patch(ctx, obj, client.RawPatch(types.MergePatchType, data))
}

func ClusterPort(cl *apiv1.Cluster) int32 {
	if v, ok := cl.Annotations[PortAnnotation]; ok {
		if p, err := strconv.ParseInt(v, 10, 32); err == nil {
			return int32(p)
		}
	}
	return 5432
}

func RWService(cl *apiv1.Cluster) string {
	return fmt.Sprintf("%s-rw.%s.svc", cl.Name, cl.Namespace)
}

func SuperuserSecret(cl *apiv1.Cluster) string { return cl.Name + "-superuser" }
func CASecret(cl *apiv1.Cluster) string        { return cl.Name + "-ca" }
func RoleSecret(cl *apiv1.Cluster, role string) string {
	return cl.Name + "-" + role
}

func BackupFor(cl *apiv1.Cluster, name string) *apiv1.Backup {
	b := &apiv1.Backup{ObjectMeta: metav1.ObjectMeta{
		Namespace: cl.Namespace, Name: name, Labels: map[string]string{ClusterLabelKey: cl.Name}}}
	b.Spec.Method = "barmanObjectStore"
	b.Spec.Cluster.Name = cl.Name
	return b
}
