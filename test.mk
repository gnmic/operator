CLAB_VERSION ?= 0.70.1
KIND_VERSION ?= v0.20.0
GNMIC_VERSION ?= 0.44.1
KUBECTL_VERSION ?= v1.31.0
TEST_CLUSTER_NAME ?= test-kind

.PHONY: install-kubectl
install-kubectl: ## Install kubectl if not present
	@if ! command -v kubectl >/dev/null 2>&1; then \
		echo "kubectl not found, installing..."; \
		curl -LO "https://dl.k8s.io/release/$$(curl -Ls https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"; \
		chmod +x kubectl; \
		sudo mv kubectl /usr/local/bin/; \
	else \
		echo "kubectl is already installed."; \
	fi

.PHONY: install-kind
install-kind: ## Install kind if not present
	@if ! command -v kind >/dev/null 2>&1; then \
		echo "kind not found, installing..."; \
		curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64; \
		chmod +x ./kind; \
		sudo mv ./kind /usr/local/bin/; \
	else \
		echo "kind is already installed."; \
	fi

.PHONY: install-gnmic
install-gnmic: ## Install gnmic if not present
	@if ! command -v gnmic >/dev/null 2>&1; then \
		echo "gnmic not found, installing..."; \
		bash -c "$$(curl -sL https://get-gnmic.openconfig.net)" -- -v $(GNMIC_VERSION); \
		echo "Adding gnmic to PATH"; \
		echo "PATH: $$PATH"; \
		if [ -f $$HOME/bin/gnmic ]; then \
			export PATH="$$HOME/bin:$$PATH"; \
		elif [ -f /usr/local/bin/gnmic ]; then \
			export PATH="/usr/local/bin:$$PATH"; \
		fi; \
		gnmic version || echo "gnmic not found in PATH after install"; \
	else \
		echo "gnmic is already installed."; \
	fi

.PHONY: install-containerlab
install-containerlab: ## Install containerlab if not present
	@if ! command -v containerlab >/dev/null 2>&1; then \
		echo "containerlab not found, installing..."; \
		bash -c "$(curl -sL https://get.containerlab.dev)" -- -v $(CLAB_VERSION); \
		echo "Adding containerlab to PATH"; \
		echo "PATH: $$PATH"; \
		ls -l $$HOME/bin; \
		ls -l /usr/local/bin; \
		if [ -f $$HOME/bin/containerlab ]; then \
			export PATH="$$HOME/bin:$$PATH"; \
		elif [ -f /usr/local/bin/containerlab ]; then \
			export PATH="/usr/local/bin:$$PATH"; \
		fi; \
		sudo containerlab version; \
	else \
		echo "containerlab is already installed."; \
	fi

.PHONY: deploy-test-cluster
deploy-test-cluster: ## Deploy a kind cluster for testing
	kind create cluster --name $(TEST_CLUSTER_NAME)

.PHONY: install-test-cluster-dependencies
install-test-cluster-dependencies: ## Install the dependencies for the test cluster
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml
	echo "waiting for cert manager to be ready..."
	kubectl wait --namespace cert-manager --for=condition=Available deployment --all --timeout=180s
	echo "cert manager ready"

.PHONY: undeploy-test-cluster
undeploy-test-cluster: ## Delete a kind cluster for testing
	kind delete cluster --name $(TEST_CLUSTER_NAME) || true

.PHONY: load-test-image
load-test-image: ## Load the test image into the test cluster
	kind load docker-image $(IMG) --name $(TEST_CLUSTER_NAME)

.PHONY: deploy-test-topology
deploy-test-topology: ## Deploy a test topology for testing
	sudo containerlab deploy -t test/integration/t1.clab.yaml -c

.PHONY: undeploy-test-topology
undeploy-test-topology: ## Undeploy a test topology for testing
	sudo containerlab destroy -t test/integration/t1.clab.yaml -c

.PHONY: apply-test-targets
apply-test-targets: ## Apply the test targets for testing
	kubectl apply -f test/integration/resources/targets/profile
	kubectl apply -f test/integration/resources/targets

.PHONY: apply-test-subscriptions
apply-test-subscriptions: ## Apply the test subscriptions for testing
	kubectl apply -f test/integration/resources/subscriptions

.PHONY: apply-test-outputs
apply-test-outputs: ## Apply the test outputs for testing
	kubectl apply -f test/integration/resources/outputs

.PHONY: apply-test-inputs
apply-test-inputs: ## Apply the test inputs for testing
	kubectl apply -f test/integration/resources/inputs

.PHONY: apply-test-processors
apply-test-processors: ## Apply the test processors for testing
	kubectl apply -f test/integration/resources/processors

.PHONY: apply-test-pipelines
apply-test-pipelines: ## Apply the test pipelines for testing
	kubectl apply -f test/integration/resources/pipelines

.PHONY: apply-test-clusters
apply-test-clusters: ## Apply the test clusters for testing
	kubectl apply -f test/integration/resources/clusters

.PHONY: apply-test-resources
apply-test-resources: apply-test-targets apply-test-subscriptions apply-test-outputs apply-test-pipelines apply-test-clusters

