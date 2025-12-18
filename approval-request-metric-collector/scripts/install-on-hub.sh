#!/bin/bash
set -e

# Detect script directory to support execution from multiple locations
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Usage: ./install-on-hub.sh <registry> <hub-cluster>
# Example: ./install-on-hub.sh arvindtestacr.azurecr.io kind-hub

if [ "$#" -lt 2 ]; then
    echo "Usage: $0 <registry> <hub-cluster>"
    echo "Example: $0 arvindtestacr.azurecr.io kind-hub"
    echo ""
    echo "Parameters:"
    echo "  registry    - ACR registry URL (e.g., arvindtestacr.azurecr.io)"
    echo "  hub-cluster - Hub cluster name (e.g., kind-hub)"
    exit 1
fi

# Configuration
REGISTRY="$1"
HUB_CLUSTER="$2"
IMAGE_NAME="approval-request-controller"
IMAGE_TAG="${IMAGE_TAG:-latest}"
NAMESPACE="fleet-system"
CHART_NAME="approval-request-controller"

# Get hub cluster context using kubectl config view (following kubefleet pattern)
HUB_CONTEXT=$(kubectl config view -o jsonpath="{.contexts[?(@.context.cluster==\"$HUB_CLUSTER\")].name}")

if [ -z "$HUB_CONTEXT" ]; then
    echo "Error: Could not find context for hub cluster '$HUB_CLUSTER'"
    echo "Available clusters:"
    kubectl config view -o jsonpath='{.clusters[*].name}' | tr ' ' '\n'
    exit 1
fi

# Construct full image repository path
IMAGE_REPOSITORY="${REGISTRY}/${IMAGE_NAME}"

echo "=== Installing ApprovalRequest Controller on hub cluster ==="
echo "Registry: ${REGISTRY}"
echo "Image: ${IMAGE_REPOSITORY}:${IMAGE_TAG}"
echo "Hub cluster: ${HUB_CLUSTER}"
echo "Hub context: ${HUB_CONTEXT}"
echo "Namespace: ${NAMESPACE}"
echo ""

echo ""

# Step 1: Verify kubefleet CRDs are installed
echo "Step 1: Verifying required kubefleet CRDs..."
REQUIRED_CRDS=(
  "approvalrequests.placement.kubernetes-fleet.io"
  "clusterapprovalrequests.placement.kubernetes-fleet.io"
  "clusterresourceplacements.placement.kubernetes-fleet.io"
  "clusterresourceoverrides.placement.kubernetes-fleet.io"
  "clusterstagedupdateruns.placement.kubernetes-fleet.io"
  "stagedupdateruns.placement.kubernetes-fleet.io"
)

MISSING_CRDS=()
for crd in "${REQUIRED_CRDS[@]}"; do
  if ! kubectl --context=${HUB_CONTEXT} get crd ${crd} &>/dev/null; then
    MISSING_CRDS+=("${crd}")
  fi
done

if [ ${#MISSING_CRDS[@]} -ne 0 ]; then
  echo "Error: Missing required CRDs from kubefleet hub-agent:"
  for crd in "${MISSING_CRDS[@]}"; do
    echo "  - ${crd}"
  done
  echo ""
  echo "Please ensure kubefleet hub-agent is installed first."
  exit 1
fi

echo "✓ All required kubefleet CRDs are installed"
echo ""

# Step 2: Install helm chart on hub cluster (includes MetricCollector, MetricCollectorReport, WorkloadTracker CRDs)
echo "Step 2: Installing helm chart on hub cluster..."
helm upgrade --install ${CHART_NAME} ${REPO_ROOT}/charts/${CHART_NAME} \
  --kube-context=${HUB_CONTEXT} \
  --namespace ${NAMESPACE} \
  --set image.repository=${IMAGE_REPOSITORY} \
  --set image.tag=${IMAGE_TAG} \
  --set image.pullPolicy=Always \
  --set controller.logLevel=2

echo "✓ Helm chart installed on hub cluster"
echo ""

# Step 3: Verify installation
echo "Step 3: Verifying installation..."
echo "Checking CRDs installed by this chart..."
kubectl --context=${HUB_CONTEXT} get crd | grep -E "metriccollectors|metriccollectorreports|workloadtrackers" || echo "  (CRDs may take a moment to appear)"

echo ""
echo "Checking pods in ${NAMESPACE}..."
kubectl --context=${HUB_CONTEXT} get pods -n ${NAMESPACE} -l app.kubernetes.io/name=${CHART_NAME}

echo ""
echo "=== Installation Complete ==="
echo ""
echo "To check controller logs:"
echo "  kubectl --context=${HUB_CONTEXT} logs -n ${NAMESPACE} -l app.kubernetes.io/name=${CHART_NAME} -f"
echo ""
echo "To verify CRDs:"
echo "  kubectl --context=${HUB_CONTEXT} get crd | grep autoapprove.kubernetes-fleet.io"
echo ""
echo "Next steps:"
echo "  1. Create a WorkloadTracker to define which workloads to monitor"
echo "  2. ApprovalRequests will be automatically processed when created by staged updates"
echo ""
