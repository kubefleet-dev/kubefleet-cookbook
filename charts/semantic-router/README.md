# Semantic Router Helm Chart

This Helm chart deploys Semantic Router - an Intelligent Mixture-of-Models Router for Efficient LLM Inference on Kubernetes.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- PV provisioner support in the underlying infrastructure (for model storage)

## Installing the Chart

To install the chart with the release name `my-semantic-router`:

```bash
helm install my-semantic-router ./semantic-router
```

Or from a packaged chart:

```bash
helm package semantic-router
helm install my-semantic-router semantic-router-0.1.0.tgz
```

## Uninstalling the Chart

To uninstall/delete the `my-semantic-router` deployment:

```bash
helm uninstall my-semantic-router
```

## Configuration

The following table lists the configurable parameters of the Semantic Router chart and their default values.

### Global Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.namespace` | Kubernetes namespace | `vllm-semantic-router-system` |

### Image Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Image repository | `ghcr.io/vllm-project/semantic-router/extproc` |
| `image.pullPolicy` | Image pull policy | `Always` |
| `image.tag` | Image tag | `latest` |

### Deployment Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |

### Service Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `service.type` | Service type | `ClusterIP` |
| `service.grpc.port` | gRPC port | `50051` |
| `service.api.port` | API port | `8080` |
| `service.metrics.enabled` | Enable metrics service | `true` |
| `service.metrics.port` | Metrics port | `9190` |

### Resource Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.requests.memory` | Memory request | `3Gi` |
| `resources.requests.cpu` | CPU request | `1` |
| `resources.limits.memory` | Memory limit | `6Gi` |
| `resources.limits.cpu` | CPU limit | `2` |

### Persistence Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `persistence.enabled` | Enable persistence | `true` |
| `persistence.storageClassName` | Storage class name | `azurefile` |
| `persistence.accessMode` | Access mode | `ReadWriteOnce` |
| `persistence.size` | Storage size | `10Gi` |

### Model Download Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `models.download.enabled` | Enable automatic model download | `true` |
| `models.download.list` | List of models to download | See values.yaml |

### Configuration Parameters

All configuration parameters from `config.yaml` can be overridden. See `values.yaml` for the complete list.

## Examples

### Basic Installation

```bash
helm install semantic-router ./semantic-router
```

### Installation with Custom Values

```bash
helm install semantic-router ./semantic-router \
  --set replicaCount=2 \
  --set resources.requests.memory=4Gi \
  --set persistence.size=20Gi
```

### Installation with Custom Configuration File

```bash
helm install semantic-router ./semantic-router -f custom-values.yaml
```

Example `custom-values.yaml`:

```yaml
replicaCount: 1

resources:
  requests:
    memory: "4Gi"
    cpu: "2"
  limits:
    memory: "8Gi"
    cpu: "4"

persistence:
  size: 20Gi

config:
  vllmEndpoints:
    - name: "production-endpoint"
      address: "10.0.0.10"
      port: 8000
      weight: 1
```

### Upgrade an Existing Release

```bash
helm upgrade semantic-router ./semantic-router -f custom-values.yaml
```

### Check Deployment Status

```bash
kubectl get pods -n vllm-semantic-router-system
kubectl logs -n vllm-semantic-router-system -l app=semantic-router
```

### Access the Service

Port forward to access the API:

```bash
kubectl port-forward -n vllm-semantic-router-system svc/semantic-router 8080:8080
```

Test the API:

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "auto",
    "messages": [
      {"role": "user", "content": "What is the derivative of x^2?"}
    ]
  }'
