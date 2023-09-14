package operator

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/scylladb/scylla-operator/pkg/gather/collect/testhelpers"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/pointer"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfakeclient "k8s.io/client-go/dynamic/fake"
	kubefakeclient "k8s.io/client-go/kubernetes/fake"
)

func TestGatherOptions_Run(t *testing.T) {
	apiResources := []*metav1.APIResourceList{
		{
			GroupVersion: corev1.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "namespaces", Namespaced: false, Kind: "Namespace", Verbs: []string{"list"}},
				{Name: "secrets", Namespaced: true, Kind: "Secret", Verbs: []string{"list"}},
			},
		},
	}

	testScheme := runtime.NewScheme()

	err := corev1.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	tt := []struct {
		name             string
		args             []string
		existingObjects  []runtime.Object
		all              bool
		relatedResources bool
		expectedDump     *testhelpers.GatherDump
		expectedError    error
	}{
		{
			name: "smoke test",
			args: []string{"namespaces"},
			all:  true,
			existingObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-namespace",
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "my-namespace",
						Name:      "my-secret",
					},
					Data: map[string][]byte{
						"secret-key": []byte("secret-value"),
					},
				},
			},
			relatedResources: true,
			expectedError:    nil,
			expectedDump: &testhelpers.GatherDump{
				EmptyDirs: nil,
				Files: []testhelpers.File{
					{
						Name: "cluster-scoped/namespaces/my-namespace.yaml",
						Content: strings.TrimPrefix(`
apiVersion: v1
kind: Namespace
metadata:
  creationTimestamp: null
  name: my-namespace
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
  namespace: my-namespace
`, "\n"),
					},
					{
						Name: "scylla-operator-gather.log",
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
			o := NewGatherOptions(streams)
			o.DestDir = tmpDir
			o.kubeClient = fakeKubeClient
			o.discoveryClient = fakeDiscoveryClient
			o.dynamicClient = fakeDynamicClient
			o.builderFlags.All = pointer.Ptr(tc.all)
			o.builder = o.builderFlags.ToBuilder(o.configFlags, tc.args)

			cmd := &cobra.Command{
				Use: "gather",
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
				if f.Name == "scylla-operator-gather.log" {
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
