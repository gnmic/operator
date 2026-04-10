
# Add NetBox instance to Kubernetes 
# Only for development and testing purposes
# is generally vibe coded and will be removed after development
##@ NetBox
NETBOX_CLUSTER_NAME ?=
NETBOX_CHART ?= netbox/netbox
NETBOX_RELEASE ?= netbox
NETBOX_NAMESPACE ?= netbox
NETBOX_PORT ?= 8081
NETBOX_URL ?= http://localhost:$(NETBOX_PORT)
NETBOX_TOKEN ?= $(shell kubectl get secret netbox-superuser -n $(NETBOX_NAMESPACE) --context kind-$(NETBOX_CLUSTER_NAME) -o jsonpath='{.data.api_token}' | base64 -d || true)
NETBOX_VALUES ?= lab/dev/netbox/netbox-values.yaml
NETBOX_PASSWORD ?=
NETBOX_INIT ?= lab/dev/netbox/initializers

.PHONY: netbox-setup
netbox-setup: netbox-deploy-cluster netbox-install

.PHONY: netbox-deploy-cluster
netbox-deploy-cluster: ## Deploy the netbox cluster
ifndef NETBOX_CLUSTER_NAME
	$(error NETBOX_CLUSTER_NAME is required. Usage: make netbox-deploy-cluster NETBOX_CLUSTER_NAME=cluster-name)
endif
	kind get clusters | grep -q "$(NETBOX_CLUSTER_NAME)" || kind create cluster --name $(NETBOX_CLUSTER_NAME)
	kubectl config use-context kind-$(CLUSTER_NAME)

.PHONY: netbox-undeploy
netbox-undeploy: ## Undeploy the netbox cluster
ifndef NETBOX_CLUSTER_NAME
	$(error NETBOX_CLUSTER_NAME is required. This will delete the cluster!!! Usage: make netbox-undeploy NETBOX_CLUSTER_NAME=cluster-name)
endif
	kind delete cluster --name $(NETBOX_CLUSTER_NAME)

.PHONY: netbox-install
netbox-install: ## Generate NetBox secrets, patch templates, create namespace, and deploy NetBox via Helm
ifndef NETBOX_PASSWORD
	$(error NETBOX_PASSWORD is required. Usage: make netbox-install NETBOX_PASSWORD=yourpassword)
endif
ifndef NETBOX_CLUSTER_NAME
	$(error NETBOX_CLUSTER_NAME is required. Usage: make netbox-install NETBOX_CLUSTER_NAME=cluster-name)
endif
	mkdir -p lab/dev/netbox/secrets
	@echo "Generating NetBox secrets..."
	@PEPPER=$$(openssl rand -hex 32); \
	API_TOKEN=$$(openssl rand -hex 32); \
	echo -e '---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: netbox-peppers\n  namespace: $(NETBOX_NAMESPACE)\ndata:\n  peppers.yaml: |-\n    API_TOKEN_PEPPERS:\n      1: '\''$$PEPPER'\''\n' > lab/dev/netbox/secrets/netbox_peppers.yaml; \
	echo -e '---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: netbox-superuser\n  namespace: $(NETBOX_NAMESPACE)\ntype: Opaque\nstringData:\n  username: "admin"\n  email: "admin@example.com"\n  password: "$(NETBOX_PASSWORD)"\n  api_token: "$$API_TOKEN"\n' > lab/dev/netbox/secrets/netbox_secret.yaml; \
	sed -i "s|\$$PEPPER|$${PEPPER}|g" lab/dev/netbox/secrets/netbox_peppers.yaml; \
	sed -i "s|\$$API_TOKEN|$${API_TOKEN}|g" lab/dev/netbox/secrets/netbox_secret.yaml
	kubectl create namespace $(NETBOX_NAMESPACE) --context kind-$(NETBOX_CLUSTER_NAME) || true
	kubectl apply -f lab/dev/netbox/secrets/ -n $(NETBOX_NAMESPACE) --context kind-$(NETBOX_CLUSTER_NAME)
	helm repo add netbox https://netbox-community.github.io/netbox-helm 2>/dev/null --kube-context kind-$(NETBOX_CLUSTER_NAME) || true
	helm repo update  --kube-context kind-$(NETBOX_CLUSTER_NAME)
	helm upgrade --install $(NETBOX_RELEASE) $(NETBOX_CHART) \
		-n $(NETBOX_NAMESPACE) --kube-context kind-$(NETBOX_CLUSTER_NAME) -f $(NETBOX_VALUES) \
		--wait --timeout 10m
# 	kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=netbox -n $(NETBOX_NAMESPACE) --context kind-$(NETBOX_CLUSTER_NAME) --timeout=600s
	@echo "Make sure NetBox is reachable before 'make netbox-sync-data' by running: kubectl port-forward svc/netbox 8081:80 -n $(NETBOX_NAMESPACE) --context kind-$(NETBOX_CLUSTER_NAME) --address='0.0.0.0' &"

.PHONY: netbox-delete
netbox-delete: ## Uninstall NetBox helm deployment and delete the namespace
ifndef NETBOX_CLUSTER_NAME
	$(error NETBOX_CLUSTER_NAME is required. Usage: make netbox-delete NETBOX_CLUSTER_NAME=cluster-name)
endif
	helm uninstall netbox -n $(NETBOX_NAMESPACE) --kube-context kind-$(NETBOX_CLUSTER_NAME) || true
	kubectl delete namespace $(NETBOX_NAMESPACE) --context kind-$(NETBOX_CLUSTER_NAME) || true

.PHONY: netbox-sync-data
netbox-sync-data: ## Publish initializers data into NetBox via REST API
ifndef NETBOX_CLUSTER_NAME
	$(error NETBOX_CLUSTER_NAME is required. Usage: make netbox-sync-data NETBOX_CLUSTER_NAME=cluster-name)
endif
	@echo "NetBox URL: $(NETBOX_URL)"
	@POD=$$(kubectl -n $(NETBOX_NAMESPACE) --context kind-$(NETBOX_CLUSTER_NAME) get pod -l app.kubernetes.io/name=netbox -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true); \
	if [ -z "$$POD" ]; then echo "Error: no NetBox pod found in namespace $(NETBOX_NAMESPACE). Run make netbox-install first."; exit 1; fi; \
	TOKEN_KEY=$$(kubectl exec -n $(NETBOX_NAMESPACE) --context kind-$(NETBOX_CLUSTER_NAME) $$POD -- python manage.py shell -c "from users.models import Token; print(next((t.key for t in Token.objects.filter(user__username='admin')), ''))" 2>/dev/null | tr -d '\r' | grep -E '^[A-Za-z0-9]+$$' | head -n1); \
	if [ -z "$$TOKEN_KEY" ]; then echo "Error: no admin v2 API token found in NetBox. Create one in NetBox admin and retry."; exit 1; fi; \
	echo "NetBox Token Key: $$TOKEN_KEY"; \
	echo "NetBox Token: $(NETBOX_TOKEN)"; \
	python3 lab/dev/netbox/publish.py $(NETBOX_URL) "nbt_$$TOKEN_KEY.$(NETBOX_TOKEN)" $(NETBOX_INIT) --force
	@echo "NetBox sync complete!"
