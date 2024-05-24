// Copyright (C) 2024 ScyllaDB

package framework

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"os"
	"path"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cacheddiscovery "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

func deleteNamespace(ctx context.Context, adminClient kubernetes.Interface, dynamicAdminClient dynamic.Interface, ns *corev1.Namespace) {
	By("Destroying clusterNamespace %q.", ns.Name)
	var gracePeriod int64 = 0
	var propagation = metav1.DeletePropagationForeground
	err := adminClient.CoreV1().Namespaces().Delete(
		ctx,
		ns.Name,
		metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriod,
			PropagationPolicy:  &propagation,
			Preconditions: &metav1.Preconditions{
				UID: &ns.UID,
			},
		},
	)
	o.Expect(err).NotTo(o.HaveOccurred())

	// We have deleted only the clusterNamespace object but it is still there with deletionTimestamp set.

	By("Waiting for clusterNamespace %q to be removed.", ns.Name)
	err = WaitForObjectDeletion(ctx, dynamicAdminClient, corev1.SchemeGroupVersion.WithResource("namespaces"), "", ns.Name, &ns.UID)
	o.Expect(err).NotTo(o.HaveOccurred())
	klog.InfoS("ClusterNamespace removed.", "ClusterNamespace", ns.Name)
}

func collectAndDeleteNamespace(ctx context.Context, adminClient kubernetes.Interface, dynamicAdminClient dynamic.Interface, ns *corev1.Namespace) {
	defer func() {
		keepNamespace := false
		switch TestContext.DeleteTestingNSPolicy {
		case DeleteTestingNSPolicyNever:
			keepNamespace = true
		case DeleteTestingNSPolicyOnSuccess:
			if g.CurrentSpecReport().Failed() {
				keepNamespace = true
			}
		case DeleteTestingNSPolicyAlways:
		default:
		}

		if keepNamespace {
			By("Keeping clusterNamespace %q for debugging", ns.Name)
			return
		}

		deleteNamespace(ctx, adminClient, dynamicAdminClient, ns)

	}()

	// Print events if the test failed.
	if g.CurrentSpecReport().Failed() {
		By(fmt.Sprintf("Collecting events from clusterNamespace %q.", ns.Name))
		DumpEventsInNamespace(ctx, adminClient, ns.Name)
	}

	// CI can't keep namespaces alive because it could get out of resources for the other tests
	// so we need to collect the namespaced dump before destroying the clusterNamespace.
	// Collecting artifacts even for successful runs helps to verify if it went
	// as expected and the amount of data is bearable.
	if len(TestContext.ArtifactsDir) != 0 {
		// FIXME
		By(fmt.Sprintf("Collecting dumps from clusterNamespace %q.", ns.Name))

		d := path.Join(TestContext.ArtifactsDir, "e2e")
		err := os.Mkdir(d, 0777)
		if err != nil && !os.IsExist(err) {
			o.Expect(err).NotTo(o.HaveOccurred())
		}

		err = DumpNamespace(ctx, cacheddiscovery.NewMemCacheClient(adminClient.Discovery()), dynamicAdminClient, adminClient.CoreV1(), d, ns.Name)
		o.Expect(err).NotTo(o.HaveOccurred())
	}
}

type ClusterInterface interface {
	FullClientInterface
	Namespace() string
	FieldManager() string
}

type createNSFunc func(ctx context.Context, adminClient kubernetes.Interface, adminClientConfig *restclient.Config) (*corev1.Namespace, Client)

type Cluster struct {
	name string

	createNS           createNSFunc
	namespace          *corev1.Namespace
	namespacesToDelete []*corev1.Namespace

	FullClient
}

var _ FullClientInterface = &Cluster{}
var _ ClusterInterface = &Cluster{}

func NewCluster(name string, restConfig *restclient.Config, createNS createNSFunc) *Cluster {
	adminClientConfig := restclient.CopyConfig(restConfig)

	return &Cluster{
		name: name,

		createNS:           createNS,
		namespace:          nil,
		namespacesToDelete: nil,

		FullClient: FullClient{
			AdminClient: AdminClient{
				Client: Client{
					Config: adminClientConfig,
				},
			},
			Client: Client{
				Config: nil,
			},
		},
	}
}

func (c *Cluster) DefaultNamespaceIfAny() string {
	o.Expect(c.namespace).NotTo(o.BeNil())
	return c.namespace.Name
}

func (c *Cluster) Namespace() string {
	return c.namespace.Name
}

func (c *Cluster) FieldManager() string {
	h := sha512.Sum512([]byte(fmt.Sprintf("%s-%s", c.ClientConfig().UserAgent, c.Namespace())))
	return base64.StdEncoding.EncodeToString(h[:])
}

func (c *Cluster) CreateUserNamespace(ctx context.Context) (*corev1.Namespace, Client) {
	ns, nsClient := c.createNS(ctx, c.KubeAdminClient(), c.ClientConfig())

	c.namespacesToDelete = append(c.namespacesToDelete, ns)

	return ns, nsClient
}

func (c *Cluster) BeforeEach(ctx context.Context) {
	ns, nsClient := c.CreateUserNamespace(ctx)

	c.namespace = ns
	c.FullClient.Client = nsClient
}

func (c *Cluster) Cleanup(ctx context.Context) {
	for _, ns := range c.namespacesToDelete {
		collectAndDeleteNamespace(ctx, c.KubeAdminClient(), c.DynamicAdminClient(), ns)
	}
}

func (c *Cluster) AfterEach(ctx context.Context) {
	c.namespace = nil
	c.Cleanup(ctx)
}
