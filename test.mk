CLAB_VERSION ?= 0.70.1
KIND_VERSION ?= v0.20.0
GNMIC_VERSION ?= 0.44.1
KUBECTL_VERSION ?= v1.31.0
TEST_CLUSTER_NAME ?= test-kind
CERT_MANAGER_VERSION ?= v1.19.3
NETBOX_TEST_PORT ?= 8082


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
		curl -Lo ./kind https://kind.sigs.k8s.io/dl/$(KIND_VERSION)/kind-linux-amd64; \
		chmod +x ./kind; \
		sudo mv ./kind /usr/local/bin/; \
	else \
		echo "kind is already installed."; \
	fi

.PHONY: install-gnmic
install-gnmic: ## Install gnmic if not present
	@if ! command -v gnmic >/dev/null 2>&1; then \
		echo "gnmic not found, installing..."; \
		bash -c "$$(curl -sSL https://get-gnmic.openconfig.net)" -- -v $(GNMIC_VERSION); \
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
		curl -sSL https://github.com/srl-labs/containerlab/releases/download/v$(CLAB_VERSION)/containerlab_$(CLAB_VERSION)_linux_amd64.tar.gz -o containerlab.tar.gz; \
		tar -xzf containerlab.tar.gz containerlab; \
		chmod +x containerlab; \
		sudo mv containerlab /usr/local/bin/; \
		rm -f containerlab.tar.gz; \
		containerlab version; \
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

.PHONY: wait-test-operator
wait-test-operator: ## Wait for the operator deployment (and thus webhooks) to be ready
	echo "waiting for gnmic-controller-manager to be available..."
	kubectl wait --namespace gnmic-system --for=condition=Available deployment/gnmic-controller-manager --timeout=180s
	echo "gnmic-controller-manager ready"

.PHONY: deploy-test-topology
deploy-test-topology: ## Deploy a test topology for testing
	sudo containerlab deploy -t test/integration/t1.clab.yaml -c

.PHONY: undeploy-test-topology
undeploy-test-topology: ## Undeploy a test topology for testing
	sudo containerlab destroy -t test/integration/t1.clab.yaml -c

.PHONY: deploy-test-http-server
deploy-test-http-server: ## Deploy a test http pod with a static file inventory for testing
	kubectl apply -f test/integration/http/resources/

.PHONY: undeploy-test-http-server
undeploy-test-http-server: ## Undeploy the http pod for testing
	kubectl delete -f test/integration/http/resources/

.PHONY: create-secrets-for-apiserver
create-secrets-for-apiserver: ## Create the secret used to authenticate push-API requests (HMAC signature)
	kubectl create secret generic gnmic-signature --from-literal=signature=1879

.PHONY: send-target-to-apiserver
send-target-to-apiserver: ## POST a target to the push API, authenticated with an HMAC-SHA512 signature over the body
	@SIG_SECRET=$$(kubectl get secret gnmic-signature -o jsonpath='{.data.signature}' | base64 --decode); \
	BODY='[{"address":"clab-t1-leaf2","port":57400,"name":"leaf2","operation":"created","targetProfile":"default","labels":[{"vendor":"nokia_srlinux"},{"role":"leaf"}]}]'; \
	SIGNATURE=$$(printf '%s' "$$BODY" | openssl dgst -sha512 -hmac "$$SIG_SECRET" | awk '{print $$NF}'); \
	kubectl port-forward -n gnmic-system svc/gnmic-controller-manager-api 8082:8082 --address=0.0.0.0 >/dev/null 2>&1 & \
	sleep 3; \
	curl --retry 3 --retry-delay 1 --retry-connrefused -X POST "http://localhost:8082/api/v1/default/target-source/http-ts/applyTargets" \
		-H "Content-Type: application/json" \
		-H "x-hook-signature: $$SIGNATURE" \
		-d "$$BODY"

.PHONY: deploy-test-netbox-instance
deploy-test-netbox-instance: NETBOX_CLUSTER_NAME=$(TEST_CLUSTER_NAME) ## Deploy the test netbox instance for testing
deploy-test-netbox-instance: NETBOX_PASSWORD=Netbox123
deploy-test-netbox-instance: netbox-setup

