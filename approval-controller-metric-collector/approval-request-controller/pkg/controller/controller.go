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

// Package controller features a controller to reconcile ApprovalRequest objects
// and create MetricCollectorReport resources on the hub cluster for metric collection.
package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	localv1alpha1 "github.com/kubefleet-dev/kubefleet-cookbook/approval-controller-metric-collector/approval-request-controller/apis/metric/v1alpha1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	// metricCollectorFinalizer is the finalizer added to ApprovalRequest objects for cleanup
	metricCollectorFinalizer = "kubernetes-fleet.io/metric-collector-report-cleanup"

	// prometheusURL is the default Prometheus URL to use for all clusters
	prometheusURL = "http://prometheus.prometheus.svc.cluster.local:9090"
)

// Reconciler reconciles an ApprovalRequest object and creates MetricCollectorReport resources
// on the hub cluster in fleet-member-{clusterName} namespaces.
type Reconciler struct {
	client.Client
	recorder record.EventRecorder
}

// Reconcile reconciles an ApprovalRequest or ClusterApprovalRequest object.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("ApprovalRequest reconciliation starts", "request", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("ApprovalRequest reconciliation ends", "request", req.NamespacedName, "latency", latency)
	}()

	var approvalReqObj placementv1beta1.ApprovalRequestObj
	var isClusterScoped bool

	// Check if request has a namespace to determine resource type
	if req.Namespace != "" {
		// Fetch namespaced ApprovalRequest
		approvalReq := &placementv1beta1.ApprovalRequest{}
		if err := r.Client.Get(ctx, req.NamespacedName, approvalReq); err != nil {
			if errors.IsNotFound(err) {
				klog.V(2).InfoS("ApprovalRequest not found, ignoring", "request", req.NamespacedName)
				return ctrl.Result{}, nil
			}
			klog.ErrorS(err, "Failed to get ApprovalRequest", "request", req.NamespacedName)
			return ctrl.Result{}, err
		}
		approvalReqObj = approvalReq
		isClusterScoped = false
	} else {
		// Fetch cluster-scoped ClusterApprovalRequest
		clusterApprovalReq := &placementv1beta1.ClusterApprovalRequest{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: req.Name}, clusterApprovalReq); err != nil {
			if errors.IsNotFound(err) {
				klog.V(2).InfoS("ClusterApprovalRequest not found, ignoring", "request", req.Name)
				return ctrl.Result{}, nil
			}
			klog.ErrorS(err, "Failed to get ClusterApprovalRequest", "request", req.Name)
			return ctrl.Result{}, err
		}
		approvalReqObj = clusterApprovalReq
		isClusterScoped = true
	}

	return r.reconcileApprovalRequestObj(ctx, approvalReqObj, isClusterScoped)
}

