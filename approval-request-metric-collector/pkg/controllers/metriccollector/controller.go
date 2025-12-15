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

package metriccollector

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	autoapprovev1alpha1 "github.com/kubefleet-dev/kubefleet-cookbook/approval-request-metric-collector/apis/autoapprove/v1alpha1"
)

const (
	// defaultCollectionInterval is the interval for collecting metrics (30 seconds)
	defaultCollectionInterval = 30 * time.Second
)

// Reconciler reconciles a MetricCollectorReport object on the hub cluster
type Reconciler struct {
	// HubClient is the client to access the hub cluster (for MetricCollectorReport and WorkloadTracker)
	HubClient client.Client
}

// Reconcile watches MetricCollectorReport on hub and updates it with metrics from member Prometheus
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("MetricCollectorReport reconciliation starts", "report", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("MetricCollectorReport reconciliation ends", "report", req.NamespacedName, "latency", latency)
	}()

	// 1. Get MetricCollectorReport from hub cluster
	report := &autoapprovev1alpha1.MetricCollectorReport{}
	if err := r.HubClient.Get(ctx, req.NamespacedName, report); err != nil {
		if errors.IsNotFound(err) {
			klog.V(2).InfoS("MetricCollectorReport not found, ignoring", "report", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get MetricCollectorReport", "report", req.NamespacedName)
		return ctrl.Result{}, err
	}

	klog.InfoS("Reconciling MetricCollectorReport", "name", report.Name, "namespace", report.Namespace)

	// 2. Get PrometheusURL from report spec (or use default)
	prometheusURL := report.Spec.PrometheusURL

	// 3. Query Prometheus on member cluster for all workload_health metrics
	promClient := NewPrometheusClient(prometheusURL, "", nil)
	collectedMetrics, collectErr := r.collectAllWorkloadMetrics(ctx, promClient)

	// 5. Update MetricCollectorReport status on hub
	now := metav1.Now()
	report.Status.LastCollectionTime = &now
	report.Status.CollectedMetrics = collectedMetrics
	report.Status.WorkloadsMonitored = int32(len(collectedMetrics))

	if collectErr != nil {
		klog.ErrorS(collectErr, "Failed to collect metrics", "prometheusUrl", prometheusURL)
		meta.SetStatusCondition(&report.Status.Conditions, metav1.Condition{
			Type:               autoapprovev1alpha1.MetricCollectorReportConditionTypeMetricsCollected,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: report.Generation,
			Reason:             autoapprovev1alpha1.MetricCollectorReportConditionReasonCollectionFailed,
			Message:            fmt.Sprintf("Failed to collect metrics: %v", collectErr),
		})
	} else {
		klog.V(2).InfoS("Successfully collected metrics", "report", report.Name, "workloads", len(collectedMetrics))
		meta.SetStatusCondition(&report.Status.Conditions, metav1.Condition{
			Type:               autoapprovev1alpha1.MetricCollectorReportConditionTypeMetricsCollected,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: report.Generation,
			Reason:             autoapprovev1alpha1.MetricCollectorReportConditionReasonCollectionSucceeded,
			Message:            fmt.Sprintf("Successfully collected metrics from %d workloads", len(collectedMetrics)),
		})
	}

	if err := r.HubClient.Status().Update(ctx, report); err != nil {
		klog.ErrorS(err, "Failed to update MetricCollectorReport status", "report", req.NamespacedName)
		return ctrl.Result{}, err
	}

	klog.InfoS("Successfully updated MetricCollectorReport", "metricsCount", len(collectedMetrics), "prometheusUrl", prometheusURL)
	return ctrl.Result{RequeueAfter: defaultCollectionInterval}, nil
}

// collectAllWorkloadMetrics queries Prometheus for all workload_health metrics
func (r *Reconciler) collectAllWorkloadMetrics(ctx context.Context, promClient PrometheusClient) ([]autoapprovev1alpha1.WorkloadMetrics, error) {
	var collectedMetrics []autoapprovev1alpha1.WorkloadMetrics

	// Query all workload_health metrics (no filtering)
	query := "workload_health"

	data, err := promClient.Query(ctx, query)
	if err != nil {
		klog.ErrorS(err, "Failed to query Prometheus for workload_health metrics")
		return nil, err
	}

	if len(data.Result) == 0 {
		klog.V(4).InfoS("No workload_health metrics found in Prometheus")
		return collectedMetrics, nil
	}

	// Extract metrics from Prometheus result
	for _, res := range data.Result {
		// Extract labels from the Prometheus metric
		// The workload_health metric includes labels like: workload_health{namespace="test-ns",app="sample-app"}
		// These labels come from Kubernetes pod labels and are added by Prometheus during scraping.
		// The relabeling configuration is in examples/prometheus/configmap.yaml:
		//   - namespace: from __meta_kubernetes_namespace (pod's namespace)
		//   - app: from __meta_kubernetes_pod_label_app (pod's "app" label)
		namespace := res.Metric["namespace"]
		workloadName := res.Metric["app"]

		if namespace == "" || workloadName == "" {
			continue
		}

		// Extract health value from Prometheus result
		// Prometheus returns values as [timestamp, value_string] array
		// We need at least 2 elements: index 0 is timestamp, index 1 is the metric value
		var health float64
		if len(res.Value) >= 2 {
			if valueStr, ok := res.Value[1].(string); ok {
				fmt.Sscanf(valueStr, "%f", &health)
			}
		}

		// Convert float to bool: workload is healthy if metric value >= 1.0
		// We use >= instead of == to handle floating point precision issues that can occur
		// during JSON serialization/deserialization. The metric app emits 1.0 for healthy
		// and 0.0 for unhealthy, so >= 1.0 safely distinguishes between the two states.
		workloadMetrics := autoapprovev1alpha1.WorkloadMetrics{
			WorkloadName: workloadName,
			Namespace:    namespace,
			Health:       health >= 1.0,
		}
		collectedMetrics = append(collectedMetrics, workloadMetrics)
	}

	klog.V(2).InfoS("Collected workload metrics from Prometheus", "count", len(collectedMetrics))
	return collectedMetrics, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("metriccollector-controller").
		For(&autoapprovev1alpha1.MetricCollectorReport{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
