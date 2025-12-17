# Approval Controller and Metric Collector Tutorial

This tutorial demonstrates how to use the Approval Request Controller and Metric Collector with KubeFleet for automated staged rollout approvals based on workload health metrics.

> **Note:** This tutorial is self-contained and provides all the steps needed to get started. For additional context on KubeFleet's staged update functionality, you can optionally refer to the [Staged Update How-To Guide](https://github.com/Azure/fleet/blob/main/docs/howtos/staged-update.md).

## Overview

This directory contains two controllers:
- **approval-request-controller**: Runs on the hub cluster to automate approval decisions for staged updates
- **metric-collector**: Runs on member clusters to collect and report workload health metrics

![Approval Controller and Metric Collector Architecture](./images/approval-request-metric-collector.png)

## How It Works

### Custom Resource Definitions (CRDs)

This solution introduces three new CRDs that work together with KubeFleet's native resources:

#### Hub Cluster CRDs

1. **MetricCollectorReport** (namespaced)
   - Created by approval-request-controller in `fleet-member-<cluster-name>` namespaces on hub
   - Watched and updated by metric-collector running on member clusters
   - Contains specification of Prometheus URL and collected `workload_health` metrics
   - Updated every 30 seconds by the metric collector with latest health data

2. **ClusterStagedWorkloadTracker** (cluster-scoped)
   - Defines which workloads to monitor for a ClusterStagedUpdateRun
   - The name must match the ClusterStagedUpdateRun name
   - Specifies workload's name, namespace, and kind (e.g., Deployment, StatefulSet)
   - Used by approval-request-controller to determine if stage is ready for approval

3. **StagedWorkloadTracker** (namespaced)
   - Defines which workloads to monitor for a StagedUpdateRun
   - The name and namespace must match the StagedUpdateRun name and namespace
   - Specifies namespace, workload name, and kind
   - Used by approval-request-controller to determine if stage is ready for approval

### Automated Approval Flow

1. **Stage Initialization**
   - User creates an UpdateRun (`ClusterStagedUpdateRun` or `StagedUpdateRun`) on the hub
   - KubeFleet creates an ApprovalRequest (`ClusterApprovalRequest` or `ApprovalRequest`) for the first stage
   - The ApprovalRequest enters "Pending" state, waiting for approval

2. **Metric Collector Report Creation**
   - Approval-request-controller watches the `ClusterApprovalRequest` and `ApprovalRequest` objects
   - For each cluster in the current stage:
     - Creates a `MetricCollectorReport` in `fleet-member-<cluster-name>` namespace on hub
     - Sets `spec.prometheusUrl` to the Prometheus endpoint
     - Each report is specific to one cluster

3. **Metric Collection on Member Clusters**
   - Metric-collector controller runs on each member cluster
   - Watches for `MetricCollectorReport` in its `fleet-member-<cluster-name>` namespace on hub
   - Every 30 seconds, it:
     - Queries local Prometheus using URL from report spec with PromQL: `workload_health`
     - Prometheus returns metrics for all pods with `prometheus.io/scrape: "true"` annotation
     - Extracts workload health (1.0 = healthy, 0.0 = unhealthy) along with metadata labels
     - Updates the `MetricCollectorReport` status on hub with **all** collected metrics
   
   **Example Prometheus Metric:**
   ```
   workload_health{app="sample-metric-app", instance="10.244.0.32:8080", job="kubernetes-pods", namespace="test-ns", pod="sample-metric-app-565fd6595b-7pfb6", pod_template_hash="565fd6595b", workload_kind="Deployment"} 1
   ```

   **Important Note on Multiple Pods:** When a workload (e.g., a Deployment) has multiple pods/replicas emitting health signals:
   - The metric collector **collects all metrics** from Prometheus and stores them in the MetricCollectorReport
   - If `sample-metric-app` has 3 replicas, the report will contain 3 separate `WorkloadMetrics` entries
   - However, for simplicity, the approval-request-controller only evaluates the **first matching metric** when checking workload health
   - This means if the first pod reports healthy, the workload is considered healthy, even if other pods report differently
   - This simplified approach works well when all pods of a workload consistently report the same health status
   - **Limitation:** If pods have different health states, only the first metric encountered is used for approval decisions
   
   **Customizing Health Aggregation Logic:**
   To implement more sophisticated health checks (e.g., all pods must be healthy, or majority healthy):
   1. Edit `pkg/controllers/approvalrequest/controller.go` in the approval-request-controller
   2. Locate the health check loop (search for "Simplified health check using first matching metric")
   3. Remove the `break` statement that stops at the first match
   4. Collect all matching metrics for the workload into a slice
   5. Implement your aggregation logic:
      - **All healthy:** Check that every metric has `Health == true`
      - **Majority healthy:** Count healthy metrics and compare to total
      - **Threshold-based:** Require N out of M pods to be healthy
   6. Rebuild and redeploy the approval-request-controller image

