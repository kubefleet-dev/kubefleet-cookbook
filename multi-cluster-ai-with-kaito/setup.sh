#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# Required variables.
if [ -z "$SUBSCRIPTION" ]; then
    echo "Variable SUBSCRIPTION is not set"
    exit 1
fi

# Default configuration for the setup.
RG="${RG:-kubefleet-kaito-demo-2025}"
LOCATION="${LOCATION:-eastus}"
VNET="${VNET:-shared-vnet}"
VNET_ADDR_PREFIX="${VNET_ADDR_PREFIX:-'10.0.0.0/8'}"
SUBNET_1="${SUBNET_1:-aks-subnet-1}"
SUBNET_1_ADDR_PREFIX="${SUBNET_1_ADDR_PREFIX:-'10.1.0.0/16'}"
SUBNET_2="${SUBNET_2:-aks-subnet-2}"
SUBNET_2_ADDR_PREFIX="${SUBNET_2_ADDR_PREFIX:-'10.2.0.0/16'}"
SUBNET_3="${SUBNET_3:-aks-subnet-routing}"
SUBNET_3_ADDR_PREFIX="${SUBNET_3_ADDR_PREFIX:-'10.3.0.0/16'}"
FLEET_HUB="${FLEET_HUB:-hub-cluster}"
MEMBER_1="${MEMBER_1:-model-serving-cluster-1}"
MEMBER_2="${MEMBER_2:-model-serving-cluster-2}"
MEMBER_3="${MEMBER_3:-query-routing-cluster}"
ACR="${ACR:-kubefleetkaitodemo2025$(echo $RANDOM | md5sum | head -c 6)}"
VM_SIZE="${VM_SIZE:-Standard_D4s_v3}"
GPU_VM_SIZE="${GPU_VM_SIZE:-Standard_NC24ads_A100_v4}"
DEEPSEEK_WORKSPACE="${DEEPSEEK_WORKSPACE:-workspace-deepseek-r1-distill-qwen-14b}"
PHI4_WORKSPACE="${PHI4_WORKSPACE:-workspace-phi-4}"
DEEPSEEK_MODEL="${DEEPSEEK_MODEL:-deepseek-r1-distill-qwen-14b}"
PHI4_MODEL="${PHI4_MODEL:-phi-4}"
DEEPSEEK_INF_POOL_INSTALLATION="${DEEPSEEK_INF_POOL_INSTALLATION:-deepseek}"
PHI4_INF_POOL_INSTALLATION="${PHI4_INF_POOL_INSTALLATION:-phi4}"
MEMBER_1_CTX=$MEMBER_1-admin
MEMBER_2_CTX=$MEMBER_2-admin
MEMBER_3_CTX=$MEMBER_3-admin
FLEET_HUB_CTX=$FLEET_HUB-admin
INFERENCE_GATEWAY="inference-gateway"

# The configuration below are for the KubeFleet setup; in most cases they do not need to be changed.
HUB_AGENT_IMAGE="hub-agent"
MEMBER_AGENT_IMAGE="member-agent"
REFRESH_TOKEN_IMAGE="refresh-token"
PROPERTY_PROVIDER="azure"
RESOURCE_SNAPSHOT_CREATION_MINIMUM_INTERVAL="0m"
RESOURCE_CHANGES_COLLECTION_DURATION="0m"
REGISTRY="$ACR.azurecr.io"
TAG="demo"

# The configuration below are for the Istio setup; in most cases they do not need to be changed.
ISTIO_TAG=1.28.0-beta.1

# Source the utility functions.
source ./azresources.sh
source ./kubefleet_setup.sh
source ./istio.sh
source ./kaito.sh
source ./kubefleet_placement.sh
source ./semantic_router.sh

# Log in to Azure CLI and set the subscription to use.
az login
az account set --subscription $SUBSCRIPTION

# Set up the Azure resource group.
echo "Creating resource group $RG in location $LOCATION..."
az group create --name $RG --location $LOCATION

# Set up the Azure networking resources.
create_azure_vnet
create_azure_vnet_subnets

# Set up the AKS clusters.
create_aks_clusters

# Set up the ACR.
create_acr

# Set up KubeFleet.
build_kubefleet_images
set_up_kubefleet

# Set up Istio.
set_up_istio

# Set up Kaito.
set_up_kaito

# Place resources via KubeFleet.
place_resources_via_kubefleet

# Set up semantic router.
set_up_semantic_router
