// Copyright (C) 2024 ScyllaDB

package framework

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"strconv"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	scyllaclientset "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"github.com/scylladb/scylla-operator/pkg/controllerhelpers"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/discovery"
	cacheddiscovery "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

type Cluster interface {
	Namespace() string
	Username() string
	FieldManager() string
	ClientConfig() *restclient.Config
	AdminClientConfig() *restclient.Config
	DiscoveryClient() *discovery.DiscoveryClient
	DynamicClient() dynamic.Interface
	DynamicAdminClient() dynamic.Interface
	KubeClient() *kubernetes.Clientset
	KubeAdminClient() *kubernetes.Clientset
	ScyllaClient() *scyllaclientset.Clientset
	ScyllaAdminClient() *scyllaclientset.Clientset
	CommonLabels() map[string]string
	SetUp(ctx context.Context)
	DumpArtifactsAndDestroy(ctx context.Context)
}

type cluster struct {
	name      string
	namespace *corev1.Namespace

	frameworkName string

	adminClientConfig *restclient.Config
	clientConfig      *restclient.Config
	username          string
}

func NewCluster(name, frameworkName string, restConfig *restclient.Config) Cluster {
	adminClientConfig := restclient.CopyConfig(restConfig)

	return &cluster{
		name: name,

		frameworkName: frameworkName,

		adminClientConfig: adminClientConfig,
		username:          "admin",
	}
}

var _ Cluster = &cluster{}

func (c *cluster) Namespace() string {
	o.Expect(c.namespace).NotTo(o.BeNil())
	return c.namespace.Name
}

func (c *cluster) Username() string {
	return c.username
}

func (c *cluster) FieldManager() string {
	h := sha512.Sum512([]byte(fmt.Sprintf("%s-%s", c.adminClientConfig.UserAgent, c.Namespace())))
	return base64.StdEncoding.EncodeToString(h[:])
}

func (c *cluster) ClientConfig() *restclient.Config {
	o.Expect(c.clientConfig).NotTo(o.BeNil())
	return c.clientConfig
}

func (c *cluster) AdminClientConfig() *restclient.Config {
	return c.adminClientConfig
}

