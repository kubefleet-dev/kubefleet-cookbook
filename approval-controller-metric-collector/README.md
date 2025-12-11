# Approval Controller and Metric Collector Tutorial

This tutorial demonstrates how to use the Approval Request Controller and Metric Collector with KubeFleet for automated staged rollout approvals based on workload health metrics.

## Overview

This directory contains two controllers:
- **approval-request-controller**: Runs on the hub cluster to automate approval decisions for staged updates
- **metric-collector**: Runs on member clusters to collect workload health metrics from Prometheus

![Approval Controller and Metric Collector Architecture](./approval-controller-metric-collector.png)

## How It Works

### Custom Resource Definitions (CRDs)

This solution introduces three new CRDs that work together with KubeFleet's native resources:

#### Hub Cluster CRDs

1. **MetricCollector** (cluster-scoped)
   - Defines Prometheus connection details and where to report metrics
   - Gets propagated to member clusters via ClusterResourcePlacement (CRP)
   - Each member cluster receives a customized version with its specific `reportNamespace`

2. **MetricCollectorReport** (namespaced)
   - Created by metric-collector on member clusters, reported back to hub
   - Lives in `fleet-member-<cluster-name>` namespaces on the hub
   - Contains collected `workload_health` metrics for all workloads in a cluster
   - Updated every 30 seconds by the metric collector

3. **ClusterStagedWorkloadTracker** (cluster-scoped)
   - Defines which workloads to monitor for a ClusterStagedUpdateRun
   - The name must match the ClusterStagedUpdateRun name
   - Specifies workload's name, namespace and expected health status
   - Used by approval-request-controller to determine if stage is ready for approval

4. **StagedWorkloadTracker** (namespaced)
   - Defines which workloads to monitor for a StagedUpdateRun
   - The name and namespace must match the StagedUpdateRun name and namespace
   - Specifies namespace, workload name, and expected health status
   - Used by approval-request-controller to determine if stage is ready for approval

### Automated Approval Flow

1. **Stage Initialization**
   - User creates an UpdateRun (`ClusterStagedUpdateRun` or `StagedUpdateRun`) on the hub
   - KubeFleet creates an ApprovalRequest (`ClusterApprovalRequest` or `ApprovalRequest`) for the first stage
   - The ApprovalRequest enters "Pending" state, waiting for approval

2. **Metric Collector Deployment**
   - Approval-request-controller watches the `ClusterApprovalRequest`, `ApprovalRequest` objects
   - Creates a `MetricCollector` resource on the hub (cluster-scoped)
   - Creates a `ClusterResourceOverride` with per-cluster customization rules
     - Each cluster gets a unique `reportNamespace`: `fleet-member-<cluster-name>`
   - Creates a `ClusterResourcePlacement` (CRP) with `PickFixed` policy
     - Targets all clusters in the current stage
   - KubeFleet propagates the customized `MetricCollector` to each member cluster

3. **Metric Collection on Member Clusters**
   - Metric-collector controller runs on each member cluster
   - Every 30 seconds, it:
     - Queries local Prometheus with PromQL: `workload_health`
     - Prometheus returns metrics for all pods with `prometheus.io/scrape: "true"` annotation
     - Extracts workload health (1.0 = healthy, 0.0 = unhealthy)
     - Creates/updates `MetricCollectorReport` on hub in `fleet-member-<cluster-name>` namespace
   
4. **Health Evaluation**
   - Approval-request-controller monitors `MetricCollectorReports` from all stage clusters
   - Every 15 seconds, it:
     - Fetches the appropriate workload tracker:
       - For cluster-scoped: `ClusterStagedWorkloadTracker` with same name as ClusterStagedUpdateRun
       - For namespace-scoped: `StagedWorkloadTracker` with same name and namespace as StagedUpdateRun
     - For each cluster in the stage:
       - Reads its `MetricCollectorReport` from `fleet-member-<cluster-name>` namespace
       - Verifies all tracked workloads are present and healthy
     - If any workload is missing or unhealthy, waits for next cycle
     - If ALL workloads across ALL clusters are healthy:
       - Sets ApprovalRequest condition `Approved: True`
       - KubeFleet proceeds to roll out the stage

