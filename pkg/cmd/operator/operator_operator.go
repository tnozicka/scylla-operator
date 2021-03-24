package operator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	scyllaversionedclient "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	scyllainformers "github.com/scylladb/scylla-operator/pkg/client/scylla/informers/externalversions"
	"github.com/scylladb/scylla-operator/pkg/cmdutil"
	"github.com/scylladb/scylla-operator/pkg/controller/cluster"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/leaderelection"
	"github.com/scylladb/scylla-operator/pkg/signals"
	"github.com/scylladb/scylla-operator/pkg/version"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type OperatorOptions struct {
	genericclioptions.ClientConfig
	genericclioptions.InClusterReflection
	genericclioptions.LeaderElection

	kubeClient   kubernetes.Interface
	scyllaClient scyllaversionedclient.Interface

	ConcurrentSyncs int
	OperatorImage   string
}

func NewRunOperatorOptions(streams genericclioptions.IOStreams) *OperatorOptions {
	return &OperatorOptions{
		ClientConfig:        genericclioptions.NewClientConfig(),
		InClusterReflection: genericclioptions.InClusterReflection{},
		LeaderElection:      genericclioptions.NewLeaderElection(),

		ConcurrentSyncs: 5,
		OperatorImage:   "",
	}
}

func NewOperatorCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewRunOperatorOptions(streams)

	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Run the scylla operator.",
		Long:  `Run the scylla operator.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := o.Validate()
			if err != nil {
				return err
			}

			err = o.Complete()
			if err != nil {
				return err
			}

			err = o.Run(streams, cmd.Name())
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
	o.LeaderElection.AddFlags(cmd)

	cmd.Flags().IntVarP(&o.ConcurrentSyncs, "concurrent-syncs", "", o.ConcurrentSyncs, "The number of ScyllaCluster objects that are allowed to sync concurrently.")
	cmd.Flags().StringVarP(&o.OperatorImage, "image", "", o.OperatorImage, "Image of the operator used")

	return cmd
}

func (o *OperatorOptions) Validate() error {
	var errs []error

	errs = append(errs, o.ClientConfig.Validate())
	errs = append(errs, o.InClusterReflection.Validate())
	errs = append(errs, o.LeaderElection.Validate())

	if len(o.OperatorImage) == 0 {
		return errors.New("operator image can't be empty")
	}

	return apierrors.NewAggregate(errs)
}

func (o *OperatorOptions) Complete() error {
	err := o.ClientConfig.Complete()
	if err != nil {
		return err
	}

	err = o.InClusterReflection.Complete()
	if err != nil {
		return err
	}

	err = o.LeaderElection.Complete()
	if err != nil {
		return err
	}

	o.kubeClient, err = kubernetes.NewForConfig(o.RestConfig)
	if err != nil {
		return fmt.Errorf("can't build kubernetes clientset: %w", err)
	}

	o.scyllaClient, err = scyllaversionedclient.NewForConfig(o.RestConfig)
	if err != nil {
		return fmt.Errorf("can't build scylla clientset: %w", err)
	}

	return nil
}

func (o *OperatorOptions) Run(streams genericclioptions.IOStreams, commandName string) error {
	klog.Infof("%s version %s", commandName, version.Get())
	klog.Infof("loglevel is set to %q", cmdutil.GetLoglevel())

	stopCh := signals.StopChannel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-stopCh
		cancel()
	}()

	return leaderelection.Run(
		ctx,
		commandName,
		commandName+"-locks",
		o.Namespace,
		o.kubeClient,
		o.LeaderElectionLeaseDuration,
		o.LeaderElectionRenewDeadline,
		o.LeaderElectionRetryPeriod,
		func(ctx context.Context) error {
			return o.run(ctx, streams)
		},
	)
}

func (o *OperatorOptions) run(ctx context.Context, streams genericclioptions.IOStreams) error {
	kubeInformers := informers.NewSharedInformerFactory(o.kubeClient, 10*time.Minute)
	scyllaInformers := scyllainformers.NewSharedInformerFactory(o.scyllaClient, 10*time.Minute)

	scc, err := cluster.NewScyllaClusterController(
		o.kubeClient,
		o.scyllaClient.ScyllaV1(),
		kubeInformers.Core().V1().Pods(),
		kubeInformers.Core().V1().Services(),
		kubeInformers.Apps().V1().StatefulSets(),
		scyllaInformers.Scylla().V1().ScyllaClusters(),
		o.OperatorImage,
	)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		kubeInformers.Start(ctx.Done())
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		scyllaInformers.Start(ctx.Done())
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		scc.Run(ctx, o.ConcurrentSyncs)
	}()

	<-ctx.Done()

	return nil
}
