function install_crds() {
    echo "Installing required CRDs for resource placement..."
    kubectl config use-context $FLEET_HUB_CTX

    echo "Adding the KAITO workspace CRD..."
    kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/tags/v0.7.1/charts/kaito/workspace/crds/kaito.sh_workspaces.yaml

    echo "Adding Kubernetes Gateway API CRDs..."
    kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.3.0/standard-install.yaml

    echo "Adding Kubernetes Gateway API Inference Extension CRDs..."
    kubectl apply -f https://github.com/kubernetes-sigs/gateway-api-inference-extension/releases/latest/download/manifests.yaml
    # Delete the v1alpha1 Gateway Inference Extension CRD to avoid conflicts.
    kubectl delete customresourcedefinition.apiextensions.k8s.io/inferencepools.inference.networking.x-k8s.io --ignore-not-found

    echo "Adding the Istio DestinationRule CRD..."
    kubectl apply -f https://gist.githubusercontent.com/michaelawyu/b93fec3b8eadc032a14bd52193080380/raw/9336c4c7bb0c5a73864ace6a73b64bc5ef9b9bff/istio-dr-crd.yaml
}

function label_member_clusters() {
    echo "Labeling member clusters for resource placement..."
    kubectl config use-context $FLEET_HUB_CTX
    kubectl label membercluster $MEMBER_1 env=prod
    kubectl label membercluster $MEMBER_2 env=staging
}

function place_kaito_workspaces() {
    echo "Placing Kaito workspaces on member cluster $1..."
    kubectl config use-context $FLEET_HUB_CTX

    echo "Adding the workspace to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: $1
  namespace: default
inference:
  preset:
    accessMode: public
    name: $2
    presetOptions: {}
resource:
  count: 1
  instanceType: $GPU_VM_SIZE
  labelSelector:
    matchLabels:
      apps: $2
EOF

    echo "Adding the ResourcePlacement API object to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ResourcePlacement
metadata:
  name: kaito-workspace-deepseek
  namespace: default
spec:
  resourceSelectors:
    - group: kaito.sh
      kind: Workspace
      name: $1
      version: v1beta1
  policy:
    placementType: PickN
    numberOfClusters: 1
    dynamicResourceClaims:
    - resourceName: kubernetes.azure.com/nodes/additional-capacity
      dynamicResourceClassName: azure-additional-capacity
      matchAttributes:
        node.kubernetes.io/instance-type: $GPU_VM_SIZE
      count: 1
      controllerName: azure-dynamic-resource-provider
      weight: 20
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 100%
      unavailablePeriodSeconds: 1
    applyStrategy:
      whenToTakeOver: IfNoDiff
      whenToApply: IfNotDrifted
      allowCoOwnership: true
    reportBackStrategy:
      type: Mirror
      destination: OriginalResource
EOF
}

function place_inf_pool_epp_via_kubefleet() {
    echo "Placing inference pools + EPPs on member cluster $3..."
    kubectl config use-context $FLEET_HUB_CTX

    echo "Installing related resources on the KubeFleet hub cluster..."
    helm install $1 \
        --set inferencePool.modelServers.matchLabels."kaito\.sh\/workspace"=$2 \
        --set inferencePool.targetPortNumber=5000 \
        --set provider.name=istio \
        --version v1.0.0 \
        oci://registry.k8s.io/gateway-api-inference-extension/charts/inferencepool
    kubectl patch infpool $1 --type='json' -p='[{"op": "replace", "path": "/spec/targetPorts/0/number", "value":5000}]'

    echo "Adding the ClusterResourcePlacement API object to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ClusterResourcePlacement
metadata:
  name: infpool-epp-$1
spec:
  resourceSelectors:
    - group: rbac.authorization.k8s.io
      kind: ClusterRole
      name: $1-epp
      version: v1
    - group: rbac.authorization.k8s.io
      kind: ClusterRoleBinding
      name: $1-epp
      version: v1
  policy:
    placementType: PickFixed
    clusterNames:
    - $3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 100%
      unavailablePeriodSeconds: 1
    applyStrategy:
      whenToTakeOver: IfNoDiff
      whenToApply: IfNotDrifted
EOF

    echo "Adding the ResourcePlacement API object to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ResourcePlacement
metadata:
  name: infpool-epp-deepseek
  namespace: default
spec:
  resourceSelectors:
    - group: ""
      kind: ConfigMap
      name: $1-epp
      version: v1
    - group: apps
      kind: Deployment
      name: $1-epp
      version: v1
    - group: ""
      kind: Service
      name: $1-epp
      version: v1
    - group: inference.networking.k8s.io
      kind: InferencePool
      name: $1
      version: v1
    - group: rbac.authorization.k8s.io
      kind: Role
      name: $1-epp
      version: v1
    - group: rbac.authorization.k8s.io
      kind: RoleBinding
      name: $1-epp
      version: v1
    - group: ""
      kind: ServiceAccount
      name: $1-epp
      version: v1
    - group: networking.istio.io
      kind: DestinationRule
      name: $1-epp
      version: v1
  policy:
    placementType: PickFixed
    clusterNames:
    - $3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 100%
      unavailablePeriodSeconds: 1
    applyStrategy:
      whenToTakeOver: IfNoDiff
      whenToApply: IfNotDrifted
EOF
}

