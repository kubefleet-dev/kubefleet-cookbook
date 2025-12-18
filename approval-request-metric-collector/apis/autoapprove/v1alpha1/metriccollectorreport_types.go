/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// MetricCollectorReportConditionTypeMetricsCollected indicates whether metrics have been successfully collected
	MetricCollectorReportConditionTypeMetricsCollected = "MetricsCollected"

	// MetricCollectorReportConditionReasonCollectionFailed indicates metric collection failed
	MetricCollectorReportConditionReasonCollectionFailed = "CollectionFailed"

	// MetricCollectorReportConditionReasonCollectionSucceeded indicates metric collection succeeded
	MetricCollectorReportConditionReasonCollectionSucceeded = "CollectionSucceeded"
)

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=mcr,categories={fleet,fleet-metrics}
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:JSONPath=`.status.workloadsMonitored`,name="Workloads",type=integer
// +kubebuilder:printcolumn:JSONPath=`.status.lastCollectionTime`,name="Last-Collection",type=date
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// MetricCollectorReport is created by the approval-request-controller on the hub cluster
// in the fleet-member-{clusterName} namespace. The metric-collector on the member cluster
// watches these reports and updates their status with collected metrics.
//
// Controller workflow:
// 1. Approval-controller creates MetricCollectorReport with spec on hub
// 2. Metric-collector watches MetricCollectorReport on hub (in fleet-member-{clusterName} namespace)
// 3. Metric-collector queries Prometheus on member cluster
// 4. Metric-collector updates MetricCollectorReport status on hub with collected metrics
//
// Namespace: fleet-member-{clusterName}
// Name: Matches the UpdateRun name
type MetricCollectorReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MetricCollectorReportSpec   `json:"spec,omitempty"`
	Status MetricCollectorReportStatus `json:"status,omitempty"`
}

// MetricCollectorReportSpec defines the configuration for metric collection.
type MetricCollectorReportSpec struct {
	// PrometheusURL is the URL of the Prometheus server on the member cluster
	// Example: "http://prometheus.fleet-system.svc.cluster.local:9090"
	PrometheusURL string `json:"prometheusUrl"`
}

// MetricCollectorReportStatus contains the collected metrics from the member cluster.
type MetricCollectorReportStatus struct {
	// Conditions represent the latest available observations of the report's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// WorkloadsMonitored is the count of workloads being monitored.
	// +optional
	WorkloadsMonitored int32 `json:"workloadsMonitored,omitempty"`

	// LastCollectionTime is when metrics were last collected on the member cluster.
	// +optional
	LastCollectionTime *metav1.Time `json:"lastCollectionTime,omitempty"`

	// CollectedMetrics contains the most recent metrics from each workload.
	// +optional
	CollectedMetrics []WorkloadMetric `json:"collectedMetrics,omitempty"`
}

// WorkloadMetric represents metrics collected from a single workload.
type WorkloadMetric struct {
	// Namespace of the workload.
	// +required
	Namespace string `json:"namespace"`

	// Name of the workload.
	// +required
	WorkloadName string `json:"workloadName"`

	// Kind of the workload controller (e.g., Deployment, StatefulSet, DaemonSet).
	// +optional
	WorkloadKind string `json:"workloadKind,omitempty"`

	// PodName is the name of the specific pod that reported this metric.
	// +required
	PodName string `json:"podName"`

	// Health indicates if the workload is healthy (true=healthy, false=unhealthy).
	// +required
	Health bool `json:"health"`
}

// +kubebuilder:object:root=true

// MetricCollectorReportList contains a list of MetricCollectorReport.
type MetricCollectorReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MetricCollectorReport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MetricCollectorReport{}, &MetricCollectorReportList{})
}