// reconcileApprovalRequestObj reconciles an ApprovalRequestObj (either ApprovalRequest or ClusterApprovalRequest).
func (r *Reconciler) reconcileApprovalRequestObj(ctx context.Context, approvalReqObj placementv1beta1.ApprovalRequestObj, isClusterScoped bool) (ctrl.Result, error) {
	obj := approvalReqObj.(client.Object)
	approvalReqRef := klog.KObj(obj)

	// Handle deletion
	if !obj.GetDeletionTimestamp().IsZero() {
		return r.handleDelete(ctx, approvalReqObj)
	}

	// Check if the approval request is already approved or rejected - stop reconciliation if so
	approvedCond := meta.FindStatusCondition(approvalReqObj.GetApprovalRequestStatus().Conditions, string(placementv1beta1.ApprovalRequestConditionApproved))
	if approvedCond != nil && approvedCond.Status == metav1.ConditionTrue {
		klog.V(2).InfoS("ApprovalRequest has been approved, stopping reconciliation", "approvalRequest", approvalReqRef)
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(obj, metricCollectorFinalizer) {
		controllerutil.AddFinalizer(obj, metricCollectorFinalizer)
		if err := r.Client.Update(ctx, obj); err != nil {
			klog.ErrorS(err, "Failed to add finalizer", "approvalRequest", approvalReqRef)
			return ctrl.Result{}, err
		}
		klog.V(2).InfoS("Added finalizer to ApprovalRequest", "approvalRequest", approvalReqRef)
	}

	// Get the UpdateRun (ClusterStagedUpdateRun or StagedUpdateRun)
	spec := approvalReqObj.GetApprovalRequestSpec()
	updateRunName := spec.TargetUpdateRun
	stageName := spec.TargetStage

	var stageStatus *placementv1beta1.StageUpdatingStatus
	if isClusterScoped {
		updateRun := &placementv1beta1.ClusterStagedUpdateRun{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: updateRunName}, updateRun); err != nil {
			klog.ErrorS(err, "Failed to get ClusterStagedUpdateRun", "approvalRequest", approvalReqRef, "updateRun", updateRunName)
			return ctrl.Result{}, err
		}

		// Find the stage
		for i := range updateRun.Status.StagesStatus {
			if updateRun.Status.StagesStatus[i].StageName == stageName {
				stageStatus = &updateRun.Status.StagesStatus[i]
				break
			}
		}
	} else {
		updateRun := &placementv1beta1.StagedUpdateRun{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: updateRunName, Namespace: obj.GetNamespace()}, updateRun); err != nil {
			klog.ErrorS(err, "Failed to get StagedUpdateRun", "approvalRequest", approvalReqRef, "updateRun", updateRunName)
			return ctrl.Result{}, err
		}

		// Find the stage
		for i := range updateRun.Status.StagesStatus {
			if updateRun.Status.StagesStatus[i].StageName == stageName {
				stageStatus = &updateRun.Status.StagesStatus[i]
				break
			}
		}
	}

	if stageStatus == nil {
		err := fmt.Errorf("stage %s not found in UpdateRun %s", stageName, updateRunName)
		klog.ErrorS(err, "Failed to find stage", "approvalRequest", approvalReqRef)
		return ctrl.Result{}, err
	}

	// Get all cluster names from the stage
	clusterNames := make([]string, 0, len(stageStatus.Clusters))
	for _, cluster := range stageStatus.Clusters {
		clusterNames = append(clusterNames, cluster.ClusterName)
	}

	if len(clusterNames) == 0 {
		klog.V(2).InfoS("No clusters in stage, skipping", "approvalRequest", approvalReqRef, "stage", stageName)
		return ctrl.Result{}, nil
	}

	klog.V(2).InfoS("Found clusters in stage", "approvalRequest", approvalReqRef, "stage", stageName, "clusters", clusterNames)

	// Create or update MetricCollectorReport resources in fleet-member namespaces
	if err := r.ensureMetricCollectorReports(ctx, obj, clusterNames, updateRunName, stageName); err != nil {
		klog.ErrorS(err, "Failed to ensure MetricCollectorReport resources", "approvalRequest", approvalReqRef)
		return ctrl.Result{}, err
	}

	klog.V(2).InfoS("Successfully ensured MetricCollectorReport resources", "approvalRequest", approvalReqRef, "clusters", clusterNames)

	// Check workload health and approve if all workloads are healthy
	if err := r.checkWorkloadHealthAndApprove(ctx, approvalReqObj, clusterNames, updateRunName, stageName); err != nil {
		klog.ErrorS(err, "Failed to check workload health", "approvalRequest", approvalReqRef)
		return ctrl.Result{}, err
	}

	// Requeue after 15 seconds to check again (will stop if approved in next reconciliation)
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// ensureMetricCollectorReports creates MetricCollectorReport in each fleet-member-{clusterName} namespace
func (r *Reconciler) ensureMetricCollectorReports(
	ctx context.Context,
	approvalReq client.Object,
	clusterNames []string,
	updateRunName, stageName string,
) error {
	// Generate report name (same for all clusters, different namespaces)
	reportName := fmt.Sprintf("mc-%s-%s", updateRunName, stageName)

	// Create MetricCollectorReport in each fleet-member namespace
	for _, clusterName := range clusterNames {
		reportNamespace := fmt.Sprintf(utils.NamespaceNameFormat, clusterName)

		report := &localv1alpha1.MetricCollectorReport{
			ObjectMeta: metav1.ObjectMeta{
				Name:      reportName,
				Namespace: reportNamespace,
				Labels: map[string]string{
					"approval-request": approvalReq.GetName(),
					"update-run":       updateRunName,
					"stage":            stageName,
					"cluster":          clusterName,
				},
			},
			Spec: localv1alpha1.MetricCollectorReportSpec{
				PrometheusURL: prometheusURL,
			},
		}

		// Create or update MetricCollectorReport
		existingReport := &localv1alpha1.MetricCollectorReport{}
		err := r.Client.Get(ctx, types.NamespacedName{
			Name:      reportName,
			Namespace: reportNamespace,
		}, existingReport)

		if err != nil {
			if errors.IsNotFound(err) {
				if err := r.Client.Create(ctx, report); err != nil {
					return fmt.Errorf("failed to create MetricCollectorReport in %s: %w", reportNamespace, err)
				}
				klog.V(2).InfoS("Created MetricCollectorReport",
					"report", reportName,
					"namespace", reportNamespace,
					"cluster", clusterName)
			} else {
				return fmt.Errorf("failed to get MetricCollectorReport in %s: %w", reportNamespace, err)
			}
		} else {
			// Update spec if needed
			if existingReport.Spec.PrometheusURL != prometheusURL {
				existingReport.Spec.PrometheusURL = prometheusURL
				if err := r.Client.Update(ctx, existingReport); err != nil {
					return fmt.Errorf("failed to update MetricCollectorReport in %s: %w", reportNamespace, err)
				}
				klog.V(2).InfoS("Updated MetricCollectorReport",
					"report", reportName,
					"namespace", reportNamespace,
					"cluster", clusterName)
			}
		}
	}

	return nil
}

