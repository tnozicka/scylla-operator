package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	scyllav1client "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned/typed/scylla/v1"
	scyllav1informers "github.com/scylladb/scylla-operator/pkg/client/scylla/informers/externalversions/scylla/v1"
	scyllav1listers "github.com/scylladb/scylla-operator/pkg/client/scylla/listers/scylla/v1"
	"github.com/scylladb/scylla-operator/pkg/resource"
	"github.com/scylladb/scylla-operator/pkg/scheme"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/component-base/metrics/prometheus/ratelimiter"
	"k8s.io/klog/v2"
)

var (
	ControllerName           = "ScyllaClusterController"
	KeyFunc                  = cache.DeletionHandlingMetaNamespaceKeyFunc
	controllerGVK            = scyllav1.GroupVersion.WithKind("ScyllaCluster")
	statefulSetControllerGVK = appsv1.SchemeGroupVersion.WithKind("StatefulSet")
	// maxSyncDuration enforces preemption. Do not raise the value! Controllers shouldn't actively wait,
	// but rather use the queue.
	maxSyncDuration = 30 * time.Second
)

type ScyllaClusterController struct {
	operatorImage string

	kubeClient   kubernetes.Interface
	scyllaClient scyllav1client.ScyllaV1Interface

	podLister         corev1listers.PodLister
	serviceLister     corev1listers.ServiceLister
	statefulSetLister appsv1listers.StatefulSetLister
	scyllaLister      scyllav1listers.ScyllaClusterLister

	cachesToSync []cache.InformerSynced

	eventRecorder record.EventRecorder

	queue workqueue.RateLimitingInterface
}

func NewScyllaClusterController(
	kubeClient kubernetes.Interface,
	scyllaClient scyllav1client.ScyllaV1Interface,
	podInformer corev1informers.PodInformer,
	serviceInformer corev1informers.ServiceInformer,
	statefulSetInformer appsv1informers.StatefulSetInformer,
	scyllaClusterInformer scyllav1informers.ScyllaClusterInformer,
	operatorImage string,
) (*ScyllaClusterController, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&corev1client.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})

	if kubeClient.CoreV1().RESTClient().GetRateLimiter() != nil {
		err := ratelimiter.RegisterMetricAndTrackRateLimiterUsage(
			"scyllacluster_controller",
			kubeClient.CoreV1().RESTClient().GetRateLimiter(),
		)
		if err != nil {
			return nil, err
		}
	}

	scc := &ScyllaClusterController{
		operatorImage: operatorImage,

		kubeClient:   kubeClient,
		scyllaClient: scyllaClient,

		podLister:         podInformer.Lister(),
		serviceLister:     serviceInformer.Lister(),
		statefulSetLister: statefulSetInformer.Lister(),
		scyllaLister:      scyllaClusterInformer.Lister(),

		cachesToSync: []cache.InformerSynced{
			podInformer.Informer().HasSynced,
			serviceInformer.Informer().HasSynced,
			statefulSetInformer.Informer().HasSynced,
			scyllaClusterInformer.Informer().HasSynced,
		},

		eventRecorder: eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "scyllacluster-controller"}),

		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "scyllacluster"),
	}

	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    scc.addService,
		UpdateFunc: scc.updateService,
		DeleteFunc: scc.deleteService,
	})

	// We need pods events to know if a pod is ready after replace operation.
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    scc.addPod,
		UpdateFunc: scc.updatePod,
	})

	statefulSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    scc.addStatefulSet,
		UpdateFunc: scc.updateStatefulSet,
		DeleteFunc: scc.deleteStatefulSet,
	})

	scyllaClusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    scc.addScyllaCluster,
		UpdateFunc: scc.updateScyllaCluster,
		DeleteFunc: scc.deleteScyllaCluster,
	})

	return scc, nil
}

func (scc *ScyllaClusterController) processNextItem(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, maxSyncDuration)
	defer cancel()

	key, quit := scc.queue.Get()
	if quit {
		return false
	}
	defer scc.queue.Done(key)

	err := scc.sync(ctx, key.(string))
	// TODO: Do smarter filtering then just Reduce to handle cases like 2 conflict errors.
	err = utilerrors.Reduce(err)
	switch {
	case err == nil:
		scc.queue.Forget(key)
		return true

	case apierrors.IsConflict(err):
		klog.V(2).InfoS("Hit conflict, will retry in a bit", "Key", key, "Error", err)

	case apierrors.IsAlreadyExists(err):
		klog.V(2).InfoS("Hit already exists, will retry in a bit", "Key", key, "Error", err)

	default:
		utilruntime.HandleError(fmt.Errorf("syncing key '%v' failed: %v", key, err))
	}

	scc.queue.AddRateLimited(key)

	return true
}

