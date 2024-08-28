package framework

import (
	"context"
	"fmt"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"github.com/scylladb/scylla-operator/pkg/gather/collect"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/pointer"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	cacheddiscovery "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

type CleanupInterface interface {
	Print(ctx context.Context)
	Collect(ctx context.Context, artifactsDir string, ginkgoNamespace string)
	Cleanup(ctx context.Context)
}

type NamespaceCleaner struct {
	Client        kubernetes.Interface
	DynamicClient dynamic.Interface
	NS            *corev1.Namespace
}

var _ CleanupInterface = &NamespaceCleaner{}

func (nd *NamespaceCleaner) Print(ctx context.Context) {
	// Print events if the test failed.
	if g.CurrentSpecReport().Failed() {
		By(fmt.Sprintf("Collecting events from namespace %q.", nd.NS.Name))
		DumpEventsInNamespace(ctx, nd.Client, nd.NS.Name)
	}
}

func (nd *NamespaceCleaner) Collect(ctx context.Context, artifactsDir string, _ string) {
	By(fmt.Sprintf("Collecting dumps from namespace %q.", nd.NS.Name))

	err := DumpNamespace(ctx, cacheddiscovery.NewMemCacheClient(nd.Client.Discovery()), nd.DynamicClient, nd.Client.CoreV1(), artifactsDir, nd.NS.Name)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (nd *NamespaceCleaner) Cleanup(ctx context.Context) {
	By("Destroying namespace %q.", nd.NS.Name)
	err := nd.Client.CoreV1().Namespaces().Delete(
		ctx,
		nd.NS.Name,
		metav1.DeleteOptions{
			GracePeriodSeconds: pointer.Ptr[int64](0),
			PropagationPolicy:  pointer.Ptr(metav1.DeletePropagationForeground),
			Preconditions: &metav1.Preconditions{
				UID: &nd.NS.UID,
			},
		},
	)
	o.Expect(err).NotTo(o.HaveOccurred())

	// We have deleted only the namespace object, but it can still there with deletionTimestamp set.
	By("Waiting for namespace %q to be removed.", nd.NS.Name)
	err = WaitForObjectDeletion(ctx, nd.DynamicClient, corev1.SchemeGroupVersion.WithResource("namespaces"), "", nd.NS.Name, &nd.NS.UID)
	o.Expect(err).NotTo(o.HaveOccurred())
	klog.InfoS("Namespace removed.", "Namespace", nd.NS.Name)
}

type RestoreStrategy string

const (
	RestoreStrategyRecreate RestoreStrategy = "Recreate"
	RestoreStrategyUpdate   RestoreStrategy = "Update"
)

type RestoringCleaner struct {
	client        kubernetes.Interface
	dynamicClient dynamic.Interface
	resourceInfo  collect.ResourceInfo
	object        *unstructured.Unstructured
	strategy      RestoreStrategy
}

var _ CleanupInterface = &RestoringCleaner{}

func NewRestoringCleaner(ctx context.Context, client kubernetes.Interface, dynamicClient dynamic.Interface, resourceInfo collect.ResourceInfo, namespace string, name string, strategy RestoreStrategy) *RestoringCleaner {
	g.By(fmt.Sprintf("Snapshotting object %s %q", resourceInfo.Resource, naming.ManualRef(namespace, name)))

	if resourceInfo.Scope.Name() == meta.RESTScopeNameNamespace {
		o.Expect(namespace).NotTo(o.BeEmpty())
	}

	obj, err := dynamicClient.Resource(resourceInfo.Resource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		klog.InfoS("No existing object found", "GVR", resourceInfo.Resource, "Instance", naming.ManualRef(namespace, name))
		obj = &unstructured.Unstructured{
			Object: map[string]interface{}{},
		}
		obj.SetNamespace(namespace)
		obj.SetName(name)
		obj.SetUID("")
	} else {
		o.Expect(err).NotTo(o.HaveOccurred())
		klog.InfoS("Snapshotted object", "GVR", resourceInfo.Resource, "Instance", naming.ManualRef(namespace, name), "UID", obj.GetUID())
	}

	return &RestoringCleaner{
		client:        client,
		dynamicClient: dynamicClient,
		resourceInfo:  resourceInfo,
		object:        obj,
		strategy:      strategy,
	}
}

func (r *RestoringCleaner) getCleansedObject() *unstructured.Unstructured {
	obj := r.object.DeepCopy()
	obj.SetResourceVersion("")
	obj.SetUID("")
	obj.SetCreationTimestamp(metav1.Time{})
	obj.SetDeletionTimestamp(nil)
	return obj
}

func (r *RestoringCleaner) Print(ctx context.Context) {}

func (r *RestoringCleaner) Collect(ctx context.Context, clusterArtifactsDir string, ginkgoNamespace string) {
	artifactsDir := clusterArtifactsDir
	if len(artifactsDir) != 0 && r.resourceInfo.Scope.Name() == meta.RESTScopeNameRoot {
		// We have to prevent global object dumps being overwritten with each "It" block.
		artifactsDir = filepath.Join(artifactsDir, "cluster-scoped-per-ns", ginkgoNamespace)
	}

	By(fmt.Sprintf("Collecting global %s %q for namespace %q.", r.resourceInfo.Resource, naming.ObjRef(r.object), ginkgoNamespace))

	err := DumpResource(
		ctx,
		r.client.Discovery(),
		r.dynamicClient,
		r.client.CoreV1(),
		artifactsDir,
		&r.resourceInfo,
		r.object.GetNamespace(),
		r.object.GetName(),
	)
	if apierrors.IsNotFound(err) {
		klog.V(2).InfoS("Skipping object collection because it no longer exists", "Ref", naming.ObjRef(r.object), "Resource", r.resourceInfo.Resource)
	} else {
		o.Expect(err).NotTo(o.HaveOccurred())
	}
}

func (r *RestoringCleaner) DeleteObject(ctx context.Context, ignoreNotFound bool) {
	By("Deleting object %s %q.", r.resourceInfo.Resource, naming.ObjRef(r.object))
	err := r.dynamicClient.Resource(r.resourceInfo.Resource).Namespace(r.object.GetNamespace()).Delete(
		ctx,
		r.object.GetName(),
		metav1.DeleteOptions{
			GracePeriodSeconds: pointer.Ptr[int64](0),
			PropagationPolicy:  pointer.Ptr(metav1.DeletePropagationForeground),
		},
	)
	if apierrors.IsNotFound(err) && ignoreNotFound {
		return
	}
	o.Expect(err).NotTo(o.HaveOccurred())

	// We have deleted only the object, but it can still be there with deletionTimestamp set.
	By("Waiting for object %s %q to be removed.", r.resourceInfo.Resource, naming.ObjRef(r.object))
	err = WaitForObjectDeletion(ctx, r.dynamicClient, r.resourceInfo.Resource, r.object.GetNamespace(), r.object.GetName(), nil)
	o.Expect(err).NotTo(o.HaveOccurred())
	By("Object %s %q has been removed.", r.resourceInfo.Resource, naming.ObjRef(r.object))
}

func (r *RestoringCleaner) RecreateObject(ctx context.Context) {
	r.DeleteObject(ctx, true)

	_, err := r.dynamicClient.Resource(r.resourceInfo.Resource).Namespace(r.object.GetNamespace()).Create(ctx, r.getCleansedObject(), metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (r *RestoringCleaner) ReplaceObject(ctx context.Context) {
	var err error
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var freshObj *unstructured.Unstructured
		freshObj, err = r.dynamicClient.Resource(r.resourceInfo.Resource).Namespace(r.object.GetNamespace()).Get(ctx, r.object.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, err = r.dynamicClient.Resource(r.resourceInfo.Resource).Namespace(r.object.GetNamespace()).Create(ctx, r.getCleansedObject(), metav1.CreateOptions{})
			return err
		}

		obj := r.getCleansedObject()
		obj.SetResourceVersion(freshObj.GetResourceVersion())

		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = r.dynamicClient.Resource(r.resourceInfo.Resource).Namespace(obj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{})
		return err
	})
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (r *RestoringCleaner) RestoreObject(ctx context.Context) {
	By("Restoring original object %s %q.", r.resourceInfo.Resource, naming.ObjRef(r.object))
	switch r.strategy {
	case RestoreStrategyRecreate:
		r.RecreateObject(ctx)
	case RestoreStrategyUpdate:
		r.ReplaceObject(ctx)
	default:
		g.Fail(fmt.Sprintf("unexpected strategy %q", r.strategy))
	}
}

func (r *RestoringCleaner) Cleanup(ctx context.Context) {
	if len(r.object.GetUID()) == 0 {
		r.DeleteObject(ctx, true)
		return
	}

	r.RestoreObject(ctx)
}
