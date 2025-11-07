# How to run the scripts in this tutorial

The scripts in this tutorial will help you:

* Create a fleet of 3 AKS (Azure Kubernetes Service) clusters for running LLM inference workloads and routing LLM queries.
* Put the 3 clusters under the management of KubeFleet, a CNCF sandbox project for multi-cluster management, with an
additional KubeFleet hub cluster (also an AKS cluster) as the management portal.
* Set up KAITO, a CNCF sandbox project for easy LLM usage, on the clusters for facilitating LLM workloads with ease.
* Connect the 3 clusters with an Istio service mesh.
* Use Kubernetes Gateway API with Inference Extension for serving LLM queries.

> Note that even though the scripts are set to use AKS clusters and related resources for simplicity reasons; the tutorial itself is not necessarily Azure specific. It can run on any Kubernetes environment, as long as inter-cluster connectivity can be established.

## Before you begin

* This tutorial assumes that you are familiar with basic Azure/AKS usage and Kubernetes usage.
* If you don't have an Azure account, [create a free account](https://azure.microsoft.com/pricing/purchase-options/azure-account) before you begin.
* Make sure that you have the following tools installed in your environment:
    * The Azure CLI (`az`).
    * The Kubernetes CLI (`kubectl`).
    * Helm
    * Docker
    * The Istio CLI (istioctl)
    * Go runtime (>=1.24)
    * `git`
    * `base64`
    * `make`
    * `curl`
* The setup in the tutorial requires usage of GPU-enabled nodes (with NVIDIA A100 GPUs or similar specs).

## Run the scripts

Switch to the current directory and follow the steps below to run the scripts:

```sh
chmod +x setup.sh
./setup.sh
```

It may take a while for the setup to complete.

The script includes some configurable parameters; in most cases though, you should be able to just use
the default values. See the list of parameters at the file `setup.sh`, and, if needed, set up
environment variables accordingly to override the default values.

## Verify the setup

After the setup script completes, follow the steps below to verify the setup:

* Switch to one of the clusters that is running the inference workload:

    ```sh
    MEMBER_1="${MEMBER_1:-model-serving-cluster-1}"
    MEMBER_2="${MEMBER_2:-model-serving-cluster-2}"
    MEMBER_3="${MEMBER_3:-query-routing-cluster}"
    MEMBER_1_CTX=$MEMBER_1-admin
    MEMBER_2_CTX=$MEMBER_2-admin
    MEMBER_3_CTX=$MEMBER_3-admin

    kubectl config use-context $MEMBER_1_CTX
    kubectl get workspace
    ```

    You should see that the KAITO workspace with the DeepSeek model is up and running. Note that it may take 
    a while for a GPU node to get ready and have the model downloaded/set up.

* Similarly, switch to the other cluster that is running the inference workload and make sure that the Phi model
is up and running:

    ```sh
    kubectl config use-context $MEMBER_2_CTX
    kubectl get workspace
    ```

* Now, switch to the query routing cluster and send some queries to the inference gateway:

    ```sh
    kubectl config use-context $MEMBER_3_CTX

    # Open another shell window.
    kubectl port-forward svc/inference-gateway-istio 10000:80

    curl -X POST http://localhost:10000/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{
        "model": "auto",
        "messages": [{"role": "user", "content": "Prove the Pythagorean theorem step by step"}],
        "max_tokens": 100    
    }'
    ```

    You should see from the response that the query is being served by the DeepSeek model.

    ```sh
    curl -X POST -i localhost:10000/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{
        "model": "auto",
        "messages": [{"role": "user", "content": "What is the color of the sky?"}],
        "max_tokens": 100
    }'
    ```

    You should see from the response that the query is being served by the Phi model.

    > Note: the tutorial features a semantic router that classifies queries based on their categories and sends queries to a LLM that is best equipped to process the category. The process is partly non-deterministic due to the nature of LLM. If you believe that a query belongs to a specific category but is not served by the expected LLM; tweak the query text a bit and give it another try.

## Additional steps

You can set up the LiteLLM proxy to interact with the models using a web UI. Follow the steps in the [LiteLLM setup README](./litellm/README.md) to complete the setup.

## Clean things up

To clean things up, delete the Azure resource group that contains all the resources:

```sh
export RG="${RG:-kubefleet-kaito-demo-2025}"
az group delete -n $RG
```

## Questions or comments?

If you have any questions or comments please using our [Q&A Discussions](https://github.com/kubefleet-dev/kubefleet/discussions/categories/q-a). 

If you find a bug or the solution doesn't work, please open an [Issue](https://github.com/kubefleet-dev/kubefleet/issues/new) so we can take a look. We welcome submissions too, so if you find a fix please open a PR!

Also, consider coming to a [Community Meeting](https://bit.ly/kubefleet-cm-meeting) too!