func (scc *ScyllaClusterController) runWorker(ctx context.Context) {
	for scc.processNextItem(ctx) {
	}
}

func (scc *ScyllaClusterController) Run(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()

	klog.InfoS("Starting controller", "controller", "ScyllaCluster")

	var wg sync.WaitGroup
	defer func() {
		klog.InfoS("Shutting down controller", "controller", "ScyllaCluster")
		scc.queue.ShutDown()
		wg.Wait()
		klog.InfoS("Shut down controller", "controller", "ScyllaCluster")
	}()

	ok := cache.WaitForNamedCacheSync(ControllerName, ctx.Done(), scc.cachesToSync...)
	if !ok {
		return
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wait.UntilWithContext(ctx, scc.runWorker, time.Second)
		}()
	}

	<-ctx.Done()
}

func (scc *ScyllaClusterController) resolveScyllaClusterController(obj metav1.Object) *scyllav1.ScyllaCluster {
	controllerRef := metav1.GetControllerOf(obj)
	if controllerRef == nil {
		return nil
	}

	if controllerRef.Kind != controllerGVK.Kind {
		return nil
	}

	sc, err := scc.scyllaLister.ScyllaClusters(obj.GetNamespace()).Get(controllerRef.Name)
	if err != nil {
		return nil
	}

	if sc.UID != controllerRef.UID {
		return nil
	}

	return sc
}

func (scc *ScyllaClusterController) resolveStatefulSetController(obj metav1.Object) *appsv1.StatefulSet {
	controllerRef := metav1.GetControllerOf(obj)
	if controllerRef == nil {
		return nil
	}

	if controllerRef.Kind != statefulSetControllerGVK.Kind {
		return nil
	}

	sts, err := scc.statefulSetLister.StatefulSets(obj.GetNamespace()).Get(controllerRef.Name)
	if err != nil {
		return nil
	}

	if sts.UID != controllerRef.UID {
		return nil
	}

	return sts
}

func (scc *ScyllaClusterController) resolveScyllaClusterControllerThroughStatefulSet(obj metav1.Object) *scyllav1.ScyllaCluster {
	sts := scc.resolveStatefulSetController(obj)
	if sts == nil {
		return nil
	}

	sc := scc.resolveScyllaClusterController(sts)
	if sc == nil {
		return nil
	}

	return sc
}

func (scc *ScyllaClusterController) enqueue(sc *scyllav1.ScyllaCluster) {
	key, err := KeyFunc(sc)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", sc, err))
		return
	}

	klog.V(4).InfoS("Enqueuing", "ScyllaCluster", klog.KObj(sc))
	scc.queue.Add(key)
}

func (scc *ScyllaClusterController) enqueueOwner(obj metav1.Object) {
	sc := scc.resolveScyllaClusterController(obj)
	if sc == nil {
		return
	}

	gvk, err := resource.GetObjectGVK(obj.(runtime.Object))
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(4).InfoS("Enqueuing owner", gvk.Kind, klog.KObj(obj), "ScyllaCluster", klog.KObj(sc))
	scc.enqueue(sc)
}

func (scc *ScyllaClusterController) enqueueOwnerThroughStatefulSet(obj metav1.Object) {
	sc := scc.resolveScyllaClusterControllerThroughStatefulSet(obj)
	if sc == nil {
		return
	}

	gvk, err := resource.GetObjectGVK(obj.(runtime.Object))
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(4).InfoS(fmt.Sprintf("%s added", gvk.Kind), gvk.Kind, klog.KObj(obj))
	scc.enqueue(sc)
}

func (scc *ScyllaClusterController) enqueueScyllaClusterFromPod(obj metav1.Object) {
	sc := scc.resolveScyllaClusterController(obj)
	if sc == nil {
		return
	}

	gvk, err := resource.GetObjectGVK(obj.(runtime.Object))
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(4).InfoS(fmt.Sprintf("%s added", gvk.Kind), gvk.Kind, klog.KObj(sc))
	scc.enqueue(sc)
}

func (scc *ScyllaClusterController) addService(obj interface{}) {
	sc := obj.(*corev1.Service)
	klog.V(4).InfoS("Observed addition of Service", "Service", klog.KObj(sc))
	scc.enqueueOwner(sc)
}