.PHONY: deploy-test-netbox-topology
deploy-test-netbox-topology: ## Deploy the netbox test topology for testing
	sudo containerlab deploy -t test/integration/netbox/netbox.clab.yaml -c
	kubectl port-forward svc/netbox $(NETBOX_TEST_PORT):80 -n netbox --context kind-$(TEST_CLUSTER_NAME) --address=0.0.0.0 >/dev/null 2>&1 &

.PHONY: sync-netbox-test-data
sync-test-netbox-data: NETBOX_CLUSTER_NAME=$(TEST_CLUSTER_NAME) ## Sync the netbox instance with the test topology for testing
sync-test-netbox-data: NETBOX_URL=http://localhost:$(NETBOX_TEST_PORT)
sync-test-netbox-data: NETBOX_INIT=test/integration/netbox/initializers
sync-test-netbox-data: netbox-sync-data

.PHONY: undeploy-test-netbox-instance
undeploy-test-netbox-instance: NETBOX_CLUSTER_NAME=$(TEST_CLUSTER_NAME) ## Undeploy the netbox instance from the test cluster
undeploy-test-netbox-instance: netbox-delete

.PHONY: undeploy-test-netbox-topology
undeploy-test-netbox-topology: ## Undeploy the netbox test topology for testing
	sudo containerlab destroy -t test/integration/netbox/netbox.clab.yaml -c

.PHONY: apply-test-targetsources
apply-test-targetsources: ## Apply the test targetsources for testing
	kubectl apply -f test/integration/resources/targetsources

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
apply-test-resources: apply-test-targets apply-test-subscriptions apply-test-outputs apply-test-pipelines apply-test-clusters apply-test-targetsources

##@ Integration Suite

# The assertion-driven suite under test/integration/suite, backed by simulated
# devices. It runs on its own kind cluster so it never contends with
# run-integration-tests for a cluster name, a topology, or a port; the two can
# run at the same time.

IT_CLUSTER_NAME ?= gnmic-it
IT_CONTEXT      := kind-$(IT_CLUSTER_NAME)
IT_KUBECTL      := kubectl --context $(IT_CONTEXT)
IT_OPERATOR_IMG ?= gnmic-operator:integration
GNMIGEN_IMAGE   ?= registry.kmrd.dev/gnmic/gnmigen:0.0.0
GNMIC_IMAGE     ?= ghcr.io/openconfig/gnmic:0.46.0
# A second pinned tag, so rollout tests can prove an image change took effect.
GNMIC_IMAGE_ALT ?= ghcr.io/openconfig/gnmic:0.44.0-amd64
IT_SUITE_DIR    := test/integration/suite
IT_TIMEOUT      ?= 30m
IT_PARALLEL     ?= 2

export GNMIGEN_IMAGE
export GNMIC_IMAGE
export GNMIC_IMAGE_ALT

