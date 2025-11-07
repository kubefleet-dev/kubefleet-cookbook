function prep_istio_setup() {
    echo "Cloning the Istio source code repository..."
    git clone https://github.com/istio/istio.git
    pushd istio

    git fetch --all
    git checkout $ISTIO_TAG
}

function connect_to_multi_cluster_service_mesh() {
    echo "Connecting AKS cluster $1 to the multi-cluster Istio service mesh..."
    kubectl config use-context $2
    go run ./istioctl/cmd/istioctl install \
        --context $2 \
        --set tag=$ISTIO_TAG \
        --set hub=gcr.io/istio-release \
        --set values.global.meshID=simplemesh \
        --set values.global.multiCluster.clusterName=$1 \
        --set values.global.network=simplenet \
        --set values.pilot.env.ENABLE_GATEWAY_API_INFERENCE_EXTENSION=true

    istioctl create-remote-secret --context=$3 --name=$4 --server $5 | kubectl apply --context=$2 -f -
    istioctl create-remote-secret --context=$6 --name=$7 --server $8 | kubectl apply --context=$2 -f -
}

function set_up_istio() {
    echo "Performing some preparatory steps before setting Istio up..."
    prep_istio_setup

    echo "Setting up the Istio multi-cluster service mesh on the KubeFleet member clusters..."
    MEMBER_1_ADDR=https://$(az aks show --resource-group $RG --name $MEMBER_1 --query "fqdn" -o tsv):443
    MEMBER_2_ADDR=https://$(az aks show --resource-group $RG --name $MEMBER_2 --query "fqdn" -o tsv):443
    MEMBER_3_ADDR=https://$(az aks show --resource-group $RG --name $MEMBER_3 --query "fqdn" -o tsv):443

    connect_to_multi_cluster_service_mesh $MEMBER_1 $MEMBER_1_CTX $MEMBER_2_CTX $MEMBER_2 $MEMBER_2_ADDR $MEMBER_3_CTX $MEMBER_3 $MEMBER_3_ADDR
    connect_to_multi_cluster_service_mesh $MEMBER_2 $MEMBER_2_CTX $MEMBER_1_CTX $MEMBER_1 $MEMBER_1_ADDR $MEMBER_3_CTX $MEMBER_3 $MEMBER_3_ADDR
    connect_to_multi_cluster_service_mesh $MEMBER_3 $MEMBER_3_CTX $MEMBER_1_CTX $MEMBER_1 $MEMBER_1_ADDR $MEMBER_2_CTX $MEMBER_2 $MEMBER_2_ADDR

    popd
}