// checkWorkloadHealthAndApprove checks if all workloads specified in ClusterStagedWorkloadTracker or StagedWorkloadTracker are healthy
// across all clusters in the stage, and approves the ApprovalRequest if they are.
func (r *Reconciler) checkWorkloadHealthAndApprove(
	ctx context.Context,
	approvalReqObj placementv1beta1.ApprovalRequestObj,
	clusterNames []string,
	updateRunName, stageName string,
) error {
	obj := approvalReqObj.(client.Object)
	approvalReqRef := klog.KObj(obj)

	klog.V(2).InfoS("Starting workload health check", "approvalRequest", approvalReqRef, "clusters", clusterNames)

	// Get the appropriate WorkloadTracker based on scope
	// The WorkloadTracker name matches the UpdateRun name
	var workloads []localv1alpha1.WorkloadReference
	var workloadTrackerName string

	if obj.GetNamespace() == "" {
		// Cluster-scoped: Get ClusterStagedWorkloadTracker with same name as ClusterStagedUpdateRun
		clusterWorkloadTracker := &localv1alpha1.ClusterStagedWorkloadTracker{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: updateRunName}, clusterWorkloadTracker); err != nil {
			if errors.IsNotFound(err) {
				klog.V(2).InfoS("ClusterStagedWorkloadTracker not found, skipping health check",
					"approvalRequest", approvalReqRef,
					"updateRun", updateRunName)
				return nil
			}
			klog.ErrorS(err, "Failed to get ClusterStagedWorkloadTracker", "approvalRequest", approvalReqRef, "updateRun", updateRunName)
			return fmt.Errorf("failed to get ClusterStagedWorkloadTracker: %w", err)
		}
		workloads = clusterWorkloadTracker.Workloads
		workloadTrackerName = clusterWorkloadTracker.Name
		klog.V(2).InfoS("Found ClusterStagedWorkloadTracker",
			"approvalRequest", approvalReqRef,
			"workloadTracker", workloadTrackerName,
			"workloadCount", len(workloads))
	} else {
		// Namespace-scoped: Get StagedWorkloadTracker with same name and namespace as StagedUpdateRun
		stagedWorkloadTracker := &localv1alpha1.StagedWorkloadTracker{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: updateRunName, Namespace: obj.GetNamespace()}, stagedWorkloadTracker); err != nil {
			if errors.IsNotFound(err) {
				klog.V(2).InfoS("StagedWorkloadTracker not found, skipping health check",
					"approvalRequest", approvalReqRef,
					"updateRun", updateRunName,
					"namespace", obj.GetNamespace())
				return nil
			}
			klog.ErrorS(err, "Failed to get StagedWorkloadTracker", "approvalRequest", approvalReqRef, "updateRun", updateRunName)
			return fmt.Errorf("failed to get StagedWorkloadTracker: %w", err)
		}
		workloads = stagedWorkloadTracker.Workloads
		workloadTrackerName = stagedWorkloadTracker.Name
		klog.V(2).InfoS("Found StagedWorkloadTracker",
			"approvalRequest", approvalReqRef,
			"workloadTracker", klog.KObj(stagedWorkloadTracker),
			"workloadCount", len(workloads))
	}

	if len(workloads) == 0 {
		klog.V(2).InfoS("WorkloadTracker has no workloads defined, skipping health check",
			"approvalRequest", approvalReqRef,
			"workloadTracker", workloadTrackerName)
		return nil
	}

	// MetricCollectorReport name is same as MetricCollector name
	metricCollectorName := fmt.Sprintf("mc-%s-%s", updateRunName, stageName)

	// Check each cluster for the required workloads
	allHealthy := true
	unhealthyDetails := []string{}

	for _, clusterName := range clusterNames {
		reportNamespace := fmt.Sprintf(utils.NamespaceNameFormat, clusterName)

		klog.V(2).InfoS("Checking MetricCollectorReport",
			"approvalRequest", approvalReqRef,
			"cluster", clusterName,
			"reportName", metricCollectorName,
			"reportNamespace", reportNamespace)

		// Get MetricCollectorReport for this cluster
		report := &localv1alpha1.MetricCollectorReport{}
		err := r.Client.Get(ctx, types.NamespacedName{
			Name:      metricCollectorName,
			Namespace: reportNamespace,
		}, report)

		if err != nil {
			if errors.IsNotFound(err) {
				klog.V(2).InfoS("MetricCollectorReport not found yet",
					"approvalRequest", approvalReqRef,
					"cluster", clusterName,
					"report", metricCollectorName,
					"namespace", reportNamespace)
				allHealthy = false
				unhealthyDetails = append(unhealthyDetails, fmt.Sprintf("cluster %s: report not found", clusterName))
				continue
			}
			klog.ErrorS(err, "Failed to get MetricCollectorReport",
				"approvalRequest", approvalReqRef,
				"cluster", clusterName,
				"report", metricCollectorName,
				"namespace", reportNamespace)
			return fmt.Errorf("failed to get MetricCollectorReport for cluster %s: %w", clusterName, err)
		}

		klog.V(2).InfoS("Found MetricCollectorReport",
			"approvalRequest", approvalReqRef,
			"cluster", clusterName,
			"collectedMetrics", len(report.Status.CollectedMetrics),
			"workloadsMonitored", report.Status.WorkloadsMonitored)

		// Check if all workloads from WorkloadTracker are present and healthy
		for _, trackedWorkload := range workloads {
			found := false
			healthy := false

			for _, collectedMetric := range report.Status.CollectedMetrics {
				if collectedMetric.Namespace == trackedWorkload.Namespace &&
					collectedMetric.WorkloadName == trackedWorkload.Name {
					found = true
					healthy = collectedMetric.Health
					klog.V(3).InfoS("Workload metric found",
						"approvalRequest", approvalReqRef,
						"cluster", clusterName,
						"workload", trackedWorkload.Name,
						"namespace", trackedWorkload.Namespace,
						"healthy", healthy)
					break
				}
			}

			if !found {
				klog.V(2).InfoS("Workload not found in MetricCollectorReport",
					"approvalRequest", approvalReqRef,
					"cluster", clusterName,
					"workload", trackedWorkload.Name,
					"namespace", trackedWorkload.Namespace)
				allHealthy = false
				unhealthyDetails = append(unhealthyDetails,
					fmt.Sprintf("cluster %s: workload %s/%s not found", clusterName, trackedWorkload.Namespace, trackedWorkload.Name))
			} else if !healthy {
				klog.V(2).InfoS("Workload is not healthy",
					"approvalRequest", approvalReqRef,
					"cluster", clusterName,
					"workload", trackedWorkload.Name,
					"namespace", trackedWorkload.Namespace)
				allHealthy = false
				unhealthyDetails = append(unhealthyDetails,
					fmt.Sprintf("cluster %s: workload %s/%s unhealthy", clusterName, trackedWorkload.Namespace, trackedWorkload.Name))
			}
		}
	}

	// If all workloads are healthy across all clusters, approve the ApprovalRequest
	if allHealthy {
		klog.InfoS("All workloads are healthy, approving ApprovalRequest",
			"approvalRequest", approvalReqRef,
			"clusters", clusterNames,
			"workloads", len(workloads))

		status := approvalReqObj.GetApprovalRequestStatus()
		approvedCond := meta.FindStatusCondition(status.Conditions, string(placementv1beta1.ApprovalRequestConditionApproved))

		// Only update if not already approved
		if approvedCond == nil || approvedCond.Status != metav1.ConditionTrue {
			meta.SetStatusCondition(&status.Conditions, metav1.Condition{
				Type:               string(placementv1beta1.ApprovalRequestConditionApproved),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: obj.GetGeneration(),
				Reason:             "AllWorkloadsHealthy",
				Message:            fmt.Sprintf("All %d workloads are healthy across %d clusters", len(workloads), len(clusterNames)),
			})

			approvalReqObj.SetApprovalRequestStatus(*status)
			if err := r.Client.Status().Update(ctx, obj); err != nil {
				klog.ErrorS(err, "Failed to approve ApprovalRequest", "approvalRequest", approvalReqRef)
				return fmt.Errorf("failed to approve ApprovalRequest: %w", err)
			}

			klog.InfoS("Successfully approved ApprovalRequest", "approvalRequest", approvalReqRef)
			r.recorder.Event(obj, "Normal", "Approved", fmt.Sprintf("All %d workloads are healthy across %d clusters in stage %s", len(workloads), len(clusterNames), stageName))
		} else {
			klog.V(2).InfoS("ApprovalRequest already approved", "approvalRequest", approvalReqRef)
		}

		// Approval successful or already approved
		return nil
	}

	// Not all workloads are healthy yet, log details and return nil (reconcile will requeue)
	klog.V(2).InfoS("Not all workloads are healthy yet",
		"approvalRequest", approvalReqRef,
		"unhealthyDetails", unhealthyDetails)

	return nil
}