.PHONY: integration-env-up
integration-env-up: install-kind ## Create the integration kind cluster and deploy the operator into it
	@if kind get clusters 2>/dev/null | grep -qx "$(IT_CLUSTER_NAME)"; then \
		echo "kind cluster $(IT_CLUSTER_NAME) already exists"; \
	else \
		kind create cluster --name $(IT_CLUSTER_NAME); \
	fi
	$(IT_KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml
	@echo "waiting for cert-manager..."
	$(IT_KUBECTL) wait --namespace cert-manager --for=condition=Available deployment --all --timeout=180s
	$(MAKE) integration-images
	$(MAKE) integration-deploy-operator

.PHONY: integration-images
integration-images: ## Build the operator image and load it, gnmi-gen and gnmic into the integration cluster
	$(MAKE) docker-build IMG=$(IT_OPERATOR_IMG)
	kind load docker-image $(IT_OPERATOR_IMG) --name $(IT_CLUSTER_NAME)
	@for img in $(GNMIGEN_IMAGE) $(GNMIC_IMAGE) $(GNMIC_IMAGE_ALT); do \
		docker image inspect $$img >/dev/null 2>&1 || docker pull $$img; \
		kind load docker-image $$img --name $(IT_CLUSTER_NAME); \
	done

# `make deploy` runs `kustomize edit set image`, which rewrites
# config/manager/kustomization.yaml in place. Restore it so running the
# integration suite never leaves a diff in the working tree.
define it_deploy
	$(MAKE) deploy IMG=$(IT_OPERATOR_IMG) KUBECTL="$(IT_KUBECTL)"; \
	status=$$?; \
	git checkout -- config/manager/kustomization.yaml 2>/dev/null || true; \
	exit $$status
endef

.PHONY: integration-deploy-operator
integration-deploy-operator: ## Deploy or redeploy the operator into the integration cluster
	@$(it_deploy)
	@echo "waiting for the operator to be available..."
	$(IT_KUBECTL) wait --namespace gnmic-system --for=condition=Available deployment/gnmic-controller-manager --timeout=180s

.PHONY: integration-env-refresh
integration-env-refresh: ## Rebuild the operator image and restart it, without recreating the cluster
	$(MAKE) integration-images
	@$(it_deploy)
	$(IT_KUBECTL) -n gnmic-system rollout restart deployment/gnmic-controller-manager
	$(IT_KUBECTL) -n gnmic-system rollout status deployment/gnmic-controller-manager --timeout=180s

.PHONY: integration-env-down
integration-env-down: ## Delete the integration kind cluster
	kind delete cluster --name $(IT_CLUSTER_NAME) || true

.PHONY: integration-env-check
integration-env-check: ## Fail fast with an actionable message if the integration environment is not ready
	@kind get clusters 2>/dev/null | grep -qx "$(IT_CLUSTER_NAME)" || \
		{ echo "kind cluster $(IT_CLUSTER_NAME) not found. Run: make integration-env-up"; exit 1; }
	@$(IT_KUBECTL) get deployment/gnmic-controller-manager -n gnmic-system >/dev/null 2>&1 || \
		{ echo "operator not deployed in $(IT_CLUSTER_NAME). Run: make integration-env-up"; exit 1; }
	@$(IT_KUBECTL) wait --namespace gnmic-system --for=condition=Available deployment/gnmic-controller-manager --timeout=60s

.PHONY: integration-test
integration-test: integration-env-check ## Run every integration suite
	go test -tags=integration -count=1 -p $(IT_PARALLEL) -timeout $(IT_TIMEOUT) -v ./$(IT_SUITE_DIR)/...

# Run one suite by directory name, e.g. make integration-test-000-spike
.PHONY: integration-test-%
integration-test-%: integration-env-check ## Run a single suite, e.g. make integration-test-001-cluster
	go test -tags=integration -count=1 -timeout $(IT_TIMEOUT) -v ./$(IT_SUITE_DIR)/$*/...

.PHONY: integration-suites
integration-suites: ## List the available integration suites
	@ls -1 $(IT_SUITE_DIR)

# CI entrypoint: create the gnmic-it cluster, run every suite, always tear down.
# Suites share no cluster with run-integration-tests, so the two jobs can run
# in parallel. Serialize suites (-p 1) so namespace teardown cannot race.
# 013-scale is skipped unless RUN_SCALE=1 (its TestMain exits immediately).
.PHONY: run-integration-tests-v2
run-integration-tests-v2: ## Bring up the gnmi-gen suite env, run all suites, tear down
	$(MAKE) integration-env-up
	@status=0; \
	$(MAKE) integration-test IT_PARALLEL=1 || status=$$?; \
	$(MAKE) integration-env-down; \
	exit $$status

# Nightly / local-only fleet suite. Not wired into CI.
# SCALE_TARGETS defaults to 200; override e.g. SCALE_TARGETS=50 make integration-test-scale
SCALE_TARGETS ?= 200
.PHONY: integration-test-scale
integration-test-scale: integration-env-check ## Run 013-scale (sets RUN_SCALE=1)
	RUN_SCALE=1 SCALE_TARGETS=$(SCALE_TARGETS) go test -tags=integration -count=1 -timeout 45m -v ./$(IT_SUITE_DIR)/013-scale/...

