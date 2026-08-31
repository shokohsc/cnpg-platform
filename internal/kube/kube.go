package kube

import (
	"context"
	"fmt"
	"strconv"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
