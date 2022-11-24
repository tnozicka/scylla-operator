package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	AvailableCondition   = "Available"
	ProgressingCondition = "Progressing"
	DegradedCondition    = "Degraded"
)

// ExposeOptions hold options related to exposing ScyllaCluster backends.
type ExposeOptions struct {
	// cql specifies expose options for CQL SSL backend.
	// +optional
	CQL *CQLExposeOptions `json:"cql,omitempty"`
}

// IngressOptions defines configuration options for Ingress objects associated with cluster nodes.
type IngressOptions struct {
	// ingressClassName specifies Ingress class name.
	IngressClassName string `json:"ingressClassName,omitempty"`

	// annotations specifies custom annotations merged into every Ingress object.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// CQLExposeOptions hold options related to exposing CQL backend.
type CQLExposeOptions struct {
	// ingress is an Ingress configuration options.
	// +optional
	Ingress *IngressOptions `json:"ingress,omitempty"`
}

type PlacementSpec struct {
	// nodeAffinity describes node affinity scheduling rules for the pod.
	// +optional
	NodeAffinity *corev1.NodeAffinity `json:"nodeAffinity,omitempty"`

	// podAffinity describes pod affinity scheduling rules.
	// +optional
	PodAffinity *corev1.PodAffinity `json:"podAffinity,omitempty"`

	// podAntiAffinity describes pod anti-affinity scheduling rules.
	// +optional
	PodAntiAffinity *corev1.PodAntiAffinity `json:"podAntiAffinity,omitempty"`

	// tolerations allow the pod to tolerate any taint that matches the triple <key,value,effect>
	// using the matching operator.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

type StorageSpec struct {
	// capacity describes the requested size of each persistent volume.
	// +kubebuilder:validation:Required
	Capacity string `json:"capacity"`

	// storageClassName is the name of a storageClass to request.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

type PrometheusSpec struct {
	// placement describes restrictions for the nodes Scylla is scheduled on.
	// +optional
	Placement *PlacementSpec `json:"placement,omitempty"`

	// resources the Scylla container will use.
	// +kubebuilder:default:={requests: {cpu: "50m", memory: "100M"}}
	Resources corev1.ResourceRequirements `json:"resources"`

	// storage describes the underlying storage that Scylla will consume.
	// +kubebuilder:validation:Required
	Storage StorageSpec `json:"storage"`
}

type GrafanaSpec struct {
	// placement describes restrictions for the nodes Scylla is scheduled on.
	// +optional
	Placement *PlacementSpec `json:"placement,omitempty"`

	// resources the Scylla container will use.
	// +kubebuilder:default:={requests: {cpu: "50m", memory: "100M"}}
	Resources corev1.ResourceRequirements `json:"resources"`

	// exposeOptions specifies options for exposing Grafana UI.
	// +optional
	ExposeOptions *ExposeOptions `json:"exposeOptions,omitempty"`
}

type Components struct {
	// prometheus holds configuration for the prometheus instance, if any.
	// +optional
	Prometheus *PrometheusSpec `json:"prometheus,omitempty"`

	// prometheus holds configuration for the grafana instance, if any.
	// +optional
	Grafana *GrafanaSpec `json:"grafana,omitempty"`
}

// ScyllaDBMonitoringSpec defines the desired state of ScyllaDBMonitoring.
type ScyllaDBMonitoringSpec struct {
	// endpointsSelector select which Endpoints should be scraped.
	// For local ScyllaDB clusters or datacenters, this is the same selector as if you were trying to select member Services.
	// For remote ScyllaDB clusters, this can select any endpoints that are created manually or for a Service without selectors.
	// +kubebuilder:validation:Required
	EndpointsSelector metav1.LabelSelector `json:"endpointsSelector"`

	// components hold additional config for the monitoring components in use.
	Components *Components `json:"components"`
}

// ScyllaDBMonitoringStatus defines the observed state of ScyllaDBMonitoring.
type ScyllaDBMonitoringStatus struct {
	// observedGeneration is the most recent generation observed for this ScyllaCluster. It corresponds to the
	// ScyllaCluster's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// conditions hold conditions describing ScyllaCluster state.
	// To determine whether a cluster rollout is finished, look for Available=True,Progressing=False,Degraded=False.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ScyllaDBMonitoring defines a monitoring instance for ScyllaDB clusters.
type ScyllaDBMonitoring struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of this scylla cluster.
	Spec ScyllaDBMonitoringSpec `json:"spec,omitempty"`

	// status is the current status of this scylla cluster.
	Status ScyllaDBMonitoringStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ScyllaDBMonitoringList holds a list of ScyllaDBMonitoring.
type ScyllaDBMonitoringList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScyllaDBMonitoring `json:"items"`
}