func (scc *ScyllaClusterController) updateService(old, cur interface{}) {
	oldService := old.(*corev1.Service)
	currentService := cur.(*corev1.Service)

	if currentService.UID != oldService.UID {
		key, err := KeyFunc(oldService)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", oldService, err))
			return
		}
		scc.deleteService(cache.DeletedFinalStateUnknown{
			Key: key,
			Obj: oldService,
		})
	}

	klog.V(4).InfoS("Observed update of Service", "Service", klog.KObj(oldService))
	scc.enqueueOwner(currentService)
}

func (scc *ScyllaClusterController) deleteService(obj interface{}) {
	sc, ok := obj.(*corev1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		sc, ok = tombstone.Obj.(*corev1.Service)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Service %#v", obj))
			return
		}
	}
	klog.V(4).InfoS("Observed deletion of Service", "Service", klog.KObj(sc))
	scc.enqueueOwner(sc)
}

func (scc *ScyllaClusterController) addPod(obj interface{}) {
	pod := obj.(*corev1.Pod)
	klog.V(4).InfoS("Observed addition of Pod", "Pod", klog.KObj(pod))
	scc.enqueueOwner(pod)
}

func (scc *ScyllaClusterController) updatePod(old, cur interface{}) {
	oldPod := old.(*corev1.Pod)
	currentPod := cur.(*corev1.Pod)

	if currentPod.UID != oldPod.UID {
		key, err := KeyFunc(oldPod)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", oldPod, err))
			return
		}
		scc.deleteService(cache.DeletedFinalStateUnknown{
			Key: key,
			Obj: oldPod,
		})
	}

	klog.V(4).InfoS("Observed update of Pod", "Pod", klog.KObj(oldPod))
	scc.enqueueOwner(currentPod)
}

func (scc *ScyllaClusterController) addStatefulSet(obj interface{}) {
	sc := obj.(*appsv1.StatefulSet)
	klog.V(4).InfoS("Observed addition of StatefulSet", "StatefulSet", klog.KObj(sc))
	scc.enqueueOwner(sc)
}

func (scc *ScyllaClusterController) updateStatefulSet(old, cur interface{}) {
	oldSts := old.(*appsv1.StatefulSet)
	currentSts := cur.(*appsv1.StatefulSet)

	if currentSts.UID != oldSts.UID {
		key, err := KeyFunc(oldSts)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", oldSts, err))
			return
		}
		scc.deleteStatefulSet(cache.DeletedFinalStateUnknown{
			Key: key,
			Obj: oldSts,
		})
	}

	klog.V(4).InfoS("Observed update of StatefulSet", "StatefulSet", klog.KObj(oldSts))
	scc.enqueueOwner(currentSts)
}

func (scc *ScyllaClusterController) deleteStatefulSet(obj interface{}) {
	sc, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		sc, ok = tombstone.Obj.(*appsv1.StatefulSet)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a StatefulSet %#v", obj))
			return
		}
	}
	klog.V(4).InfoS("Observed deletion of StatefulSet", "StatefulSet", klog.KObj(sc))
	scc.enqueueOwner(sc)
}

func (scc *ScyllaClusterController) addScyllaCluster(obj interface{}) {
	sc := obj.(*scyllav1.ScyllaCluster)
	klog.V(4).InfoS("Observed addition of ScyllaCluster", "ScyllaCluster", klog.KObj(sc))
	scc.enqueue(sc)
}

func (scc *ScyllaClusterController) updateScyllaCluster(old, cur interface{}) {
	oldSC := old.(*scyllav1.ScyllaCluster)
	currentSC := cur.(*scyllav1.ScyllaCluster)

	if currentSC.UID != oldSC.UID {
		key, err := KeyFunc(oldSC)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", oldSC, err))
			return
		}
		scc.deleteScyllaCluster(cache.DeletedFinalStateUnknown{
			Key: key,
			Obj: oldSC,
		})
	}

	klog.V(4).InfoS("Observed update of ScyllaCluster", "ScyllaCluster", klog.KObj(oldSC))
	scc.enqueue(currentSC)
}

func (scc *ScyllaClusterController) deleteScyllaCluster(obj interface{}) {
	sc, ok := obj.(*scyllav1.ScyllaCluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		sc, ok = tombstone.Obj.(*scyllav1.ScyllaCluster)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a ScyllaCluster %#v", obj))
			return
		}
	}
	klog.V(4).InfoS("Observed deletion of ScyllaCluster", "ScyllaCluster", klog.KObj(sc))
	scc.enqueue(sc)
}
