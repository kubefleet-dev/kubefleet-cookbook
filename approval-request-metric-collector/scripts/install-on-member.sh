#!/bin/bash
set -e

# Detect script directory to support execution from multiple locations
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Usage: ./install-on-member.sh <registry> <hub-cluster> <member-cluster-1> [member-cluster-2] [member-cluster-3] ...
# Example: ./install-on-member.sh arvindtestacr.azurecr.io kind-hub kind-cluster-1 kind-cluster-2 kind-cluster-3

if [ "$#" -lt 3 ]; then
    echo "Usage: $0 <registry> <hub-cluster> <member-cluster-1> [member-cluster-2] ..."
    echo "Example: $0 arvindtestacr.azurecr.io kind-hub kind-cluster-1 kind-cluster-2 kind-cluster-3"
    echo ""
    echo "Parameters:"
    echo "  registry         - ACR registry URL (e.g., arvindtestacr.azurecr.io)"
    echo "  hub-cluster      - Hub cluster name (e.g., kind-hub)"
    echo "  member-clusters  - One or more member cluster names"
    exit 1
fi

# Configuration
REGISTRY="$1"
HUB_CLUSTER="$2"
MEMBER_CLUSTERS=("${@:3}")
MEMBER_NAMESPACE="default"
PROMETHEUS_URL="http://prometheus.test-ns:9090"
IMAGE_TAG="${IMAGE_TAG:-latest}"
METRIC_COLLECTOR_IMAGE="metric-collector"
METRIC_APP_IMAGE="metric-app"

# Get hub cluster context and API server URL using kubectl config view (following kubefleet pattern)
HUB_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$HUB_CLUSTER\")].name}")
HUB_API_SERVER=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"$HUB_CLUSTER\")].cluster.server}")

if [ -z "$HUB_CONTEXT" ]; then
    echo "Error: Could not find context for hub cluster '$HUB_CLUSTER'"
    echo "Available clusters:"
    kubectl config view -o jsonpath='{.clusters[*].name}' | tr ' ' '\n'
    exit 1
fi

if [ -z "$HUB_API_SERVER" ]; then
    echo "Error: Could not find API server URL for hub cluster '$HUB_CLUSTER'"
    exit 1
fi

# Construct full image repository paths
METRIC_COLLECTOR_REPOSITORY="${REGISTRY}/${METRIC_COLLECTOR_IMAGE}"
METRIC_APP_REPOSITORY="${REGISTRY}/${METRIC_APP_IMAGE}"

echo "=== Installing MetricCollector on ${#MEMBER_CLUSTERS[@]} member cluster(s) ==="
echo "Registry: ${REGISTRY}"
echo "Metric Collector Image: ${METRIC_COLLECTOR_REPOSITORY}:${IMAGE_TAG}"
echo "Metric App Image: ${METRIC_APP_REPOSITORY}:${IMAGE_TAG}"
echo "Hub cluster: ${HUB_CLUSTER}"
echo "Hub context: ${HUB_CONTEXT}"
echo "Hub API server: ${HUB_API_SERVER}"
echo "Member clusters: ${MEMBER_CLUSTERS[@]}"
echo ""

echo ""

# Install on each member cluster
CLUSTER_INDEX=0
for MEMBER_CLUSTER in "${MEMBER_CLUSTERS[@]}"; do
  CLUSTER_INDEX=$((CLUSTER_INDEX + 1))
  
  MEMBER_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$MEMBER_CLUSTER\")].name}")
  MEMBER_CLUSTER_NAME="${MEMBER_CLUSTER}"
  HUB_NAMESPACE="fleet-member-${MEMBER_CLUSTER_NAME}"

  if [ -z "$MEMBER_CONTEXT" ]; then
      echo "Error: Could not find context for member cluster '$MEMBER_CLUSTER'"
      echo "Available clusters:"
      kubectl config view -o jsonpath='{.clusters[*].name}' | tr ' ' '\n'
      exit 1
  fi

  echo "========================================"
  echo "Installing on Member Cluster ${CLUSTER_INDEX}/${#MEMBER_CLUSTERS[@]}"
  echo "  Cluster: ${MEMBER_CLUSTER}"
  echo "  Context: ${MEMBER_CONTEXT}"
  echo "  Cluster Name: ${MEMBER_CLUSTER_NAME}"
  echo "========================================"
  echo ""

  # Step 1: Setup RBAC on hub cluster
  echo "Step 1: Setting up RBAC on hub cluster..."
  
  # Verify namespace exists (should be created by KubeFleet when member cluster joins)
  if ! kubectl --context=${HUB_CONTEXT} get namespace ${HUB_NAMESPACE} &>/dev/null; then
      echo "Error: Namespace ${HUB_NAMESPACE} does not exist on hub cluster"
      echo "This namespace should be automatically created by KubeFleet when the member cluster joins the hub"
      echo "Please ensure the member cluster is properly registered with the hub"
      exit 1
  fi

  cat <<EOF | kubectl --context=${HUB_CONTEXT} apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: metric-collector-sa
  namespace: ${HUB_NAMESPACE}