5. **Stage Progression**
   - KubeFleet applies the update to the approved stage clusters
   - Creates a new ApprovalRequest for the next stage (if any)
   - The cycle repeats for each stage

## Prerequisites

- Docker or Podman for building images
- kubectl configured with access to your clusters
- Helm 3.x
- KubeFleet installed on hub and member clusters

## Setup Overview

Before diving into the setup steps, here's a bird's eye view of what you'll be building:

### Architecture Components

**Hub Cluster** - The control plane where you'll deploy:
1. **3 Member Clusters** (kind-cluster-1, kind-cluster-2, kind-cluster-3)
   - Labeled with `environment=staging` or `environment=prod`
   - These labels determine which stage each cluster belongs to during rollouts

2. **Prometheus** (propagated to all clusters)
   - Monitors workload health via `/metrics` endpoints
   - Scrapes pods with `prometheus.io/scrape: "true"` annotation
   - Provides `workload_health` metric (1.0 = healthy, 0.0 = unhealthy)

3. **Approval Request Controller**
   - Watches `ClusterApprovalRequest` and `ApprovalRequest` objects
   - Deploys MetricCollector to stage clusters via ClusterResourcePlacement
   - Evaluates workload health from MetricCollectorReports
   - Auto-approves stages when all workloads are healthy

4. **Sample Metric App** (will be rolled out to clusters)
   - Simple Go application exposing `/metrics` endpoint
   - Reports `workload_health=1.0` by default
   - Used to demonstrate health-based approvals

**Member Clusters** - Where workloads run:
1. **Metric Collector**
   - Queries local Prometheus every 30 seconds
   - Reports workload health back to hub cluster
   - Creates/updates MetricCollectorReport in hub's `fleet-member-<cluster-name>` namespace

2. **Prometheus** (received from hub)
   - Runs on each member cluster
   - Scrapes local workload metrics

3. **Sample Metric App** (received from hub)
   - Deployed via staged rollout
   - Monitored for health during updates

### WorkloadTracker - The Decision Maker

The **WorkloadTracker** is a critical resource that tells the approval controller which workloads must be healthy before approving a stage. Without it, the controller doesn't know what to monitor.

**Two Types:**

1. **ClusterStagedWorkloadTracker** (for ClusterStagedUpdateRun)
   - Cluster-scoped resource on the hub
   - Name must exactly match the ClusterStagedUpdateRun name
   - Example: If your UpdateRun is named `example-cluster-staged-run`, the tracker must also be named `example-cluster-staged-run`
   - Contains a list of workloads (name + namespace) to monitor across all clusters in each stage

2. **StagedWorkloadTracker** (for StagedUpdateRun)
   - Namespace-scoped resource on the hub
   - Name and namespace must exactly match the StagedUpdateRun
   - Example: If your UpdateRun is `example-staged-run` in namespace `test-ns`, the tracker must be `example-staged-run` in `test-ns`
   - Contains a list of workloads to monitor

**How It Works:**
```yaml
# ClusterStagedWorkloadTracker example
workloads:
  - name: sample-metric-app    # Deployment name
    namespace: test-ns         # Namespace where it runs
```

When the approval controller evaluates a stage:
1. It fetches the WorkloadTracker that matches the UpdateRun name (and namespace)
2. For each cluster in the stage, it reads the MetricCollectorReport
3. It verifies that every workload listed in the tracker appears in the report with `health=1.0`
4. Only when ALL workloads in ALL clusters are healthy does it approve the stage

**Critical Rule:** The WorkloadTracker must be created BEFORE starting the UpdateRun. If the controller can't find a matching tracker, it won't approve any stages.

### The Staged Rollout Flow

When you create a **ClusterStagedUpdateRun** or **StagedUpdateRun**, here's what happens:

