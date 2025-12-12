# Metric Collector

The Metric Collector is a standalone controller that runs on **member clusters** to collect workload health metrics from Prometheus and report them back to the hub cluster.

## Overview

This controller is designed to be a standalone component that can run independently on member clusters. It:
- Watches `MetricCollector` resources deployed to the member cluster
- Queries local Prometheus for `workload_health` metrics
- Creates/updates `MetricCollectorReport` resources on the hub cluster
- Supports both token-based and certificate-based authentication to the hub
- Runs every 30 seconds (configurable) to collect and report metrics

## Architecture

The controller runs on member clusters and:
1. Receives `MetricCollector` resources via KubeFleet's ResourcePlacement
2. Queries the local Prometheus endpoint for workload health metrics
3. Parses metrics and extracts workload names, namespaces, and health status
4. Reports collected metrics to the hub cluster in `fleet-member-<cluster-name>` namespace
5. Maintains continuous metric collection and reporting

## Installation

### Prerequisites

**On Hub Cluster:**
- KubeFleet hub-agent installed
- `MetricCollectorReport` CRD installed (installed by approval-request-controller)
- RBAC permissions for metric-collector service account to create/update reports

**On Member Cluster:**
- Prometheus deployed and accessible
- Workloads exposing `workload_health` metrics
- Network connectivity to hub cluster API server

### Install via Script

Use the provided installation script to install the metric collector on all member clusters:

```bash
# Run from the metric-collector directory
./install-on-member.sh 3  # For 3 member clusters
```

This script automatically:
1. Builds the `metric-collector:latest` image
2. Builds the `metric-app:local` image (sample app)
3. Loads both images into each kind cluster
4. Creates hub token secret with proper RBAC on hub
5. Installs the metric-collector via Helm on each member

For detailed step-by-step setup instructions, see the [main tutorial](../README.md).

## Verification

### Check Controller Status

```bash
# Check pod status
kubectl get pods -n default -l app.kubernetes.io/name=metric-collector

# Check logs
kubectl logs -n default -l app.kubernetes.io/name=metric-collector -f
```

### Check MetricCollector Resources

```bash
# View MetricCollector resources on member cluster
kubectl get metriccollector -A
```

### Check Reports on Hub

```bash
# Switch to hub cluster
kubectl config use-context kind-hub

# View reports for this cluster
kubectl get metriccollectorreport -n fleet-member-cluster-1

# View report details
kubectl describe metriccollectorreport <report-name> -n fleet-member-cluster-1
```

## Additional Resources

- [Main Tutorial](../README.md)
- [Approval Request Controller](../approval-request-controller/README.md)
- [KubeFleet Documentation](https://github.com/Azure/kubefleet)