function place_single_cluster_gateway_via_kubefleet() {
    echo "Placing gateways on member cluster $1..."
    kubectl config use-context $FLEET_HUB_CTX

    echo "Adding the Gateway API object to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: $INFERENCE_GATEWAY-$2
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    port: 80
    protocol: HTTP
EOF

    echo "Adding the HTTPRoute API object to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: single-model-routes-$2
spec:
  parentRefs:
  - name: $INFERENCE_GATEWAY-$2
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /
    backendRefs:
    - name: $2
      group: inference.networking.k8s.io
      kind: InferencePool
EOF

    echo "Adding the ResourcePlacement API object to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ResourcePlacement
metadata:
  name: gateway-deepseek
  namespace: default
spec:
  resourceSelectors:
    - group: gateway.networking.k8s.io
      kind: Gateway
      name: $INFERENCE_GATEWAY-$2
      version: v1
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: single-model-routes-$2
      version: v1
  policy:
    placementType: PickFixed
    clusterNames:
    - $1
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 100%
      unavailablePeriodSeconds: 1
    applyStrategy:
      whenToTakeOver: IfNoDiff
      whenToApply: IfNotDrifted
EOF
}

function place_multi_cluster_gateway_via_kubefleet() {
    echo "Placing multi-cluster gateways on member cluster $MEMBER_3..."
    kubectl config use-context $FLEET_HUB_CTX

    echo "Adding the Gateway API object to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: $INFERENCE_GATEWAY
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    port: 80
    protocol: HTTP
EOF

    echo "Adding the HTTPRoute API object to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: multi-model-routes
spec:
  parentRefs:
  - name: $INFERENCE_GATEWAY
  rules:
  - matches:
    - headers:
      - type: Exact
        name: x-selected-model
        value: deepseek-r1-distill-qwen-14b
      path:
        type: PathPrefix
        value: /
    backendRefs:
    - name: deepseek
      group: inference.networking.k8s.io
      kind: InferencePool
  - matches:
    - headers:
      - type: Exact
        name: x-selected-model
        value: phi-4
      path:
        type: PathPrefix
        value: /
    backendRefs:
    - name: phi4
      group: inference.networking.k8s.io
      kind: InferencePool
EOF

    echo "Adding the ResourcePlacement API object to the KubeFleet hub cluster..."
    cat <<EOF | kubectl apply -f -
apiVersion: placement.kubernetes-fleet.io/v1beta1
kind: ResourcePlacement
metadata:
  name: llm-routing-gateway
  namespace: default
spec:
  resourceSelectors:
    - group: gateway.networking.k8s.io
      kind: Gateway
      name: $INFERENCE_GATEWAY
      version: v1
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: multi-model-routes
      version: v1
    - group: networking.istio.io
      kind: DestinationRule
      name: $DEEPSEEK_INF_POOL_INSTALLATION-epp
      version: v1
    - group: networking.istio.io
      kind: DestinationRule
      name: $PHI4_INF_POOL_INSTALLATION-epp
      version: v1
  policy:
    placementType: PickFixed
    clusterNames:
    - $MEMBER_3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 100%
      unavailablePeriodSeconds: 1
    applyStrategy:
      whenToTakeOver: IfNoDiff
      whenToApply: IfNotDrifted
EOF
}

function place_resources_via_kubefleet() {
    echo "Placing resources via KubeFleet..."

    install_crds
    label_member_clusters

    place_kaito_workspaces $DEEPSEEK_WORKSPACE $DEEPSEEK_MODEL_NAME
    place_kaito_workspaces $PHI4_WORKSPACE $PHI4_MODEL_NAME

    place_inf_pool_epp_via_kubefleet $DEEPSEEK_INF_POOL_INSTALLATION $DEEPSEEK_WORKSPACE $MEMBER_1
    place_inf_pool_epp_via_kubefleet $PHI4_INF_POOL_INSTALLATION $PHI4_WORKSPACE $MEMBER_2

    place_single_cluster_gateway_via_kubefleet $MEMBER_1 $DEEPSEEK_INF_POOL_INSTALLATION
    place_single_cluster_gateway_via_kubefleet $MEMBER_2 $PHI4_INF_POOL_INSTALLATION
}