4. **Health Evaluation**
   - Approval-request-controller monitors `MetricCollectorReports` from all stage clusters
   - Every 15 seconds, it:
     - Fetches the appropriate workload tracker:
       - For cluster-scoped: `ClusterStagedWorkloadTracker` with same name as ClusterStagedUpdateRun
       - For namespace-scoped: `StagedWorkloadTracker` with same name and namespace as StagedUpdateRun
     - For each cluster in the stage:
       - Reads its `MetricCollectorReport` status from `fleet-member-<cluster-name>` namespace
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

- Docker for building images
- Azure CLI (`az`) for ACR operations
- kubectl configured with access to your clusters
- Helm 3.x
- KubeFleet installed on hub and member clusters
- Azure Container Registry (ACR) with anonymous pull enabled

## Building and Pushing Images to ACR

Before installing the controllers, you need to build the Docker images and push them to Azure Container Registry (ACR).

**Critical Note:** Enable anonymous pull on the ACR so that clusters can pull images without authentication. Ensure to disable anonymous pull or delete the ACR after testing.

### 1. Create ACR with Anonymous Pull

Create a resource group and ACR with Standard SKU (Basic SKU doesn't support anonymous pull):

```bash
# Create resource group
az group create --name test-kubefleet-rg --location eastus

# Create container registry with Standard SKU
az acr create --resource-group test-kubefleet-rg --name myfleetacr --sku Standard

# Login to ACR
az acr login --name myfleetacr

# Enable anonymous pull
az acr update --name myfleetacr --anonymous-pull-enabled
```

From the `az acr create` output, note down the login server (e.g., `myfleetacr.azurecr.io`).

> Note: Users can also create their own registry to push their docker images, it doesn't have to be ACR.

### 2. Build and Push Images

Export registry and tag variables:

```bash
export REGISTRY="myfleetacr.azurecr.io"
export TAG="latest"

cd approval-request-metric-collector
```

Build and push all images at once, to build for a specific architecture (default is your system's architecture):

```bash
# For AMD64 (x86_64), ARCH used by AKS fleet, clusters.
make docker-build-all GOARCH=amd64

# For ARM64 (Apple Silicon, ARM servers)
make docker-build-all GOARCH=arm64
```

Or build individual images:

```bash
# Build and push approval-request-controller image
make docker-build-approval-controller

# Build and push metric-collector image
make docker-build-metric-collector

# Build and push metric-app image
make docker-build-metric-app
```

### 3. Verify Images in ACR

List images in your ACR:

```bash
az acr repository list --name myfleetacr --output table
```

Expected output:
```
Result
---------------------------
approval-request-controller
metric-app
metric-collector
```

Verify tags for a specific image:

```bash
az acr repository show-tags --name myfleetacr --repository approval-request-controller --output table
```

Expected output:
```
Result
--------
latest
```

**You're now ready to proceed with the setup!** Your ACR contains all three required images that will be pulled by your clusters.

### 4. Cleanup (After Testing)

When you're done testing, delete the resource group to clean up all resources:

```bash
az group delete --name test-kubefleet-rg
```

## Setup Overview

Before diving into the setup steps, here's a bird's eye view of what you'll be building:

### Architecture Components

**Hub Cluster** - The control plane where you'll deploy:
1. **3 Member Clusters** (cluster-1, cluster-2, cluster-3)
   - Labeled with `environment=staging` or `environment=prod`
   - These labels determine which stage each cluster belongs to during rollouts

2. **Prometheus** (propagated to all clusters)
   - Monitors workload health via `/metrics` endpoints
   - Scrapes pods with `prometheus.io/scrape: "true"` annotation
   - Provides `workload_health` metric (1.0 = healthy, 0.0 = unhealthy)

3. **Approval Request Controller**
   - Watches `ClusterApprovalRequest` and `ApprovalRequest` objects
   - Creates MetricCollectorReport directly in `fleet-member-<cluster-name>` namespaces
   - Evaluates workload health from MetricCollectorReport status
   - Auto-approves stages when all workloads are healthy

4. **Sample Metric App** (will be rolled out to clusters)
   - Simple Go application exposing `/metrics` endpoint
   - Reports `workload_health=1.0` by default
   - Used to demonstrate health-based approvals

**Member Clusters** - Where workloads run:
1. **Metric Collector**
   - Connects to hub cluster to watch MetricCollectorReport in its namespace
   - Queries local Prometheus every 30 seconds using URL from MetricCollectorReport spec
   - Updates MetricCollectorReport status on hub with collected health metrics

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
   - Contains a list of workloads (name, namespace, and kind) to monitor across all clusters in each stage

2. **StagedWorkloadTracker** (for StagedUpdateRun)
   - Namespace-scoped resource on the hub
   - Name and namespace must exactly match the StagedUpdateRun
   - Example: If your UpdateRun is `example-staged-run` in namespace `test-ns`, the tracker must be `example-staged-run` in `test-ns`
   - Contains a list of workloads to monitor

**How It Works:**
```yaml
# ClusterStagedWorkloadTracker example
workloads:
  - name: sample-metric-app    # Workload name (matches the app label)
    namespace: test-ns         # Namespace where it runs
    kind: Deployment           # Workload kind (optional, enables precise matching)
```

When the approval controller evaluates a stage:
1. It fetches the WorkloadTracker that matches the UpdateRun name (and namespace)
2. For each cluster in the stage, it reads the MetricCollectorReport
3. It verifies that every workload listed in the tracker appears in the report as healthy
4. The matching logic compares namespace, name, and kind (if specified) in a case-insensitive manner
5. Only when ALL workloads in ALL clusters are healthy does it approve the stage

**Critical Rule:** The WorkloadTracker must be created BEFORE starting the UpdateRun. If the controller can't find a matching tracker, it won't approve any stages.

### The Staged Rollout Flow

When you create a **ClusterStagedUpdateRun** or **StagedUpdateRun**, here's what happens:

1. **Stage 1 (staging)**: Rollout starts with `cluster-1`
   - KubeFleet creates an ApprovalRequest for the staging stage
   - Approval controller creates MetricCollectorReport in `fleet-member-cluster-1` namespace
   - Metric collector on `cluster-1` watches its report on hub and updates status with health metrics
   - When `sample-metric-app` is healthy, approval controller auto-approves
   - KubeFleet proceeds with the rollout to `cluster-1`

2. **Stage 2 (prod)**: After staging succeeds
   - KubeFleet creates an ApprovalRequest for the prod stage
   - Approval controller creates MetricCollectorReports in `fleet-member-cluster-2` and `fleet-member-cluster-3`
   - Metric collectors on both clusters watch their reports and update with health data
   - When ALL workloads across BOTH prod clusters are healthy, auto-approve
   - KubeFleet completes the rollout to production clusters

### Key Resources You'll Create

| Resource | Purpose | Where |
|----------|---------|-------|
| **ClusterResourcePlacement** | Define what resources to propagate (Prometheus, sample-app) | Hub |
| **StagedUpdateStrategy** | Define stages with label selectors and approval requirements | Hub |
| **WorkloadTracker** | Specify which workloads to monitor for health | Hub |
| **UpdateRun** | Start the staged rollout process | Hub |
| **MetricCollectorReport** | Created by approval controller, updated by metric collector | Hub (fleet-member-* ns) |

## Setup

### Prerequisites

Before starting this tutorial, ensure you have:
- A KubeFleet hub cluster with fleet controllers installed
- Three member clusters joined to the hub cluster
- kubectl configured with access to the hub cluster context

This can be achieved through a number of ways,
- https://kubefleet.dev/docs/getting-started/
- https://learn.microsoft.com/en-us/azure/kubernetes-fleet/quickstart-create-fleet-and-members-portal

### 1. Label Member Clusters for Staged Rollout

The staged rollout uses labels to determine which clusters belong to each stage. Ensure your member clusters have the following labels:

**Stage 1 (staging)** - One cluster:
- `environment=staging`

**Stage 2 (prod)** - Two or more clusters:
- `environment=prod`

Expected cluster configuration:
```
cluster-1: environment=staging
cluster-2: environment=prod
cluster-3: environment=prod
```

The `StagedUpdateStrategy` uses these labels to select clusters for each stage:
- **Stage 1 (staging)**: Selects clusters with `environment=staging`
- **Stage 2 (prod)**: Selects clusters with `environment=prod`

Note: If you are updating fleet member cluster CRs joined via Azure portal, CLI please use the following command, this is because we don't allow users to use kubectl to update labels directly a validating webhook configuration will deny any user,
```
az fleet member update -g <resourceGroupName> -f <fleetName> -n <memberClusterName> --labels "<labelKey>=<labelValue>"
```

### 2. Deploy Prometheus

From the kubefleet-cookbook repo, navigate to the approval-request-metric-collector directory and deploy Prometheus for metrics collection:

```bash
cd approval-request-metric-collector

# Switch to hub cluster context
kubectl config use-context <hub-context>

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

### 3. Deploy Sample Metric Application

Create the test namespace and deploy the sample application:

```bash
# Create test namespace
kubectl create ns test-ns

# Create sample-metric-app deployment
kubectl apply -f ./examples/sample-metric-app/
```

> Note: If users are using a different REGISTRY, TAG variables from the setup, please update examples/sample-metric-app/sample-metric-app.yaml accordingly.

**Important: Configuring WORKLOAD_KIND Environment Variable**

The sample-metric-app emits a `workload_health` metric with a `workload_kind` label that identifies the parent workload type. This label **must match** the `kind` field specified in your WorkloadTracker.

The sample deployment sets `WORKLOAD_KIND=Deployment`:
```yaml
env:
- name: WORKLOAD_KIND
  value: "Deployment"
```

For other workload types, update the environment variable accordingly:
- **StatefulSet**: `WORKLOAD_KIND=StatefulSet`
- **DaemonSet**: `WORKLOAD_KIND=DaemonSet`
- **Job**: `WORKLOAD_KIND=Job`

This is necessary because Prometheus's `__meta_kubernetes_pod_controller_kind` returns the immediate controller (e.g., ReplicaSet for Deployments), not the actual parent resource. By setting this environment variable, the metric app emits the correct workload type that matches your WorkloadTracker configuration.

### 4. Install Approval Request Controller (Hub Cluster)

Install the approval request controller on the hub cluster using the ACR registry:

```bash
# Set your ACR registry name
export REGISTRY="myfleetacr.azurecr.io"

# Run the installation script
scripts/install-on-hub.sh ${REGISTRY} <HUB_CONTEXT>
```

The script performs the following:
1. Configures the controller to use the approval-request-controller image from your ACR
2. Verifies that required KubeFleet CRDs are installed
3. Installs the controller via Helm with the custom CRDs (MetricCollectorReport, ClusterStagedWorkloadTracker, StagedWorkloadTracker)
4. Verifies the installation

### 5. Configure Workload Tracker

Apply the appropriate workload tracker based on which type of staged update you'll use:

#### For Cluster-Scoped Updates (ClusterStagedUpdateRun):

```bash
# Apply ClusterStagedWorkloadTracker
# This defines which workloads to monitor for the staged rollout
# The name "example-cluster-staged-run" must match the ClusterStagedUpdateRun name
# Tracks: sample-metric-app Deployment in test-ns namespace
kubectl apply -f ./examples/workloadtracker/clusterstagedworkloadtracker.yaml
```

#### For Namespace-Scoped Updates (StagedUpdateRun):

```bash
# Apply StagedWorkloadTracker
# This defines which workloads to monitor for the namespace-scoped staged rollout
# The name "example-staged-run" and namespace "test-ns" must match the StagedUpdateRun
# Tracks: sample-metric-app in test-ns namespace with kind Deployment
kubectl apply -f ./examples/workloadtracker/stagedworkloadtracker.yaml
```

### 6. Install Metric Collector (Member Clusters)

Install the metric collector on all member clusters using the ACR registry:

```bash
# Find the contexts for hub, member clusters.
kubectl config get-contexts
```

```bash
# Run the installation script for all member clusters
# Replace <hub-context> <cluster-1-context> <cluster-2-context> <cluster-3-context> with your actual cluster contexts
scripts/install-on-member.sh ${REGISTRY} <hub-context> <cluster-1-context> <cluster-2-context> <cluster-3-context>

# Example:
# scripts/install-on-member.sh ${REGISTRY} hub cluster-1 cluster-2 cluster-3
```

The script performs the following:
1. Configures the metric-collector to use the image from your ACR
2. Creates service account with hub cluster access token
3. Installs metric-collector via Helm on each member cluster
4. Configures connection to hub API server and local Prometheus

### 7. Start Staged Rollout

Choose one of the following options based on your use case:

#### Option A: Cluster-Scoped Staged Update (ClusterStagedUpdateRun)

Create a cluster-scoped staged update run:

Switch back to hub cluster and create a cluster-scoped staged update run:

```bash
# Switch to hub cluster
kubectl config use-context <hub-context>

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

#### Option B: Namespace-Scoped Staged Update (StagedUpdateRun)

Alternatively, you can use namespace-scoped resources:

``` bash
# Switch to hub cluster
kubectl config use-context <hub-context>

# Apply namespace-scoped ClusterResourcePlacement
# This CRP is configured to only place resources in the test-ns namespace
# This resource is needed because we cannot propagate Namespace which is a 
# cluster-scoped resource via RP
kubectl apply -f ./examples/updateRun/example-ns-only-crp.yaml

kubectl get crp -A
```

Output:
```bash
NAME             GEN   SCHEDULED   SCHEDULED-GEN   AVAILABLE   AVAILABLE-GEN   AGE
ns-only-crp      1     True        1               True        1               4s
prometheus-crp   1     True        1               True        1               31m
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

### 8. Monitor the Staged Rollout

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
NAMESPACE                 NAME                                    WORKLOADS   LAST-COLLECTION   AGE
fleet-member-cluster-1    mc-example-cluster-staged-run-staging   1           27s               2m57s
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
NAMESPACE              NAME                          WORKLOADS   LAST-COLLECTION   AGE
fleet-member-cluster-1 mc-example-staged-run-staging 1           27s               57s
```

The approval controller will automatically approve stages when the metric collectors report that workloads are healthy.

## Verification

### Check Controller Status

On the hub cluster:
```bash
kubectl config use-context <hub-context>
kubectl get pods -n fleet-system
kubectl logs -n fleet-system deployment/approval-request-controller -f
```

On member clusters:
```bash
kubectl config use-context <member-cluster-context>
kubectl get pods -n default
kubectl logs -n default deployment/metric-collector -f
```

### Check Metrics Collection

Verify that MetricCollectorReports are being created and updated on the hub:
```bash
kubectl config use-context <hub-context>
kubectl get metriccollectorreport -A
```

## Configuration

### Approval Request Controller
- Located in `charts/approval-request-controller/values.yaml`
- Key settings: log level, resource limits, RBAC, CRD installation
- Default Prometheus URL: `http://prometheus.prometheus.svc.cluster.local:9090`
- Reconciliation interval: 15 seconds

### Metric Collector
- Located in `charts/metric-collector/values.yaml`
- Key settings: hub cluster URL, Prometheus URL, member cluster name
- Metric collection interval: 30 seconds
- Connects to hub using service account token

## Troubleshooting

### Controller not starting
- Check that all required CRDs are installed: `kubectl get crds | grep autoapprove.kubernetes-fleet.io`
- Verify RBAC permissions are configured correctly

### Metrics not being collected
- Verify Prometheus is accessible: `kubectl port-forward -n prometheus svc/prometheus 9090:9090`
- Check metric collector logs for connection errors
- Ensure workloads have Prometheus scrape annotations

### Approvals not happening
- Check the appropriate Workload tracker object exists
- Check that the workload tracker name matches the update run name:
  - For ClusterStagedUpdateRun: ClusterStagedWorkloadTracker name must match
  - For StagedUpdateRun: StagedWorkloadTracker name and namespace must match
- Verify workloads in the tracker match those reporting metrics (name, namespace, and kind)
- Verify MetricCollectorReports are being created on the hub
- Review approval-request-controller logs for decision-making details

## Additional Resources

- [Approval Request Controller README](./approval-request-controller/README.md)
- [Metric Collector README](./metric-collector/README.md)
- [KubeFleet Documentation](https://github.com/Azure/kubefleet)
