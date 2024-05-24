package framework

import (
	o "github.com/onsi/gomega"
	scyllaclientset "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

type GenericClientInterface interface {
	DiscoveryClient() *discovery.DiscoveryClient
}

type ClientInterface interface {
	GenericClientInterface
	ClientConfig() *restclient.Config
	KubeClient() *kubernetes.Clientset
	DynamicClient() dynamic.Interface
	ScyllaClient() *scyllaclientset.Clientset
}

type AdminClientInterface interface {
	GenericClientInterface
	AdminClientConfig() *restclient.Config
	KubeAdminClient() *kubernetes.Clientset
	DynamicAdminClient() dynamic.Interface
	ScyllaAdminClient() *scyllaclientset.Clientset
}

type FullClientInterface interface {
	ClientInterface
	AdminClientInterface
}

type Client struct {
	Config *restclient.Config
}

var _ GenericClientInterface = &Client{}
var _ ClientInterface = &Client{}

func (c *Client) ClientConfig() *restclient.Config {
	o.Expect(c.Config).NotTo(o.BeNil())
	return c.Config
}

func (c *Client) DiscoveryClient() *discovery.DiscoveryClient {
	client, err := discovery.NewDiscoveryClientForConfig(c.ClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return client
}

func (c *Client) DynamicClient() dynamic.Interface {
	cs, err := dynamic.NewForConfig(c.ClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return cs
}

func (c *Client) KubeClient() *kubernetes.Clientset {
	cs, err := kubernetes.NewForConfig(c.ClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return cs
}

func (c *Client) ScyllaClient() *scyllaclientset.Clientset {
	cs, err := scyllaclientset.NewForConfig(c.ClientConfig())
	o.Expect(err).NotTo(o.HaveOccurred())
	return cs
}

type AdminClient struct {
	Client
}

var _ AdminClientInterface = &AdminClient{}

func (ac *AdminClient) AdminClientConfig() *restclient.Config {
	return ac.Client.ClientConfig()
}

func (ac *AdminClient) DynamicAdminClient() dynamic.Interface {
	return ac.Client.DynamicClient()
}

func (ac *AdminClient) KubeAdminClient() *kubernetes.Clientset {
	return ac.Client.KubeClient()
}

func (ac *AdminClient) ScyllaAdminClient() *scyllaclientset.Clientset {
	return ac.Client.ScyllaClient()
}

type FullClient struct {
	Client
	AdminClient
}

var _ ClientInterface = &FullClient{}
var _ AdminClientInterface = &FullClient{}
var _ FullClientInterface = &FullClient{}

func (c *FullClient) DiscoveryClient() *discovery.DiscoveryClient {
	return c.Client.DiscoveryClient()
}