1. **Stage 1 (staging)**: Rollout starts with `kind-cluster-1`
   - KubeFleet creates an ApprovalRequest for the staging stage
   - Approval controller deploys MetricCollector to `kind-cluster-1`
   - Metric collector reports health metrics back to hub
   - When `sample-metric-app` is healthy, approval controller auto-approves
   - KubeFleet proceeds with the rollout to `kind-cluster-1`

2. **Stage 2 (prod)**: After staging succeeds
   - KubeFleet creates an ApprovalRequest for the prod stage
   - Approval controller deploys MetricCollector to `kind-cluster-2` and `kind-cluster-3`
   - Metric collectors report health from both clusters
   - When ALL workloads across BOTH prod clusters are healthy, auto-approve
   - KubeFleet completes the rollout to production clusters

### Key Resources You'll Create

| Resource | Purpose | Where |
|----------|---------|-------|
| **MemberCluster** | Register member clusters with hub, apply stage labels | Hub |
| **ClusterResourcePlacement** | Define what resources to propagate (Prometheus, sample-app) | Hub |
| **StagedUpdateStrategy** | Define stages with label selectors and approval requirements | Hub |
| **WorkloadTracker** | Specify which workloads to monitor for health | Hub |
| **UpdateRun** | Start the staged rollout process | Hub |
| **MetricCollector** | Automatically created by approval controller per stage | Hub → Member |
| **MetricCollectorReport** | Automatically created by metric collector | Member → Hub |

### What the Installation Scripts Do

**`install-on-hub.sh`** (Approval Request Controller):
- Builds controller Docker image with multi-arch support
- Loads image into kind hub cluster
- Verifies KubeFleet CRDs are installed
- Installs controller via Helm with custom CRDs (MetricCollector, MetricCollectorReport, WorkloadTracker)
- Sets up RBAC for managing placements, overrides, and approval requests

**`install-on-member.sh`** (Metric Collector):
- Builds metric-collector and metric-app Docker images
- Loads both images into each kind member cluster
- Creates service account with hub cluster access token
- Installs metric-collector via Helm on each member cluster
- Configures connection to hub API server and local Prometheus

With this understanding, you're ready to start the setup!

## Setup

### 1. Setup KubeFleet Clusters

First, set up the KubeFleet hub and member clusters using kind (Kubernetes in Docker):

```bash
cd /path/to/kubefleet

# Checkout main branch
git checkout main
git fetch upstream
git rebase -i upstream/main

# Set up clusters (creates 1 hub + 3 member kind clusters)
export MEMBER_CLUSTER_COUNT=3
make setup-clusters
```

This will create local kind clusters for development and testing:
- 1 hub cluster (context: `kind-hub`)
- 3 member clusters (contexts: `kind-cluster-1`, `kind-cluster-2`, `kind-cluster-3`)

**Note:** This tutorial uses kind clusters for easy local development. For production deployments, you would use real Kubernetes clusters (AKS, EKS, GKE, etc.) and adapt the installation scripts accordingly.

### 2. Register Member Clusters with Hub

Switch to hub cluster context and register the member clusters:

From the kubefleet-cookbook repo run,

```bash
cd approval-controller-metric-collector/approval-request-controller

# Switch to hub cluster
kubectl config use-context kind-hub

# Register member clusters with the hub
# This creates MemberCluster resources for kind-cluster-1, kind-cluster-2, and kind-cluster-3
# Each MemberCluster resource contains:
#   - API endpoint and credentials for the member cluster
#   - Labels for organizing clusters into stages:
#     * kind-cluster-1: environment=staging (Stage 1)
#     * kind-cluster-2: environment=prod (Stage 2)
#     * kind-cluster-3: environment=prod (Stage 2)
# These labels are used by the StagedUpdateStrategy's labelSelector to determine
# which clusters are part of each stage during the UpdateRun
kubectl apply -f ./examples/membercluster/

# Verify clusters are registered
kubectl get cluster -A
```

