function set_up_semantic_router() {
    echo "Setting up semantic router in member cluster $MEMBER_3..."
    git clone https://github.com/rambohe-ch/semantic-router.git
    pushd semantic-router
    git checkout add-helm-chart

    kubectl config use-context $MEMBER_3_CTX
    helm upgrade --install semantic-router --namespace vllm-semantic-router-system ./deploy/helm/semantic-router

    popd
}