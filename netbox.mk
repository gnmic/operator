
# Add NetBox instance to Kubernetes 
# Only for development and testing purposes
# is generally vibe coded and will be removed after development
##@ NetBox
NETBOX_CHART ?= netbox/netbox
NETBOX_RELEASE ?= netbox
NETBOX_NAMESPACE ?= netbox
NETBOX_URL ?= http://localhost:8081
NETBOX_TOKEN ?= $(shell kubectl get secret netbox-superuser -n netbox -o jsonpath='{.data.api_token}' | base64 -d || true)
NETBOX_VALUES ?= lab/dev/netbox/netbox-values.yaml
NETBOX_PASSWORD ?=
NB_INIT ?= lab/dev/netbox/initializers


.PHONY: netbox-install
netbox-install: ## Generate NetBox secrets, patch templates, create namespace, and deploy NetBox via Helm
ifndef NETBOX_PASSWORD
	$(error NETBOX_PASSWORD is required. Usage: make netbox-install NETBOX_PASSWORD=yourpassword)
endif
	mkdir -p lab/dev/netbox/secrets
	@echo "Generating NetBox secrets..."
	@PEPPER=$$(openssl rand -hex 32); \
	API_TOKEN=$$(openssl rand -hex 32); \
	echo -e '---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: netbox-peppers\n  namespace: netbox\ndata:\n  peppers.yaml: |-\n    API_TOKEN_PEPPERS:\n      1: '\''$$PEPPER'\''\n' > lab/dev/netbox/secrets/netbox_peppers.yaml; \
	echo -e '---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: netbox-superuser\n  namespace: netbox\ntype: Opaque\nstringData:\n  username: "admin"\n  email: "admin@example.com"\n  password: "$(NETBOX_PASSWORD)"\n  api_token: "$$API_TOKEN"\n' > lab/dev/netbox/secrets/netbox_secret.yaml; \
	sed -i "s|\$$PEPPER|$${PEPPER}|g" lab/dev/netbox/secrets/netbox_peppers.yaml; \
	sed -i "s|\$$API_TOKEN|$${API_TOKEN}|g" lab/dev/netbox/secrets/netbox_secret.yaml
	kubectl create namespace $(NETBOX_NAMESPACE) || true
	kubectl apply -f lab/dev/netbox/secrets/ -n $(NETBOX_NAMESPACE)
	helm repo add netbox https://netbox-community.github.io/netbox-helm 2>/dev/null || true
	helm repo update
	helm upgrade --install $(NETBOX_RELEASE) $(NETBOX_CHART) \
		-n $(NETBOX_NAMESPACE) -f $(NETBOX_VALUES)
	kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=netbox -n $(NETBOX_NAMESPACE) --timeout=600s
	@echo "Make sure NetBox is reachable before 'make netbox-sync' by running: kubectl port-forward svc/netbox 8081:80 -n netbox --address='0.0.0.0' &"

.PHONY: netbox-delete
netbox-delete: ## Uninstall NetBox and delete the namespace
	helm uninstall netbox -n netbox || true
	kubectl delete namespace netbox || true

.PHONY: netbox-sync
netbox-sync: ## Publish initializers data into NetBox via REST API
	@echo "NetBox URL: $(NETBOX_URL)"
	@POD=$$(kubectl -n $(NETBOX_NAMESPACE) get pod -l app.kubernetes.io/name=netbox -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true); \
	if [ -z "$$POD" ]; then echo "Error: no NetBox pod found in namespace $(NETBOX_NAMESPACE). Run make netbox-install first."; exit 1; fi; \
	TOKEN_KEY=$$(kubectl exec -n $(NETBOX_NAMESPACE) $$POD -- python manage.py shell -c "from users.models import Token; print(next((t.key for t in Token.objects.filter(user__username='admin')), ''))" 2>/dev/null | tr -d '\r' | grep -E '^[A-Za-z0-9]+$$' | head -n1); \
	if [ -z "$$TOKEN_KEY" ]; then echo "Error: no admin v2 API token found in NetBox. Create one in NetBox admin and retry."; exit 1; fi; \
	echo "NetBox Token Key: $$TOKEN_KEY"; \
	echo "NetBox Token: $(NETBOX_TOKEN)"; \
	python3 lab/dev/netbox/publish.py $(NETBOX_URL) "nbt_$$TOKEN_KEY.$(NETBOX_TOKEN)" $(NB_INIT) --force
	@echo "NetBox sync complete!"
