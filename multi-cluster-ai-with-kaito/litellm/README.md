# Instructions for setting up the LiteLLM proxy

This document provides additional instructions for setting up the LiteLLM proxy in your environment.

## Before you begin

* Make sure that you have completed other parts of the tutorial.
* Set up a PostgreSQL database server, which LiteLLM requires for storing information.
    * Any PostgreSQL installation should work, as long as the Kubernetes clusters you have created in this
    tutorial can access the PostgreSQL instance. You may use an
    [Azure DB for PostgreSQL instance](https://learn.microsoft.com/en-us/azure/postgresql/flexible-server/quickstart-create-server),
    or deploy a PostgreSQL operator inside the query routing cluster.
    * After the PostgreSQL database server is set up, create a database `litellm` in the server.

        ```sql
        CREATE DATABASE litellm
        ```
    
    * Write down the address of the server, the password of the default `postgres` user, and a username/password combo that LiteLLM
    will use to access the server.

## Setting up LiteLLM

* Edit the `secret.yaml` file in the directory, replace `POSTGRES-PASSWORD`, `YOUR-USERNAME`, and `YOUR-PASSWORD` with
the password of the default `postgres` user, and the username/password for the account that LiteLLM will use respectively.
* Edit the `values.yaml` file in the directory, replace `YOUR-POSTGRES-ENDPOINT` with the address of your PostgreSQL database server.
    * You may find out that there are various placeholders in the file; it is OK to leave them as they are.
* Switch to the current directory, and run the command below to deploy the LiteLLM proxy:

    ```sh
    helm install litellm --values ./values.yaml oci://ghcr.io/berriai/litellm-helm:0.1.742 --namespace litellm --create-namespace
    kubectl apply -f ./secret.yaml
    ```

    It may take a few moments before the LiteLLM proxy starts up.

* LiteLLM will create a secret in the `litellm` namespace, `litellm-masterkey`, that contains the password of the `admin` user, which
you can use to access the LiteLLM UI. To retrieve the password, run the commands below:

    ```sh
    kubectl get secret -n litellm litellm-masterkey -o jsonpath='{.data.masterkey}' | base64 -d
    ```

    Write down the output. Depending on the shell program you use, you may see a precentage sign `%` at the end of the output,
    which represents a missing new line character; ignore it: for example, if the output is `123456%`, the password
    should be `123456`.

* Port forward the LiteLLM service:

    ```sh
    export LITELLM_FORWARDING_PORT=10000
    kubectl port-forward svc/litellm -n litellm $LITELLM_FORWARDING_PORT:4000
    ```

* Open a browser window, and go to `localhost:10000/ui`. You should see that the LiteLLM UI loads up. If prompted for username/password,
use the username `admin` and the master password you just wrote down.

* On the left panel, click `Models + Endpoints`. Then switch to the `Add Model` tab.

* Add a new model using the setup below:

    * For the `Provider` part, pick `OpenAI-Compatible Endpoints`.
    * For the `LiteLLM Model Name(s)` part, type `openai/auto`.
    * For the `Mode` part, pick `Chat - /chat/completions`.
    * For the `API Base` part, type `http://inference-gateway-istio.default.svc.cluster.local/v1` if you haven't updated the name of
    the inference gateway when you set up the environment; replace `inference-gateway` with the value of your own if the name
    has been modified.
    * No need to change other parts.

* Click the `Test Connect` button; you should see a connection successful message.
* Click the `Add Model` button to add the model.

* On the left panel, check `Test Key`.

* Make sure that in the `Configurations` panel, the model `openai/auto` has been selected and the endpoint type is `/v1/chat/completions`.

* You can now use the chat panel to interact with the models. 
    * Note that conversational continuity may lead to your messages keep landing on the same model; remember to clear the chat history
    using the `Clear Chat` button as necessary.