the output should look something like this,

```bash
NAME             JOINED   AGE   MEMBER-AGENT-LAST-SEEN   NODE-COUNT   AVAILABLE-CPU   AVAILABLE-MEMORY
kind-cluster-1   True     40s   29s                      0            0               0
kind-cluster-2   True     40s   3s                       0            0               0
kind-cluster-3   True     40s   37s                      0            0               0
```
Wait until all member clusters show as joined.

### 3. Deploy Prometheus

Create the prometheus namespace and deploy Prometheus for metrics collection:

```bash
# Create prometheus namespace
kubectl create ns prometheus

# Deploy Prometheus (ConfigMap, Deployment, Service, RBAC, and CRP)
# - ConfigMap: Contains Prometheus scrape configuration
# - Deployment: Runs Prometheus server
# - Service: Exposes Prometheus on port 9090
# - RBAC: ServiceAccount, ClusterRole, and ClusterRoleBinding for pod discovery
# - CRP: ClusterResourcePlacement to propagate Prometheus to all member clusters
kubectl apply -f ./examples/prometheus/
```

This deploys Prometheus configured to scrape pods from all namespaces with the proper annotations.

### 4. Deploy Sample Metric Application

Create the test namespace and deploy the sample application:

```bash
# Create test namespace
kubectl create ns test-ns

# Deploy sample metric app
# This creates a Deployment with a simple Go app that exposes a /metrics endpoint
# The app reports workload_health=1.0 (healthy) by default
kubectl apply -f ./examples/sample-metric-app/
```

### 5. Install Approval Request Controller (Hub Cluster)

Install the approval request controller on the hub cluster:

```bash
# Run the installation script
./install-on-hub.sh
```

The script performs the following:
1. Builds the `approval-request-controller:latest` image
2. Loads the image into the kind hub cluster
3. Verifies that required kubefleet CRDs are installed
4. Installs the controller via Helm with the custom CRDs (MetricCollector, MetricCollectorReport, ClusterStagedWorkloadTracker, StagedWorkloadTracker)
5. Verifies the installation

### 6. Configure Workload Tracker

Apply the appropriate workload tracker based on which type of staged update you'll use:

#### For Cluster-Scoped Updates (ClusterStagedUpdateRun):

```bash
# Apply ClusterStagedWorkloadTracker
# This defines which workloads to monitor for the staged rollout
# The name "example-cluster-staged-run" must match the ClusterStagedUpdateRun name
# Tracks: sample-metric-app in test-ns namespace
kubectl apply -f ./examples/workloadtracker/clusterstagedworkloadtracker.yaml
```

#### For Namespace-Scoped Updates (StagedUpdateRun):

```bash
# Apply StagedWorkloadTracker
# This defines which workloads to monitor for the namespace-scoped staged rollout
# The name "example-staged-run" and namespace "test-ns" must match the StagedUpdateRun
# Tracks: sample-metric-app in test-ns namespace
kubectl apply -f ./examples/workloadtracker/stagedworkloadtracker.yaml
```

This tells the approval controller which workloads to track.

### 7. Install Metric Collector (Member Clusters)

Install the metric collector on all member clusters:

```bash
cd ../metric-collector

# Run the installation script for all member clusters
# This builds both metric-collector and metric-app images and loads them into each cluster
./install-on-member.sh 3
```

The script performs the following for each member cluster:
1. Builds the `metric-collector:latest` image
2. Builds the `metric-app:local` image
3. Loads both images into each kind cluster
4. Creates hub token secret with proper RBAC
5. Installs the metric-collector via Helm

The `metric-app:local` image is loaded so it's available when you propagate the sample-metric-app deployment from hub to member clusters.

### 8. Create Staged Update

You can create staged updates using either cluster-scoped or namespace-scoped resources:

#### Option A: Cluster-Scoped Staged Update (ClusterStagedUpdateRun)

Switch back to hub cluster and create a cluster-scoped staged update run:

