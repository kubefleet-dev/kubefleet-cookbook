function build_kubefleet_images() {
    export OUTPUT_TYPE="type=registry"
    export REGISTRY="$ACR.azurecr.io"
    export TAG="demo"
    export TARGET_ARCH="amd64"
    export AUTO_DETECT_ARCH="FALSE"

    echo "Cloning the KubeFleet source code repository..."
    git clone https://github.com/kubefleet-dev/kubefleet.git
    pushd kubefleet
    git checkout kubefleet-kaito-demo-2025

    echo "Building the KubeFleet images and pushing them to ACR..."
    make docker-build-hub-agent
    make docker-build-member-agent
    make docker-build-refresh-token
}

function install_kubefleet_hub_agent() {
    echo "Installing KubeFleet hub agent in the KubeFleet hub cluster..."
    kubectl config use-context $FLEET_HUB_CTX
    helm upgrade --install hub-agent ./charts/hub-agent/ \
        --set image.pullPolicy=Always \
        --set image.repository=$REGISTRY/$HUB_AGENT_IMAGE \
        --set image.tag=$TAG \
        --set namespace=fleet-system \
        --set logVerbosity=5 \
        --set enableWebhook=false \
        --set webhookClientConnectionType=service \
        --set forceDeleteWaitTime="1m0s" \
        --set clusterUnhealthyThreshold="3m0s" \
        --set logFileMaxSize=100000 \
        --set MaxConcurrentClusterPlacement=200 \
        --set resourceSnapshotCreationMinimumInterval=$RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL \
        --set resourceChangesCollectionDuration=$RESOURCE_CHANGES_COLLECTION_DURATION
}

function set_up_kubefleet_member_cluster_access() {
    echo "Creating the service account for KubeFleet member cluster $1..."
    kubectl config use-context $FLEET_HUB_CTX
    kubectl create serviceaccount fleet-member-agent-$1 -n fleet-system
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: fleet-member-agent-$1-sa
  namespace: fleet-system
  annotations:
    kubernetes.io/service-account.name: fleet-member-agent-$1
type: kubernetes.io/service-account-token
EOF

    echo "Adding the service account token to the KubeFleet member cluster $1..."
    local TOKEN=$(kubectl get secret fleet-member-agent-$1-sa -n fleet-system -o jsonpath='{.data.token}' | base64 -d)
    kubectl config use-context $2
    kubectl delete secret hub-kubeconfig-secret --ignore-not-found
    kubectl create secret generic hub-kubeconfig-secret --from-literal=token=$TOKEN
}

function install_kubefleet_member_agent() {
    echo "Installing KubeFleet member agent in the KubeFleet member cluster $1..."
    kubectl config use-context $2

    helm upgrade --install member-agent ./charts/member-agent/ \
        --set config.hubURL=$FLEET_HUB_ADDR \
        --set image.repository=$REGISTRY/$MEMBER_AGENT_IMAGE \
        --set image.tag=$TAG \
        --set refreshtoken.repository=$REGISTRY/$REFRESH_TOKEN_IMAGE \
        --set refreshtoken.tag=$TAG \
        --set image.pullPolicy=Always \
        --set refreshtoken.pullPolicy=Always \
        --set config.memberClusterName=$1 \
        --set logVerbosity=5 \
        --set namespace=fleet-system \
        --set enableV1Alpha1APIs=false \
        --set enableV1Beta1APIs=true \
        --set propertyProvider=$PROPERTY_PROVIDER
}

function create_member_cluster_object() {
    echo "Creating KubeFleet MemberCluster API object for cluster $1 in the hub cluster..."
    kubectl config use-context $FLEET_HUB_CTX

    cat <<EOF | kubectl apply -f -
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata:
  name: $1
spec:
  identity:
    name: fleet-member-agent-$1
    kind: ServiceAccount
    namespace: fleet-system
    apiGroup: ""
EOF
}

function set_up_kubefleet() {
    echo "Setting up the KubeFleet hub cluster..."
    install_kubefleet_hub_agent

    echo "Setting up the KubeFleet member clusters..."
    FLEET_HUB_ADDR=https://$(az aks show --resource-group $RG --name $FLEET_HUB --query "fqdn" -o tsv):443
    
    set_up_kubefleet_member_cluster_access $MEMBER_1 $MEMBER_1_CTX
    install_kubefleet_member_agent $MEMBER_1 $MEMBER_1_CTX
    create_member_cluster_object $MEMBER_1

    set_up_kubefleet_member_cluster_access $MEMBER_2 $MEMBER_2_CTX
    install_kubefleet_member_agent $MEMBER_2 $MEMBER_2_CTX
    create_member_cluster_object $MEMBER_2

    set_up_kubefleet_member_cluster_access $MEMBER_3 $MEMBER_3_CTX
    install_kubefleet_member_agent $MEMBER_3 $MEMBER_3_CTX
    create_member_cluster_object $MEMBER_3

    popd
}