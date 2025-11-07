function set_up_semantic_router() {
    echo "Setting up semantic router in member cluster $MEMBER_3..."

    kubectl config use-context $MEMBER_3_CTX
    helm upgrade \
        --install semantic-router \
        --namespace vllm-semantic-router-system \
        --create-namespace \
        --set namespace.create=false \
        charts/semantic-router.tgz
    
    cat <<EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: semantic-router
  namespace: default
spec:
  configPatches:
  - applyTo: HTTP_FILTER
    match:
      context: GATEWAY
      listener:
        filterChain:
          filter:
            name: envoy.filters.network.http_connection_manager
            subFilter:
              name: envoy.filters.http.router
    patch:
      operation: INSERT_BEFORE
      value:
        name: envoy.filters.http.ext_proc
        typed_config:
          '@type': type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor
          allow_mode_override: true
          failure_mode_allow: true
          grpc_service:
            envoy_grpc:
              cluster_name: outbound|50051||semantic-router.vllm-semantic-router-system.svc.cluster.local
            timeout: 30s
          max_message_timeout: 600s
          message_timeout: 300s
          mutation_rules:
            allow_all_routing: false
            allow_envoy: false
            disallow_system: true
          processing_mode:
            request_body_mode: BUFFERED
            request_header_mode: SEND
            request_trailer_mode: SKIP
            response_body_mode: BUFFERED
            response_header_mode: SEND
            response_trailer_mode: SKIP
EOF

    popd
}