```bash
cd ../approval-request-controller

# Switch to hub cluster
kubectl config use-context kind-hub

# Apply ClusterStagedUpdateStrategy
# Defines the stages for the rollout: staging (cluster-1) -> prod (cluster-2, cluster-3)
# Each stage requires approval before proceeding
kubectl apply -f ./examples/updateRun/example-csus.yaml

# Apply ClusterResourcePlacement for sample-metric-app
# This is the resource that will be updated across stages
# Selects the sample-metric-app deployment in test-ns namespace
kubectl apply -f ./examples/updateRun/example-crp.yaml

# Verify CRP is created
kubectl get crp -A
```

Output:
```bash
NAME              GEN   SCHEDULED   SCHEDULED-GEN   AVAILABLE   AVAILABLE-GEN   AGE
example-crp       1     True        1                                           4s
prometheus-crp    1     True        1               True        1               3m1s
```

```bash
# Apply ClusterStagedUpdateRun to start the staged rollout
# This creates the actual update run that progresses through the defined stages
# Name: example-cluster-staged-run (must match ClusterStagedWorkloadTracker)
# References the ClusterResourcePlacement (example-crp) and ClusterStagedUpdateStrategy
kubectl apply -f ./examples/updateRun/example-csur.yaml

# Check the staged update run status
kubectl get csur -A
```

Output:
```bash
NAME                         PLACEMENT     RESOURCE-SNAPSHOT-INDEX   POLICY-SNAPSHOT-INDEX   INITIALIZED   PROGRESSING   SUCCEEDED   AGE
example-cluster-staged-run   example-crp   0                         0                       True          True                      5s
```

#### Option B: Namespace-Scoped Staged Update (StagedUpdateRun)

Alternatively, you can use namespace-scoped resources:

```bash
cd ../approval-request-controller

# Switch to hub cluster
kubectl config use-context kind-hub
```

``` bash
# Apply namespace-scoped ClusterResourcePlacement
# This CRP is configured to only place resources in the test-ns namespace
# This resource is needed because we cannot propagate Namespace which is a 
# cluster-scoped resource via RP
kubectl apply -f ./examples/updateRun/example-ns-only-crp.yaml

kubectl get crp -A
```

Output:
```bash
NAME              GEN   SCHEDULED   SCHEDULED-GEN   AVAILABLE   AVAILABLE-GEN   AGE
ns-only-crp       1     True        1               True        1               5s
prometheus-crp   1     True        1               True        1               2m34s
```

```bash
# Apply StagedUpdateStrategy (namespace-scoped)
# Defines the stages: staging (cluster-1) -> prod (cluster-2, cluster-3)
# Each stage requires approval before proceeding
kubectl apply -f ./examples/updateRun/example-sus.yaml

# Apply ResourcePlacement (namespace-scoped)
# This is the namespace-scoped version that works with the test-ns namespace
# References the ns-only-crp ClusterResourcePlacement
kubectl apply -f ./examples/updateRun/example-rp.yaml

# Verify RP is created
kubectl get rp -A
```

Output:
```bash
NAMESPACE   NAME         GEN   SCHEDULED   SCHEDULED-GEN   AVAILABLE   AVAILABLE-GEN   AGE
test-ns     example-rp   1     True        1                                           35s
```

```bash
# Apply StagedUpdateRun to start the staged rollout (namespace-scoped)
# This creates the actual update run that progresses through the defined stages
# Name: example-staged-run (must match StagedWorkloadTracker)
# Namespace: test-ns (must match StagedWorkloadTracker namespace)
# References the ResourcePlacement (example-rp)
kubectl apply -f ./examples/updateRun/example-sur.yaml

# Check the staged update run status
kubectl get sur -A
```

Output:
```bash
NAMESPACE   NAME                 PLACEMENT    RESOURCE-SNAPSHOT-INDEX   POLICY-SNAPSHOT-INDEX   INITIALIZED   PROGRESSING   SUCCEEDED   AGE
test-ns     example-staged-run   example-rp   0                         0                       True          True                      5s
```

