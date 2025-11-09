function create_azure_vnet() {
    echo "Creating an Azure virtual network..."
    az network vnet create \
        --name $VNET \
        -g $RG \
        --location $LOCATION \
        --address-prefix $VNET_ADDR_PREFIX \
        --subnet-name $SUBNET_1 \
        --subnet-prefixes $SUBNET_1_ADDR_PREFIX
}

function create_azure_vnet_subnet() {
    az network vnet subnet create \
        -g $RG \
        --vnet-name $VNET \
        -n $1 \
        --address-prefixes $2
}

function create_azure_vnet_subnets() {
    echo "Creating additional subnets in the virtual network..."
    create_azure_vnet_subnet $SUBNET_2 $SUBNET_2_ADDR_PREFIX
    create_azure_vnet_subnet $SUBNET_3 $SUBNET_3_ADDR_PREFIX
}

function create_aks_cluster() {
    echo "Creating AKS cluster $1..."
    az aks create \
        --name $1 \
        --resource-group $RG \
        --location $LOCATION \
        --vnet-subnet-id $2 \
        --network-plugin azure \
        --enable-oidc-issuer \
        --enable-workload-identity \
        --enable-managed-identity \
        --generate-ssh-keys \
        --node-vm-size $VM_SIZE \
        --node-count 1 \
        --service-cidr $3 \
        --dns-service-ip $4
}

function create_kubefleet_hub_cluster() {
    echo "Creating KubeFleet hub cluster $FLEET_HUB..."
    az aks create \
        --name $FLEET_HUB \
        --resource-group $RG \
        --location $LOCATION \
        --network-plugin azure \
        --enable-oidc-issuer \
        --enable-workload-identity \
        --enable-managed-identity \
        --generate-ssh-keys \
        --node-vm-size $VM_SIZE \
        --node-count 1
}

function create_aks_clusters() {
    SUBNET_1_ID=$(az network vnet subnet show --resource-group $RG --vnet-name $VNET --name $SUBNET_1 --query "id" --output tsv)
    SUBNET_2_ID=$(az network vnet subnet show --resource-group $RG --vnet-name $VNET --name $SUBNET_2 --query "id" --output tsv)
    SUBNET_3_ID=$(az network vnet subnet show --resource-group $RG --vnet-name $VNET --name $SUBNET_3 --query "id" --output tsv)

    echo "Creating AKS clusters..."
    create_aks_cluster $MEMBER_1 $SUBNET_1_ID 172.16.0.0/16 172.16.0.10
    create_aks_cluster $MEMBER_2 $SUBNET_2_ID 172.17.0.0/16 172.17.0.10
    create_aks_cluster $MEMBER_3 $SUBNET_3_ID 172.18.0.0/16 172.18.0.10
    create_kubefleet_hub_cluster

    echo "Retrieving admin credentials for AKS clusters..."
    az aks get-credentials -n $MEMBER_1 -g $RG --admin
    az aks get-credentials -n $MEMBER_2 -g $RG --admin
    az aks get-credentials -n $MEMBER_3 -g $RG --admin
    az aks get-credentials -n $FLEET_HUB -g $RG --admin
}

function create_acr() {
    echo "Creating Azure Container Registry $ACR..."
    az acr create \
        --resource-group $RG \
        --name $ACR \
        --sku Standard \
        --admin-enabled true

    echo "Connecting the ACR to the AKS clusters..."
    az aks update -n $MEMBER_1 -g $RG --attach-acr $ACR
    az aks update -n $MEMBER_2 -g $RG --attach-acr $ACR
    az aks update -n $MEMBER_3 -g $RG --attach-acr $ACR
    az aks update -n $FLEET_HUB -g $RG --attach-acr $ACR

    echo "Logging into the ACR..."
    az acr login --name $ACR
}