```

## Model Selection Strategy

The semantic router intelligently routes requests to different models based on task complexity and type. The default configuration includes two models with different strengths:

### Model Distribution

| Model | Use Cases | Estimated Traffic | Typical Scenarios |
|-------|-----------|-------------------|-------------------|
| **phi-4** | Simple tasks, chat, consultation | ~30-40% | Casual chat, business communication, simple Q&A, historical facts |
| **deepseek-r1-distill-qwen-14b** | Complex reasoning tasks | ~60-70% | Math proofs, scientific analysis, legal reasoning, economic modeling |

### phi-4 Selection Scenarios

The **phi-4** model (192.168.2.1:5000) is optimized for conversational and simple tasks:

#### 1. Other/General Chat (Highest Priority for phi-4)

- **Score**: phi-4: 0.9 vs deepseek: 0.6
- **Examples**:

  ```bash
  # Casual conversation
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "Hello, how are you today?"}]}'
  
  # General questions
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "Tell me a joke"}]}'
  ```

#### 2. Business Communication

- **Score**: phi-4: 0.85 vs deepseek: 0.7
- **Examples**:

  ```bash
  # Business email writing
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "How to write an effective business proposal?"}]}'
  
  # Team communication
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "Tips for improving team collaboration"}]}'
  ```

#### 3. History Questions

- **Score**: phi-4: 0.85 vs deepseek: 0.7
- **Examples**:

  ```bash
  # Historical facts
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "When did World War II start?"}]}'
  
  # Historical overview
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "Tell me about the Tang Dynasty"}]}'
  ```

#### 4. Psychology Consultation

- **Score**: phi-4: 0.8 vs deepseek: 0.6
- **Examples**:

  ```bash
  # Stress management
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "How to manage work stress?"}]}'
  
  # Emotional intelligence
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "What is emotional intelligence?"}]}'
  ```

### deepseek-r1-distill-qwen-14b Selection Scenarios

The **deepseek-r1-distill-qwen-14b** model (192.168.1.1:5000) excels at complex reasoning tasks with "thinking" mode enabled:

#### 1. Math & Reasoning (Perfect Score + Reasoning)

- **Score**: deepseek: 1.0 (reasoning: true) vs phi-4: 0.9
- **Example**:

  ```bash
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "Prove the Pythagorean theorem step by step"}]}'
  ```

#### 2. Physics Analysis (Perfect Score + Reasoning)

- **Score**: deepseek: 1.0 (reasoning: true) vs phi-4: 0.7
- **Example**:

  ```bash
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "Explain general relativity and spacetime curvature"}]}'
  ```

#### 3. Chemistry Mechanisms (Perfect Score + Reasoning)

- **Score**: deepseek: 1.0 (reasoning: true) vs phi-4: 0.6
- **Example**:

  ```bash
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "Explain the mechanism of oxidation-reduction reactions"}]}'
  ```

#### 4. Legal Analysis (High Score + Reasoning)

- **Score**: deepseek: 0.9 (reasoning: true) vs phi-4: 0.4
- **Example**:

  ```bash
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "Analyze the legal implications of contract breach"}]}'
  ```

#### 5. Economic Modeling (Perfect Score + Reasoning)

- **Score**: deepseek: 1.0 (reasoning: true) vs phi-4: 0.8
- **Example**:

  ```bash
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "auto", "messages": [{"role": "user", "content": "Explain the transmission mechanism of monetary policy"}]}'
  ```

### Testing Model Selection

To verify which model was selected for your request, check the response headers or logs:

```bash
# Enable verbose output to see routing headers
curl -v -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "auto", "messages": [{"role": "user", "content": "Your question here"}]}' \
  2>&1 | grep -i "x-selected-model"
```

Or check the pod logs:

```bash
kubectl logs -n vllm-semantic-router-system -l app=semantic-router --tail=50 | grep "Selected model"
```

### Customizing Model Selection

You can adjust model selection priorities by modifying the `categories` section in `values.yaml`. Each category has model scores that determine routing decisions:

```yaml
config:
  categories:
    - name: business
      modelScores:
        - model: phi-4
          score: 0.85  # Higher score = higher priority
          useReasoning: false
        - model: deepseek-r1-distill-qwen-14b
          score: 0.7
          useReasoning: false
```

To force a specific model, set the model name explicitly instead of using "auto":

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "phi-4", "messages": [{"role": "user", "content": "Hello"}]}'
```

## Troubleshooting

### Check Pod Status

```bash
kubectl get pods -n vllm-semantic-router-system
kubectl describe pod <pod-name> -n vllm-semantic-router-system
```

### View Logs

```bash
kubectl logs -n vllm-semantic-router-system -l app=semantic-router --tail=100
```

### Check Model Download Status

```bash
kubectl exec -n vllm-semantic-router-system -it <pod-name> -- ls -la /app/models
```

### Access Metrics

```bash
kubectl port-forward -n vllm-semantic-router-system svc/semantic-router-metrics 9190:9190
curl http://localhost:9190/metrics
```

## Architecture

The chart deploys the following components:

1. **Namespace**: Isolated namespace for semantic-router resources
2. **ConfigMap**: Configuration for the semantic router (config.yaml and tools_db.json)
3. **PersistentVolumeClaim**: Storage for ML models
4. **Deployment**: Main semantic router application with:
   - Init container for model download from Hugging Face
   - Main container running the semantic router
5. **Services**:
   - Main service for gRPC and API endpoints
   - Metrics service for Prometheus scraping

## Security

The deployment includes:
- Non-root user execution
- No privilege escalation
- Read-only configuration mounts
- Resource limits

## Contributing

For issues and contributions, please visit: https://github.com/vllm-project/semantic-router

## License

See the [LICENSE](https://github.com/vllm-project/semantic-router/blob/main/LICENSE) file for details.