### 9. Monitor the Staged Rollout

Watch the staged update progress:

#### For Cluster-Scoped Updates:

```bash
# Check the staged update run status
kubectl get csur -A

# Check approval requests (should be auto-approved based on metrics)
kubectl get clusterapprovalrequest -A
```

Output:
```bash
NAME                                       UPDATE-RUN                   STAGE     APPROVED   AGE
example-cluster-staged-run-after-staging   example-cluster-staged-run   staging   True       2m9s
```

```bash
# Check metric collector reports
kubectl get metriccollectorreport -A
```

Output:
```bash
NAMESPACE                     NAME                                    WORKLOADS   LAST-COLLECTION   AGE
fleet-member-kind-cluster-1   mc-example-cluster-staged-run-staging   1           27s               2m57s
```

#### For Namespace-Scoped Updates:

```bash
# Check the staged update run status
kubectl get sur -A

# Check approval requests (should be auto-approved based on metrics)
kubectl get approvalrequest -A
```

Output:
```bash
NAMESPACE   NAME                               UPDATE-RUN           STAGE     APPROVED   AGE
test-ns     example-staged-run-after-staging   example-staged-run   staging   True       64s
```

```bash
# Check metric collector reports
kubectl get metriccollectorreport -A
```

Output:
```bash
NAMESPACE                     NAME                            WORKLOADS   LAST-COLLECTION   AGE
fleet-member-kind-cluster-1   mc-example-staged-run-staging   1           27s               57s
```

The approval controller will automatically approve stages when the metric collectors report that workloads are healthy.

## Verification

### Check Controller Status

On the hub cluster:
```bash
kubectl config use-context kind-hub
kubectl get pods -n fleet-system
kubectl logs -n fleet-system deployment/approval-request-controller -f
```

On member clusters:
```bash
kubectl config use-context kind-cluster-1
kubectl get pods -n default
kubectl logs -n default deployment/metric-collector -f
```

### Check Metrics Collection

Verify that MetricCollector resources exist on member clusters:
```bash
kubectl config use-context kind-cluster-1
kubectl get metriccollector -A
```

Verify that MetricCollectorReports are being created on the hub:
```bash
kubectl config use-context kind-hub
kubectl get metriccollectorreport -A
```

## Configuration

### Approval Request Controller
- Located in `approval-request-controller/charts/approval-request-controller/values.yaml`
- Key settings: log level, resource limits, RBAC, CRD installation
- Default Prometheus URL: `http://prometheus.prometheus.svc.cluster.local:9090`
- Reconciliation interval: 15 seconds

### Metric Collector
- Located in `metric-collector/charts/metric-collector/values.yaml`
- Key settings: hub cluster URL, Prometheus URL, member cluster name
- Metric collection interval: 30 seconds
- Connects to hub using service account token

## Troubleshooting

### Controller not starting
- Check that all required CRDs are installed: `kubectl get crds | grep metric.kubernetes-fleet.io`
- Verify RBAC permissions are configured correctly

### Metrics not being collected
- Verify Prometheus is accessible: `kubectl port-forward -n test-ns svc/prometheus 9090:9090`
- Check metric collector logs for connection errors
- Ensure workloads have Prometheus scrape annotations

### Approvals not happening
- Check the appropriate Workload tracker object exists
- Check that the workload tracker name matches the update run name:
  - For ClusterStagedUpdateRun: ClusterStagedWorkloadTracker name must match
  - For StagedUpdateRun: StagedWorkloadTracker name and namespace must match
- Verify workload tracker resources define correct health thresholds
- Verify MetricCollectorReports are being created on the hub
- Review approval-request-controller logs for decision-making details

## Additional Resources

- [Approval Request Controller README](./approval-request-controller/README.md)
- [Metric Collector README](./metric-collector/README.md)
- [KubeFleet Documentation](https://github.com/Azure/kubefleet)
