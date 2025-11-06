function prep_kaito_setup() {
    echo "Adding the KAITO Helm charts..."
    helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
    helm repo update

    echo "Retrieving the KAITO GPU Provisioner setup script..."
    GPU_PROVISIONER_VERSION=0.3.7
    curl -sO https://raw.githubusercontent.com/Azure/gpu-provisioner/main/hack/deploy/configure-helm-values.sh
}

function install_kaito_core() {
    echo "Installing KAITO core components in member cluster $1..."
    kubectl config use-context $2
    helm upgrade --install kaito-workspace kaito/workspace \
        --namespace kaito-workspace \
        --create-namespace \
        --set clusterName="$1" \
        --set featureGates.gatewayAPIInferenceExtension=true \
        --wait
}

function install_kaito_gpu_provisioner() {
    echo "Installing KAITO GPU provisioner in member cluster $1..."
    kubectl config use-context $2

    echo "Creating managed identity..."
    local IDENTITY_NAME="kaitogpuprovisioner-$1"
    az identity create --name $IDENTITY_NAME -g $RG
    local IDENTITY_PRINCIPAL_ID=$(az identity show --name $IDENTITY_NAME -g $RG --query 'principalId' -o tsv)
    az role assignment create \
        --assignee $IDENTITY_PRINCIPAL_ID \
        --scope /subscriptions/$SUBSCRIPTION/resourceGroups/$RG/providers/Microsoft.ContainerService/managedClusters/$1 \
        --role "Contributor"

    echo "Configuring Helm values..."
    chmod +x ./configure-helm-values.sh && ./configure-helm-values.sh $1 $RG $IDENTITY_NAME

    echo "Installing Helm chart..."
    helm upgrade --install gpu-provisioner \
        --values gpu-provisioner-values.yaml \
        --set settings.azure.clusterName=$1 \
        --wait \
        https://github.com/Azure/gpu-provisioner/raw/gh-pages/charts/gpu-provisioner-$GPU_PROVISIONER_VERSION.tgz \
        --namespace gpu-provisioner \
        --create-namespace

    echo "Enabling federated authentication..."
    local AKS_OIDC_ISSUER=$(az aks show -n $1 -g $RG --query "oidcIssuerProfile.issuerUrl" -o tsv)
    az identity federated-credential create \
        --name kaito-federated-credential-$1 \
        --identity-name $IDENTITY_NAME \
        -g $RG \
        --issuer $AKS_OIDC_ISSUER \
        --subject system:serviceaccount:"gpu-provisioner:gpu-provisioner" \
        --audience api://AzureADTokenExchange
}

function set_up_kaito() {
    echo "Performing some preparatory steps before setting KAITO up..."
    prep_kaito_setup

    echo "Installing KAITO in member cluster $MEMBER_1..."
    install_kaito_core $MEMBER_1 $MEMBER_1_CTX
    install_kaito_gpu_provisioner $MEMBER_1 $MEMBER_1_CTX

    echo "Installing KAITO in member cluster $MEMBER_2..."
    install_kaito_core $MEMBER_2 $MEMBER_2_CTX
    install_kaito_gpu_provisioner $MEMBER_2 $MEMBER_2_CTX
}