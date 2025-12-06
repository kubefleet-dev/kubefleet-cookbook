# ApprovalRequest Controller

The ApprovalRequest Controller is a standalone controller that runs on the **hub cluster** to automate approval decisions for staged updates based on workload health metrics.

## Overview

This controller is designed to be a standalone component that can run independently from the main kubefleet repository. It:
- Uses kubefleet v0.1.2 as an external dependency
- Includes its own APIs for MetricCollectorReport and WorkloadTracker
- Watches `ApprovalRequest` and `ClusterApprovalRequest` resources (from kubefleet)
- Creates `MetricCollector` resources on member clusters via ClusterResourcePlacement
- Monitors workload health via `MetricCollectorReport` objects
- Automatically approves requests when all tracked workloads are healthy
- Runs every 15 seconds to check health status

## Architecture

The controller is designed to run on the hub cluster and:
1. Deploys MetricCollector instances to member clusters using CRP
2. Collects health metrics from MetricCollectorReports
3. Compares metrics against WorkloadTracker specifications
4. Approves ApprovalRequests when all workloads are healthy

## Installation

### Prerequisites

The following CRDs must be installed on the hub cluster (installed by kubefleet hub-agent):
- `approvalrequests.placement.kubernetes-fleet.io`
- `clusterapprovalrequests.placement.kubernetes-fleet.io`
- `clusterresourceplacements.placement.kubernetes-fleet.io`
- `clusterresourceoverrides.placement.kubernetes-fleet.io`
- `clusterstagedupdateruns.placement.kubernetes-fleet.io`
- `stagedupdateruns.placement.kubernetes-fleet.io`

The following CRDs are installed by this chart:
- `metriccollectors.metric.kubernetes-fleet.io`
- `metriccollectorreports.metric.kubernetes-fleet.io`
- `workloadtrackers.metric.kubernetes-fleet.io`

### Install via Helm

```bash
# Build the image
make docker-build IMAGE_NAME=approval-request-controller IMAGE_TAG=latest

# Load into kind (if using kind)
kind load docker-image approval-request-controller:latest --name hub

# Install the chart
helm install approval-request-controller ./charts/approval-request-controller \
  --namespace fleet-system \
  --create-namespace
```

## Configuration

The controller watches for:
- `ApprovalRequest` (namespaced)
- `ClusterApprovalRequest` (cluster-scoped)

Both resources from kubefleet are monitored, and the controller creates `MetricCollector` resources on appropriate member clusters based on the staged update configuration.

### Health Check Interval

The controller checks workload health every **15 seconds**. This interval is configurable via the `reconcileInterval` parameter in the Helm chart.

## API Reference

### WorkloadTracker

`WorkloadTracker` is a cluster-scoped custom resource that defines which workloads the approval controller should monitor for health metrics before auto-approving staged rollouts.

#### Example: Single Workload

```yaml
apiVersion: metric.kubernetes-fleet.io/v1beta1
kind: WorkloadTracker
metadata:
  name: sample-workload-tracker
workloads:
  - name: sample-metric-app
    namespace: test-ns
```

#### Example: Multiple Workloads

```yaml
apiVersion: metric.kubernetes-fleet.io/v1beta1
kind: WorkloadTracker
metadata:
  name: multi-workload-tracker
workloads:
  - name: frontend
    namespace: production
  - name: backend-api
    namespace: production
  - name: worker-service
    namespace: production
```

#### Usage Notes

- **Cluster-scoped:** WorkloadTracker is a cluster-scoped resource, not namespaced
- **Optional:** If no WorkloadTracker exists, the controller will skip health checks and won't auto-approve
- **Single instance:** The controller expects one WorkloadTracker per cluster and uses the first one found
- **Health criteria:** All workloads listed must report healthy (metric value = 1.0) before approval
- **Prometheus metrics:** Each workload should expose `workload_health` metrics that the MetricCollector can query

For a complete example, see: [`./examples/workloadtracker/workloadtracker.yaml`](./examples/workloadtracker/workloadtracker.yaml)

## Additional Resources

- **Main Tutorial:** See [`../README.md`](../README.md) for a complete end-to-end tutorial on setting up automated staged rollouts with approval automation
- **Metric Collector:** See [`../metric-collector/README.md`](../metric-collector/README.md) for details on the metric collection component that runs on member clusters
- **KubeFleet Documentation:** [Azure/fleet](https://github.com/Azure/fleet) - Multi-cluster orchestration platform
- **Example Configurations:**
  - [`./examples/workloadtracker/`](./examples/workloadtracker/) - WorkloadTracker resource examples
  - [`./examples/stagedupdaterun/`](./examples/stagedupdaterun/) - Staged update configuration examples
  - [`./examples/prometheus/`](./examples/prometheus/) - Prometheus deployment and configuration for metric collection
```
