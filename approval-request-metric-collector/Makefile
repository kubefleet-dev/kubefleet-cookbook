# Makefile for ApprovalRequest Controller and Metric Collector

# Image settings
REGISTRY ?=
TAG ?= latest

# Build settings
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Tools
CONTROLLER_GEN_VERSION ?= v0.16.0
CONTROLLER_GEN = go run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Code Generation

.PHONY: manifests
manifests: ## Generate CRD manifests
	$(CONTROLLER_GEN) crd paths="./apis/..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: ## Generate DeepCopy code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./apis/..."

##@ Build

.PHONY: docker-build-approval-controller
docker-build-approval-controller: ## Build approval-request-controller docker image
	docker buildx build \
		--file docker/approval-request-controller.Dockerfile \
		--tag $(REGISTRY)/approval-request-controller:$(TAG) \
		--platform=linux/$(GOARCH) \
		--build-arg GOARCH=$(GOARCH) \
		--push \
		.

.PHONY: docker-build-metric-collector
docker-build-metric-collector: ## Build metric-collector docker image
	docker buildx build \
		--file docker/metric-collector.Dockerfile \
		--tag $(REGISTRY)/metric-collector:$(TAG) \
		--platform=linux/$(GOARCH) \
		--build-arg GOARCH=$(GOARCH) \
		--push \
		.

.PHONY: docker-build-metric-app
docker-build-metric-app: ## Build metric-app docker image
	docker buildx build \
		--file docker/metric-app.Dockerfile \
		--tag $(REGISTRY)/metric-app:$(TAG) \
		--platform=linux/$(GOARCH) \
		--build-arg GOARCH=$(GOARCH) \
		--push \
		.

.PHONY: docker-build-all
docker-build-all: docker-build-approval-controller docker-build-metric-collector docker-build-metric-app ## Build and push all docker images

.PHONY: docker-build
docker-build: docker-build-all ## Alias for docker-build-all
