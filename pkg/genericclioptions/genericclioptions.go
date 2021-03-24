package genericclioptions

import (
	"fmt"
	"io"
	"io/ioutil"
	"time"

	"github.com/spf13/cobra"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// IOStreams is a structure containing all standard streams.
type IOStreams struct {
	// In think, os.Stdin
	In io.Reader
	// Out think, os.Stdout
	Out io.Writer
	// ErrOut think, os.Stderr
	ErrOut io.Writer
}

type ClientConfig struct {
	Kubeconfig string
	RestConfig *restclient.Config
}

func NewClientConfig() ClientConfig {
	return ClientConfig{
		Kubeconfig: "",
		RestConfig: nil,
	}
}

func (cc *ClientConfig) AddFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&cc.Kubeconfig, "kubeconfig", "", cc.Kubeconfig, "Path to the kubeconfig file")
}

func (cc *ClientConfig) Validate() error {
	return nil
}

func (cc *ClientConfig) Complete() error {
	var err error

	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	// Use explicit kubeconfig if set.
	loader.ExplicitPath = cc.Kubeconfig
	cc.RestConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loader,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return fmt.Errorf("can't create client config: %w", err)
	}

	return nil
}

type InClusterReflection struct {
	Namespace string
}

func (o *InClusterReflection) AddFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&o.Namespace, "namespace", "", o.Namespace, "Namespace where the controller is running. Auto-detected if run inside a cluster.")
}

func (o *InClusterReflection) Validate() error {
	return nil
}

func (o *InClusterReflection) Complete() error {
	if len(o.Namespace) == 0 {
		// Autodetect if running inside a cluster
		bytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return fmt.Errorf("can't autodetect controller namespace: %w", err)
		}

		o.Namespace = string(bytes)
	}

	return nil
}

type LeaderElection struct {
	LeaderElectionLeaseDuration time.Duration
	LeaderElectionRenewDeadline time.Duration
	LeaderElectionRetryPeriod   time.Duration
}

func NewLeaderElection() LeaderElection {
	return LeaderElection{
		LeaderElectionLeaseDuration: 60 * time.Second,
		LeaderElectionRenewDeadline: 35 * time.Second,
		LeaderElectionRetryPeriod:   10 * time.Second,
	}
}

func (le *LeaderElection) AddFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().DurationVar(&le.LeaderElectionLeaseDuration, "leader-election-lease-duration", le.LeaderElectionLeaseDuration, "LeaseDuration is the duration that non-leader candidates will wait to force acquire leadership.")
	cmd.PersistentFlags().DurationVar(&le.LeaderElectionRenewDeadline, "leader-election-renew-deadline", le.LeaderElectionRenewDeadline, "RenewDeadline is the duration that the acting master will retry refreshing leadership before giving up.")
	cmd.PersistentFlags().DurationVar(&le.LeaderElectionRetryPeriod, "leader-election-retry-period", le.LeaderElectionRetryPeriod, "RetryPeriod is the duration the LeaderElector clients should wait between tries of actions.")
}

func (le *LeaderElection) Validate() error {
	return nil
}

func (le *LeaderElection) Complete() error {
	return nil
}