// handleDelete handles the deletion of an ApprovalRequest or ClusterApprovalRequest
func (r *Reconciler) handleDelete(ctx context.Context, approvalReqObj placementv1beta1.ApprovalRequestObj) (ctrl.Result, error) {
	obj := approvalReqObj.(client.Object)
	if !controllerutil.ContainsFinalizer(obj, metricCollectorFinalizer) {
		return ctrl.Result{}, nil
	}

	approvalReqRef := klog.KObj(obj)
	klog.V(2).InfoS("Cleaning up MetricCollectorReports for ApprovalRequest", "approvalRequest", approvalReqRef)

	// Get cluster names from UpdateRun to know which reports to delete
	spec := approvalReqObj.GetApprovalRequestSpec()
	updateRunName := spec.TargetUpdateRun
	stageName := spec.TargetStage
	reportName := fmt.Sprintf("mc-%s-%s", updateRunName, stageName)

	// Fetch UpdateRun to get cluster names
	var clusterNames []string
	if obj.GetNamespace() == "" {
		// Cluster-scoped: Get ClusterStagedUpdateRun
		updateRun := &placementv1beta1.ClusterStagedUpdateRun{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: updateRunName}, updateRun); err != nil {
			if !errors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to get ClusterStagedUpdateRun for cleanup", "approvalRequest", approvalReqRef)
			}
			// Continue with finalizer removal even if UpdateRun not found
		} else {
			// Find the stage
			for i := range updateRun.Status.StagesStatus {
				if updateRun.Status.StagesStatus[i].StageName == stageName {
					for _, cluster := range updateRun.Status.StagesStatus[i].Clusters {
						clusterNames = append(clusterNames, cluster.ClusterName)
					}
					break
				}
			}
		}
	} else {
		// Namespace-scoped: Get StagedUpdateRun
		updateRun := &placementv1beta1.StagedUpdateRun{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: updateRunName, Namespace: obj.GetNamespace()}, updateRun); err != nil {
			if !errors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to get StagedUpdateRun for cleanup", "approvalRequest", approvalReqRef)
			}
			// Continue with finalizer removal even if UpdateRun not found
		} else {
			// Find the stage
			for i := range updateRun.Status.StagesStatus {
				if updateRun.Status.StagesStatus[i].StageName == stageName {
					for _, cluster := range updateRun.Status.StagesStatus[i].Clusters {
						clusterNames = append(clusterNames, cluster.ClusterName)
					}
					break
				}
			}
		}
	}

	// Delete MetricCollectorReport from each fleet-member namespace
	for _, clusterName := range clusterNames {
		reportNamespace := fmt.Sprintf(utils.NamespaceNameFormat, clusterName)
		report := &localv1alpha1.MetricCollectorReport{}

		if err := r.Client.Get(ctx, types.NamespacedName{
			Name:      reportName,
			Namespace: reportNamespace,
		}, report); err == nil {
			if err := r.Client.Delete(ctx, report); err != nil && !errors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to delete MetricCollectorReport",
					"report", reportName,
					"namespace", reportNamespace,
					"cluster", clusterName)
				return ctrl.Result{}, fmt.Errorf("failed to delete MetricCollectorReport in %s: %w", reportNamespace, err)
			}
			klog.V(2).InfoS("Deleted MetricCollectorReport",
				"report", reportName,
				"namespace", reportNamespace,
				"cluster", clusterName)
		}
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(obj, metricCollectorFinalizer)
	if err := r.Client.Update(ctx, obj); err != nil {
		klog.ErrorS(err, "Failed to remove finalizer", "approvalRequest", approvalReqRef)
		return ctrl.Result{}, err
	}

	klog.V(2).InfoS("Successfully cleaned up MetricCollectorReports", "approvalRequest", approvalReqRef, "clusters", clusterNames)
	return ctrl.Result{}, nil
}

// SetupWithManagerForClusterApprovalRequest sets up the controller with the Manager for ClusterApprovalRequest resources.
func (r *Reconciler) SetupWithManagerForClusterApprovalRequest(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("clusterapprovalrequest-controller")
	return ctrl.NewControllerManagedBy(mgr).
		Named("clusterapprovalrequest-controller").
		For(&placementv1beta1.ClusterApprovalRequest{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// SetupWithManagerForApprovalRequest sets up the controller with the Manager for ApprovalRequest resources.
func (r *Reconciler) SetupWithManagerForApprovalRequest(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("approvalrequest-controller")
	return ctrl.NewControllerManagedBy(mgr).
		Named("approvalrequest-controller").
		For(&placementv1beta1.ApprovalRequest{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
