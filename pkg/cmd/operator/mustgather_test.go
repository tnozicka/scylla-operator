package operator

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	"github.com/scylladb/scylla-operator/pkg/gather/collect/testhelpers"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/spf13/cobra"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfakeclient "k8s.io/client-go/dynamic/fake"
	kubefakeclient "k8s.io/client-go/kubernetes/fake"
)

func TestMustGatherOptions_Run(t *testing.T) {
	apiResources := []*metav1.APIResourceList{
		{
			GroupVersion: corev1.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "namespaces", Namespaced: false, Kind: "Namespace", Verbs: []string{"list"}},
				{Name: "pods", Namespaced: true, Kind: "Pod", Verbs: []string{"list"}},
				{Name: "secrets", Namespaced: true, Kind: "Secret", Verbs: []string{"list"}},
			},
		},
		{
			GroupVersion: admissionregistrationv1.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "validatingwebhookconfigurations", Namespaced: false, Kind: "ValidatingWebhookConfiguration", Verbs: []string{"list"}},
				{Name: "mutatingwebhookconfigurations", Namespaced: false, Kind: "MutatingWebhookConfiguration", Verbs: []string{"list"}},
			},
		},
		{
			GroupVersion: scyllav1alpha1.GroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "scyllaoperatorconfigs", Namespaced: true, Kind: "ScyllaOperatorConfigs", Verbs: []string{"list"}},
				{Name: "nodeconfigs", Namespaced: true, Kind: "NodeConfigs", Verbs: []string{"list"}},
			},
		},
		{
			GroupVersion: scyllav1.GroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "scyllaclusters", Namespaced: true, Kind: "ScyllaCluster", Verbs: []string{"list"}},
			},
		},
	}

	testScheme := runtime.NewScheme()

	err := corev1.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	err = admissionregistrationv1.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	err = scyllav1alpha1.Install(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	err = scyllav1.Install(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	tt := []struct {
		name            string
		existingObjects []runtime.Object
		expectedDump    *testhelpers.GatherDump
		expectedError   error
	}{
		{
			name: "smoke test",
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "scylla-operator",
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "scylla-operator",
						Name:      "my-secret",
					},
					Data: map[string][]byte{
						"secret-key": []byte("secret-value"),
					},
				},
			},
			expectedError: nil,
			expectedDump: &testhelpers.GatherDump{
				EmptyDirs: nil,
				Files: []testhelpers.File{
					{
						Name: "cluster-scoped/namespaces/scylla-operator.yaml",
						Content: strings.TrimPrefix(`
apiVersion: v1
kind: Namespace
metadata:
  creationTimestamp: null
  name: scylla-operator
spec: {}
status: {}
`, "\n"),
					},
					{
						Name: "namespaces/scylla-operator/secrets/my-secret.yaml",
						Content: strings.TrimPrefix(`
apiVersion: v1
data:
  secret-key: PHJlZGFjdGVkPg==
kind: Secret
metadata:
  creationTimestamp: null
  name: my-secret
  namespace: scylla-operator
`, "\n"),
					},
					{
						Name: "scylla-operator-must-gather.log",
					},
				},
			},
		},
	}

	t.Parallel()
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			fakeKubeClient := kubefakeclient.NewSimpleClientset(tc.existingObjects...)
			fakeKubeClient.Resources = apiResources
			simpleFakeDiscoveryClient := fakeKubeClient.Discovery()
			fakeDiscoveryClient := &testhelpers.FakeDiscoveryWithSPR{
				FakeDiscovery: simpleFakeDiscoveryClient.(*fakediscovery.FakeDiscovery),
			}
			existingUnstructuredObjects := make([]runtime.Object, 0, len(tc.existingObjects))
			for _, e := range tc.existingObjects {
				u := &unstructured.Unstructured{}
				err := testScheme.Convert(e, u, nil)
				if err != nil {
					t.Fatal(err)
				}
				existingUnstructuredObjects = append(existingUnstructuredObjects, u)
			}
			fakeDynamicClient := dynamicfakeclient.NewSimpleDynamicClient(testScheme, existingUnstructuredObjects...)
			streams := genericclioptions.IOStreams{
				In:     os.Stdin,
				Out:    os.Stdout,
				ErrOut: os.Stderr,
			}
			o := NewMustGatherOptions(streams)
			o.DestDir = tmpDir
			o.kubeClient = fakeKubeClient
			o.discoveryClient = fakeDiscoveryClient
			o.dynamicClient = fakeDynamicClient

			cmd := &cobra.Command{
				Use: "must-gather",
			}
			o.AddFlags(cmd.Flags())
			err = o.Run(
				streams,
				cmd,
			)
			if !reflect.DeepEqual(err, tc.expectedError) {
				t.Errorf("expected error %#v, got %#v", tc.expectedError, err)
			}

			got, err := testhelpers.ReadGatherDump(tmpDir)
			if err != nil {
				t.Fatal(err)
			}

			// The log has time stamps and other variable input, let's test only it's presence for now.
			// Eventually we can come back and see about reducing / replacing the variable input.
			for i := range got.Files {
				f := &got.Files[i]
				if f.Name == "scylla-operator-must-gather.log" {
					f.Content = ""
				}
			}

			diff := cmp.Diff(tc.expectedDump, got)
			if len(diff) != 0 {
				t.Errorf("expected and got filesystems differ:\n%s", diff)
			}
		})
	}
}