func (c *cluster) DiscoveryClient() *discovery.DiscoveryClient {
	client, err := discovery.NewDiscoveryClientForConfig(c.ClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return client
}

func (c *cluster) DynamicClient() dynamic.Interface {
	client, err := dynamic.NewForConfig(c.ClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return client
}

func (c *cluster) DynamicAdminClient() dynamic.Interface {
	client, err := dynamic.NewForConfig(c.AdminClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return client
}

func (c *cluster) KubeClient() *kubernetes.Clientset {
	client, err := kubernetes.NewForConfig(c.ClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return client
}

func (c *cluster) KubeAdminClient() *kubernetes.Clientset {
	client, err := kubernetes.NewForConfig(c.AdminClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return client
}

func (c *cluster) ScyllaClient() *scyllaclientset.Clientset {
	client, err := scyllaclientset.NewForConfig(c.ClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return client
}

func (c *cluster) ScyllaAdminClient() *scyllaclientset.Clientset {
	client, err := scyllaclientset.NewForConfig(c.AdminClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return client
}

func (c *cluster) CommonLabels() map[string]string {
	return map[string]string{
		"e2e":       "scylla-operator",
		"framework": c.frameworkName,
	}
}

func (c *cluster) SetUp(ctx context.Context) {
	if c.namespace != nil {
		// cluster is already set up.
		return
	}

	By("Creating a new namespace")
	var ns *corev1.Namespace
	generateName := func() string {
		return names.SimpleNameGenerator.GenerateName(fmt.Sprintf("e2e-test-%s-", c.name))
	}
	name := generateName()
	sr := g.CurrentSpecReport()
	err := wait.PollImmediate(2*time.Second, 30*time.Second, func() (bool, error) {
		var err error
		// We want to know the name ahead, even if the api call fails.
		ns, err = c.KubeAdminClient().CoreV1().Namespaces().Create(
			ctx,
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: c.CommonLabels(),
					Annotations: map[string]string{
						"ginkgo-parallel-process": strconv.Itoa(sr.ParallelProcess),
						"ginkgo-full-text":        sr.FullText(),
					},
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				// regenerate on conflict
				Infof("Namespace name %q was already taken, generating a new name and retrying", name)
				name = generateName()
				return false, nil
			}
			return true, err
		}
		return true, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	Infof("Created namespace %q.", ns.Name)

	c.namespace = ns

	// Create user service account.
	userSA, err := c.KubeAdminClient().CoreV1().ServiceAccounts(ns.Name).Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: ServiceAccountName,
		},
	}, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	// Grant it edit permission in this namespace.
	_, err = c.KubeAdminClient().RbacV1().RoleBindings(ns.Name).Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: userSA.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				APIGroup:  corev1.GroupName,
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: userSA.Namespace,
				Name:      userSA.Name,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "admin",
		},
	}, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	// Create a service account token Secret for the user ServiceAccount.
	userSATokenSecret, err := c.KubeAdminClient().CoreV1().Secrets(ns.Name).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: ServiceAccountTokenSecretName,
			Annotations: map[string]string{
				corev1.ServiceAccountNameKey: userSA.Name,
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	By("Waiting for service account token Secret %q in namespace %q.", userSATokenSecret.Name, userSATokenSecret.Namespace)
	ctxUserSATokenSecret, ctxUserSATokenSecretCancel := context.WithTimeout(ctx, serviceAccountTokenSecretWaitTimeout)
	defer ctxUserSATokenSecretCancel()
	userSATokenSecret, err = WaitForServiceAccountTokenSecret(ctxUserSATokenSecret, c.KubeAdminClient().CoreV1(), userSATokenSecret.Namespace, userSATokenSecret.Name)
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(userSATokenSecret.Data).To(o.HaveKey(corev1.ServiceAccountTokenKey))

	token := userSATokenSecret.Data[corev1.ServiceAccountTokenKey]
	o.Expect(token).NotTo(o.BeEmpty())

	// Create a restricted client using the user SA.
	c.clientConfig = restclient.AnonymousClientConfig(c.AdminClientConfig())
	c.clientConfig.BearerToken = string(token)

	// Wait for default ServiceAccount.
	By("Waiting for default ServiceAccount in namespace %q.", ns.Name)
	ctxSa, ctxSaCancel := context.WithTimeout(ctx, serviceAccountWaitTimeout)
	defer ctxSaCancel()
	_, err = WaitForServiceAccount(ctxSa, c.KubeAdminClient().CoreV1(), ns.Name, "default")
	o.Expect(err).NotTo(o.HaveOccurred())

	// Waits for the configmap kube-root-ca.crt containing CA trust bundle so that pods do not have to retry mounting
	// the config map because it creates noise and slows the startup.
	By("Waiting for kube-root-ca.crt in namespace %q.", ns.Name)
	_, err = controllerhelpers.WaitForConfigMapState(
		ctx,
		c.KubeClient().CoreV1().ConfigMaps(ns.Name),
		"kube-root-ca.crt",
		controllerhelpers.WaitForStateOptions{},
		func(configMap *corev1.ConfigMap) (bool, error) {
			return true, nil
		},
	)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (c *cluster) deleteNamespace(ctx context.Context, ns *corev1.Namespace) {
	if c.namespace == nil {
		return
	}

	By("Destroying namespace %q.", ns.Name)
	var gracePeriod int64 = 0
	var propagation = metav1.DeletePropagationForeground
	err := c.KubeAdminClient().CoreV1().Namespaces().Delete(
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

	// We have deleted only the namespace object but it is still there with deletionTimestamp set.

	By("Waiting for namespace %q to be removed.", ns.Name)
	err = WaitForObjectDeletion(ctx, c.DynamicAdminClient(), corev1.SchemeGroupVersion.WithResource("namespaces"), "", ns.Name, &ns.UID)
	o.Expect(err).NotTo(o.HaveOccurred())
	klog.InfoS("Namespace removed.", "Namespace", ns.Name)
}

func (c *cluster) DumpArtifactsAndDestroy(ctx context.Context) {
	if c.namespace == nil {
		return
	}

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
			By("Keeping namespace %q for debugging", c.Namespace())
			return
		}

		c.deleteNamespace(ctx, c.namespace)
		c.namespace = nil
		c.clientConfig = nil
	}()

	// Print events if the test failed.
	if g.CurrentSpecReport().Failed() {
		By(fmt.Sprintf("Collecting events from namespace %q.", c.namespace.Name))
		DumpEventsInNamespace(ctx, c.KubeAdminClient(), c.namespace.Name)
	}

	// CI can't keep namespaces alive because it could get out of resources for the other tests
	// so we need to collect the namespaced dump before destroying the namespace.
	// Collecting artifacts even for successful runs helps to verify if it went
	// as expected and the amount of data is bearable.
	if len(TestContext.ArtifactsDir) != 0 {
		By(fmt.Sprintf("Collecting dumps from namespace %q.", c.namespace.Name))

		d := path.Join(TestContext.ArtifactsDir, "e2e")
		err := os.Mkdir(d, 0777)
		if err != nil && !os.IsExist(err) {
			o.Expect(err).NotTo(o.HaveOccurred())
		}

		err = DumpNamespace(ctx, cacheddiscovery.NewMemCacheClient(c.KubeAdminClient().Discovery()), c.DynamicAdminClient(), c.KubeAdminClient().CoreV1(), d, c.Namespace())
		o.Expect(err).NotTo(o.HaveOccurred())
	}
}
