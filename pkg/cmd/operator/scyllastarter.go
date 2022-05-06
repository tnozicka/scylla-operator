package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall"

	scyllaversionedclient "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"github.com/scylladb/scylla-operator/pkg/controllerhelpers"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/internalapi"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/sidecar/config"
	"github.com/scylladb/scylla-operator/pkg/sidecar/identity"
	"github.com/scylladb/scylla-operator/pkg/signals"
	"github.com/scylladb/scylla-operator/pkg/version"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
)

type ScyllaStarterOptions struct {
	genericclioptions.ClientConfig
	genericclioptions.InClusterReflection

	ServiceName string
	CPUCount    int

	kubeClient   kubernetes.Interface
	scyllaClient scyllaversionedclient.Interface
}

func NewScyllaStarterOptions(streams genericclioptions.IOStreams) *ScyllaStarterOptions {
	clientConfig := genericclioptions.NewClientConfig("scylla-starter")
	clientConfig.QPS = 2
	clientConfig.Burst = 5

	return &ScyllaStarterOptions{
		ClientConfig:        clientConfig,
		InClusterReflection: genericclioptions.InClusterReflection{},
	}
}

func NewScyllaStarterCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewScyllaStarterOptions(streams)

	cmd := &cobra.Command{
		Use:   "run-scylla",
		Short: "Run scylla.",
		Long:  `Run scylla.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := o.Validate()
			if err != nil {
				return err
			}

			err = o.Complete()
			if err != nil {
				return err
			}

			err = o.Run(streams, cmd)
			if err != nil {
				return err
			}

			return nil
		},

		SilenceErrors: true,
		SilenceUsage:  true,
	}

	o.ClientConfig.AddFlags(cmd)
	o.InClusterReflection.AddFlags(cmd)

	cmd.Flags().StringVarP(&o.ServiceName, "service-name", "", o.ServiceName, "Name of the service corresponding to the managed node.")
	cmd.Flags().IntVarP(&o.CPUCount, "cpu-count", "", o.CPUCount, "Number of cpus to use.")

	return cmd
}

func (o *ScyllaStarterOptions) Validate() error {
	var errs []error

	errs = append(errs, o.ClientConfig.Validate())
	errs = append(errs, o.InClusterReflection.Validate())

	if len(o.ServiceName) == 0 {
		errs = append(errs, fmt.Errorf("service-name can't be empty"))
	} else {
		serviceNameValidationErrs := apimachineryvalidation.NameIsDNS1035Label(o.ServiceName, false)
		if len(serviceNameValidationErrs) != 0 {
			errs = append(errs, fmt.Errorf("invalid service name %q: %v", o.ServiceName, serviceNameValidationErrs))
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (o *ScyllaStarterOptions) Complete() error {
	err := o.ClientConfig.Complete()
	if err != nil {
		return err
	}

	err = o.InClusterReflection.Complete()
	if err != nil {
		return err
	}

	o.kubeClient, err = kubernetes.NewForConfig(o.ProtoConfig)
	if err != nil {
		return fmt.Errorf("can't build kubernetes clientset: %w", err)
	}

	o.scyllaClient, err = scyllaversionedclient.NewForConfig(o.RestConfig)
	if err != nil {
		return fmt.Errorf("can't build scylla clientset: %w", err)
	}

	return nil
}

func (o *ScyllaStarterOptions) prepareCommand(streams genericclioptions.IOStreams, cmd *cobra.Command) ([]string, []string, error) {
	stopCh := signals.StopChannel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-stopCh
		cancel()
	}()

	// Wait for the service that holds identity for this scylla node.
	serviceFieldSelector := fields.OneTermEqualSelector("metadata.name", o.ServiceName)
	serviceLW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = serviceFieldSelector.String()
			return o.kubeClient.CoreV1().Services(o.Namespace).List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = serviceFieldSelector.String()
			return o.kubeClient.CoreV1().Services(o.Namespace).Watch(ctx, options)
		},
	}
	klog.V(2).InfoS("Waiting for Service", "Service", naming.ManualRef(o.Namespace, o.ServiceName))
	event, err := watchtools.UntilWithSync(
		ctx,
		serviceLW,
		&corev1.Service{},
		nil,
		func(e watch.Event) (bool, error) {
			switch t := e.Type; t {
			case watch.Added, watch.Modified:
				return true, nil
			case watch.Error:
				return true, apierrors.FromObject(e.Object)
			default:
				return true, fmt.Errorf("unexpected event type %v", t)
			}
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("can't wait for service %q: %w", naming.ManualRef(o.Namespace, o.ServiceName), err)
	}
	service := event.Object.(*corev1.Service)

	// Wait for this Pod to have ContainerID set.
	podFieldSelector := fields.OneTermEqualSelector("metadata.name", o.ServiceName)
	podLW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = podFieldSelector.String()
			return o.kubeClient.CoreV1().Pods(o.Namespace).List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = podFieldSelector.String()
			return o.kubeClient.CoreV1().Pods(o.Namespace).Watch(ctx, options)
		},
	}
	klog.V(2).InfoS("Waiting for Pod To have scylla ContainerID set", "Pod", naming.ManualRef(o.Namespace, o.ServiceName))
	var containerID string
	var pod *corev1.Pod
	_, err = watchtools.UntilWithSync(
		ctx,
		podLW,
		&corev1.Pod{},
		nil,
		func(e watch.Event) (bool, error) {
			switch t := e.Type; t {
			case watch.Added, watch.Modified:
				pod = e.Object.(*corev1.Pod)

				containerID, err = controllerhelpers.GetScyllaContainerID(pod)
				if err != nil {
					klog.Warningf("can't get scylla container id in pod %q: %w", naming.ObjRef(pod), err)
					return false, nil
				}

				if len(containerID) == 0 {
					klog.V(4).InfoS("ContainerID is not yet set", "Pod", klog.KObj(pod))
					return false, nil
				}

				return true, nil

			case watch.Error:
				return true, apierrors.FromObject(e.Object)

			default:
				return true, fmt.Errorf("unexpected event type %v", t)
			}
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("can't wait for pod's ContainerID: %w", err)
	}

	member := identity.NewMemberFromObjects(service, pod)

	labelSelector := labels.Set{
		naming.OwnerUIDLabel:      string(pod.UID),
		naming.ConfigMapTypeLabel: string(naming.NodeConfigDataConfigMapType),
	}.AsSelector()
	podLW = &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = labelSelector.String()
			return o.kubeClient.CoreV1().ConfigMaps(pod.Namespace).List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = labelSelector.String()
			return o.kubeClient.CoreV1().ConfigMaps(pod.Namespace).Watch(ctx, options)
		},
	}
	klog.V(2).InfoS("Waiting for NodeConfig's data ConfigMap ", "Selector", labelSelector.String())
	_, err = watchtools.UntilWithSync(
		ctx,
		podLW,
		&corev1.ConfigMap{},
		nil,
		func(e watch.Event) (bool, error) {
			switch t := e.Type; t {
			case watch.Added, watch.Modified:
				cm := e.Object.(*corev1.ConfigMap)

				if cm.Data == nil {
					klog.V(4).InfoS("ConfigMap missing data", "ConfigMap", klog.KObj(cm))
					return false, nil
				}

				srcData, found := cm.Data[naming.ScyllaRuntimeConfigKey]
				if !found {
					klog.V(4).InfoS("ConfigMap is missing key", "ConfigMap", klog.KObj(cm), "Key", naming.ScyllaRuntimeConfigKey)
					return false, nil
				}

				src := &internalapi.SidecarRuntimeConfig{}
				err = json.Unmarshal([]byte(srcData), src)
				if err != nil {
					klog.V(4).ErrorS(err, "Can't unmarshal scylla runtime config", "ConfigMap", klog.KObj(cm), "Key", naming.ScyllaRuntimeConfigKey)
					return false, nil
				}

				if src.ContainerID != containerID {
					klog.V(4).InfoS("Scylla runtime config is not yet updated with our container id",
						"ConfigMap", klog.KObj(cm),
						"ConfigContainerID", src.ContainerID,
						"SidecarContainerID", containerID,
					)
					return false, nil
				}

				if len(src.BlockingNodeConfigs) > 0 {
					klog.V(4).InfoS("Waiting on NodeConfig(s)",
						"ConfigMap", klog.KObj(cm),
						"ContainerID", containerID,
						"NodeConfig", src.BlockingNodeConfigs,
					)
					return false, nil
				}

				klog.V(4).InfoS("ConfigMap container ready", "ConfigMap", klog.KObj(cm), "ContainerID", containerID)
				return true, nil

			case watch.Error:
				return true, apierrors.FromObject(e.Object)

			default:
				return true, fmt.Errorf("unexpected event type %v", t)
			}
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("can't wait for optimization: %w", err)
	}

	klog.V(2).InfoS("Starting scylla")

	cfg := config.NewScyllaConfig(member, o.kubeClient, o.scyllaClient, o.CPUCount)
	scyllaCmd, env, err := cfg.Setup(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("can't set up scylla: %w", err)
	}

	return scyllaCmd, env, nil

}
func (o *ScyllaStarterOptions) Run(streams genericclioptions.IOStreams, cmd *cobra.Command) error {
	klog.Infof("%s version %s", cmd.Name(), version.Get())
	cliflag.PrintFlags(cmd.Flags())

	command, env, err := o.prepareCommand(streams, cmd)
	if err != nil {
		return fmt.Errorf("can't prepare scylla command: %w", err)
	}
	if len(command) == 0 {
		return fmt.Errorf("scylla command can't be empty")
	}

	// Exec into Scylla.
	return syscall.Exec(command[0], command, env)
}