---
# Role for MetricCollectorReport access in fleet-member namespace
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: metric-collector-report-role
  namespace: ${HUB_NAMESPACE}
rules:
- apiGroups: ["autoapprove.kubernetes-fleet.io"]
  resources: ["metriccollectorreports"]
  verbs: ["get", "list", "watch", "update", "patch"]
- apiGroups: ["autoapprove.kubernetes-fleet.io"]
  resources: ["metriccollectorreports/status"]
  verbs: ["update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: metric-collector-report-rolebinding
  namespace: ${HUB_NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: metric-collector-report-role
subjects:
- kind: ServiceAccount
  name: metric-collector-sa
  namespace: ${HUB_NAMESPACE}
---
# ClusterRole for reading ClusterStagedWorkloadTracker (cluster-scoped)
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: metric-collector-workloadtracker-reader-${MEMBER_CLUSTER_NAME}
rules:
- apiGroups: ["placement.kubernetes-fleet.io"]
  resources: ["clusterstagedworkloadtrackers"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: metric-collector-workloadtracker-${MEMBER_CLUSTER_NAME}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: metric-collector-workloadtracker-reader-${MEMBER_CLUSTER_NAME}
subjects:
- kind: ServiceAccount
  name: metric-collector-sa
  namespace: ${HUB_NAMESPACE}
EOF

  echo "✓ RBAC configured on hub cluster"
  echo ""

  # Step 2: Create token secret on hub cluster
  echo "Step 2: Creating token secret on hub cluster..."
  cat <<EOF | kubectl --context=${HUB_CONTEXT} apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: metric-collector-token
  namespace: ${HUB_NAMESPACE}
  annotations:
    kubernetes.io/service-account.name: metric-collector-sa
type: kubernetes.io/service-account-token
EOF

  # Wait for token to be created
  echo "Waiting for token to be created..."
  sleep 3

  # Get token
  TOKEN=$(kubectl --context=${HUB_CONTEXT} get secret metric-collector-token -n ${HUB_NAMESPACE} -o jsonpath='{.data.token}' | base64 -d)
  if [ -z "$TOKEN" ]; then
      echo "Error: Failed to get token from hub cluster for ${MEMBER_CLUSTER_NAME}"
      exit 1
  fi

  echo "✓ Token created on hub cluster"
  echo ""

  # Step 3: Create namespace and secrets on member cluster
  echo "Step 3: Creating secrets on member cluster..."

  kubectl --context=${MEMBER_CONTEXT} create secret generic hub-token \
    --from-literal=token="${TOKEN}" \
    -n ${MEMBER_NAMESPACE} \
    --dry-run=client -o yaml | kubectl --context=${MEMBER_CONTEXT} apply -f -

  echo "✓ Secrets created on member cluster"
  echo ""

  # Step 4: Install helm chart on member cluster (includes CRD)
  echo "Step 4: Installing helm chart on member cluster..."
  helm upgrade --install metric-collector ${REPO_ROOT}/charts/metric-collector \
    --kube-context=${MEMBER_CONTEXT} \
    --namespace ${MEMBER_NAMESPACE} \
    --set memberCluster.name=${MEMBER_CLUSTER_NAME} \
    --set hubCluster.url=${HUB_API_SERVER} \
    --set hubCluster.tls.insecure=true \
    --set prometheus.url=${PROMETHEUS_URL} \
    --set image.repository=${METRIC_COLLECTOR_REPOSITORY} \
    --set image.tag=${IMAGE_TAG} \
    --set image.pullPolicy=Always \
    --set metricApp.image.repository=${METRIC_APP_REPOSITORY} \
    --set metricApp.image.tag=${IMAGE_TAG}

  echo "✓ Helm chart installed on member cluster"
  echo ""

  # Step 5: Verify installation
  echo "Step 5: Verifying installation..."
  echo "Checking pods on member cluster..."
  kubectl --context=${MEMBER_CONTEXT} get pods -n ${MEMBER_NAMESPACE} -l app.kubernetes.io/name=metric-collector

  echo ""
  echo "✓ Installation complete for ${MEMBER_CLUSTER_NAME}"
  echo ""
done

echo "========================================"
echo "=== All Installations Complete ==="
echo "========================================"
echo ""
echo "To check logs from a specific member cluster:"
echo "  kubectl --context=${MEMBER_CLUSTERS[0]} logs -n ${MEMBER_NAMESPACE} -l app.kubernetes.io/name=metric-collector -f"
echo ""
echo "To check MetricCollectorReports on hub:"
echo "  kubectl --context=${HUB_CONTEXT} get metriccollectorreports -A"
echo ""
