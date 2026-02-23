
# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
GOLANGCI_LINT_VERSION ?= v1.54.2
golangci-lint:
	@[ -f $(GOLANGCI_LINT) ] || { \
	set -e ;\
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell dirname $(GOLANGCI_LINT)) $(GOLANGCI_LINT_VERSION) ;\
	}

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter & yamllint
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v5.2.1
CONTROLLER_TOOLS_VERSION ?= v0.20.0

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || GOBIN=$(LOCALBIN) GO111MODULE=on go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

##@ Helm

HELM_CHART_DIR ?= helm

.PHONY: helm-crds
helm-crds: manifests ## Copy CRDs to Helm chart
	cp config/crd/bases/*.yaml $(HELM_CHART_DIR)/crds/

.PHONY: helm-lint
helm-lint: ## Lint Helm chart
	helm lint $(HELM_CHART_DIR)

.PHONY: helm-template
helm-template: ## Template Helm chart for debugging
	helm template gnmic-operator $(HELM_CHART_DIR)

.PHONY: helm-package
helm-package: helm-crds ## Package Helm chart
	helm package $(HELM_CHART_DIR)

##@ Development

CLUSTER_NAME ?= gnmic-dev
CERT_MANAGER_VERSION ?= v1.19.3

.PHONY: setup-dev-cluster
setup-dev-cluster: deploy-dev-cluster install-dev-cluster-dependencies load-dev-image

.PHONY: deploy-dev-cluster
deploy-dev-cluster: ## Deploy the development cluster
	kind create cluster --name $(CLUSTER_NAME)

.PHONY: undeploy-dev-cluster
undeploy-dev-cluster: ## Undeploy the development cluster
	kind delete cluster --name $(CLUSTER_NAME)

.PHONY: install-dev-cluster-dependencies
install-dev-cluster-dependencies: ## Install the dependencies for the development cluster
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml
	echo "waiting for cert manager to be ready..."
	kubectl wait --namespace cert-manager --for=condition=Available deployment --all --timeout=180s
	echo "cert manager ready"

.PHONY: load-dev-image
load-dev-image: ## Load the development image into the development cluster
	kind load docker-image $(IMG) --name $(CLUSTER_NAME)

##@ Development Lab

TARGET_USERNAME ?= admin
TARGET_PASSWORD ?= NokiaSrl1!

.PHONY: setup-dev-lab
setup-dev-lab: deploy-dev-lab configure-nodes-dev-lab apply-resources-dev-lab ## Setup the development lab cluster

.PHONY: deploy-dev-lab
deploy-dev-lab: ## Deploy a simple 3-node container lab topology
	sudo containerlab deploy -t lab/dev/3-nodes.clab.yaml -c

.PHONY: undeploy-dev-lab
undeploy-dev-lab: ## Undeploy the operator from the development lab cluster
	sudo containerlab destroy -t lab/dev/3-nodes.clab.yaml -c

.PHONY: configure-nodes-dev-lab
configure-nodes-dev-lab: ## Configure the nodes in the development lab cluster
	gnmic -a clab-3-nodes-spine1:57400 -u $(TARGET_USERNAME) -p $(TARGET_PASSWORD) --skip-verify set --request-file lab/dev/configs/spine1.yaml
	gnmic -a clab-3-nodes-leaf1:57400 -u $(TARGET_USERNAME) -p $(TARGET_PASSWORD) --skip-verify set --request-file lab/dev/configs/leaf1.yaml
	gnmic -a clab-3-nodes-leaf2:57400 -u $(TARGET_USERNAME) -p $(TARGET_PASSWORD) --skip-verify set --request-file lab/dev/configs/leaf2.yaml

.PHONY: apply-resources-dev-lab
apply-resources-dev-lab: apply-targets-dev-lab apply-subscriptions-dev-lab apply-outputs-dev-lab apply-pipelines-dev-lab apply-clusters-dev-lab ## Apply the resources for the development lab cluster
.PHONY: delete-resources-dev-lab
delete-resources-dev-lab: delete-clusters-dev-lab delete-targets-dev-lab delete-subscriptions-dev-lab delete-outputs-dev-lab delete-pipelines-dev-lab ## Delete the resources for the development lab cluster
.PHONY: apply-targets-dev-lab
apply-targets-dev-lab: ## Apply the targets for the development lab cluster
	kubectl apply -f lab/dev/resources/targets/profile
	kubectl apply -f lab/dev/resources/targets

.PHONY: delete-targets-dev-lab
delete-targets-dev-lab: ## Delete the targets for the development lab cluster
	kubectl delete -f lab/dev/resources/targets

.PHONY: apply-subscriptions-dev-lab
apply-subscriptions-dev-lab: ## Apply the subscriptions for the development lab cluster
	kubectl apply -f lab/dev/resources/subscriptions

.PHONY: delete-subscriptions-dev-lab
delete-subscriptions-dev-lab: ## Delete the subscriptions for the development lab cluster
	kubectl delete -f lab/dev/resources/subscriptions

.PHONY: apply-outputs-dev-lab
apply-outputs-dev-lab: ## Apply the outputs for the development lab cluster
	kubectl apply -f lab/dev/resources/outputs

.PHONY: delete-outputs-dev-lab
delete-outputs-dev-lab: ## Delete the outputs for the development lab cluster
	kubectl delete -f lab/dev/resources/outputs

.PHONY: apply-pipelines-dev-lab
apply-pipelines-dev-lab: ## Apply the pipelines for the development lab cluster
	kubectl apply -f lab/dev/resources/pipelines

.PHONY: delete-pipelines-dev-lab
delete-pipelines-dev-lab: ## Delete the pipelines for the development lab cluster
	kubectl delete -f lab/dev/resources/pipelines
.PHONY: apply-clusters-dev-lab
apply-clusters-dev-lab: ## Apply the clusters for the development lab cluster
	kubectl apply -f lab/dev/resources/clusters

.PHONY: delete-clusters-dev-lab
delete-clusters-dev-lab: ## Delete the clusters for the development lab cluster
	kubectl delete -f lab/dev/resources/clusters

