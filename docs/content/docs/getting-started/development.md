---
title: "Development Guide"
linkTitle: "Development"
weight: 3
description: >
  Build, test, and deploy the gNMIc Operator from source.
---

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| [Go](https://go.dev/dl/) | 1.25+ | Compile the operator |
| [Docker](https://docs.docker.com/get-docker/) | 20+ | Build container images |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | 1.25+ | Interact with the cluster |
| [Kind](https://kind.sigs.k8s.io/) | 0.20+ | Local Kubernetes cluster |
| [Containerlab](https://containerlab.dev) | 0.54+ | Lab topologies with real network devices |
| [gNMIc CLI](https://gnmic.openconfig.net) | 0.38+ | Configure lab nodes via gNMI Set |

## Repository Layout

```text
.
├── api/v1alpha1/       # CRD type definitions and deepcopy methods
├── cmd/main.go         # Operator entrypoint
├── config/
│   ├── crd/            # Generated CRD manifests
│   ├── default/        # Kustomize overlay that combines everything
│   ├── manager/        # Controller Deployment and Service
│   ├── rbac/           # ClusterRole, ServiceAccount, Bindings
│   ├── webhook/        # Webhook configuration
│   ├── certmanager/    # cert-manager Issuer and Certificate
│   └── samples/        # Example CRs
├── internal/
│   ├── controller/     # Reconcilers (Cluster, Pipeline, TargetState, …)
│   ├── gnmic/          # gNMIc client helpers (config builder, SSE, …)
│   ├── utils/          # Shared utilities
│   └── webhook/        # Admission webhook handlers
├── helm/               # Helm chart
├── lab/dev/            # Development lab (Containerlab topology + operator resources)
├── Dockerfile
└── Makefile
```

## Initial Setup

Clone the repository and run the one-time setup that creates a Kind cluster, installs cert-manager, builds the operator image, loads it, and deploys it:

```bash
git clone https://github.com/gnmic/operator.git
cd operator

# Build the operator image
make docker-build IMG=gnmic-operator:dev

# Create a Kind cluster, install cert-manager, load the image
make setup-dev-cluster IMG=gnmic-operator:dev

# Deploy the operator (CRDs + RBAC + webhooks + controller)
make deploy IMG=gnmic-operator:dev
```

`setup-dev-cluster` is a convenience target that chains:
1. `deploy-dev-cluster` -- creates a Kind cluster (name defaults to `gnmic-dev`, override with `CLUSTER_NAME=`)
2. `install-dev-cluster-dependencies` -- installs cert-manager
3. `load-dev-image` -- loads `$IMG` into the Kind cluster

## Setting Up the Lab

Once the operator is running, deploy the Containerlab topology and apply the operator resources:

```bash
# Deploy the lab, configure nodes via gNMI, and apply operator CRs
make setup-dev-lab
```

`setup-dev-lab` chains:
1. `deploy-dev-lab` -- runs `containerlab deploy` for `lab/dev/3-nodes.clab.yaml`
2. `configure-nodes-dev-lab` -- pushes interface and BGP configs to each node using gNMIc CLI
3. `apply-resources-dev-lab` -- applies Targets, Subscriptions, Outputs, Pipelines, and Clusters

The lab places its management interfaces on the `kind` Docker network so that gNMIc pods running inside Kind can reach the SR Linux nodes by their container names (e.g. `clab-3-nodes-leaf1:57400`).

### Lab Resource Management

You can apply or delete individual resource types:

```bash
make apply-targets-dev-lab
make apply-subscriptions-dev-lab
make apply-outputs-dev-lab
make apply-pipelines-dev-lab
make apply-clusters-dev-lab

make delete-targets-dev-lab
make delete-subscriptions-dev-lab
make delete-outputs-dev-lab
make delete-pipelines-dev-lab
make delete-clusters-dev-lab
```

Or all at once:

```bash
make apply-resources-dev-lab
make delete-resources-dev-lab
```

## Development Loop

Once the initial setup is done, the day-to-day cycle is:

1. Make code changes
2. Rebuild, reload, and redeploy:

```bash
make docker-build IMG=gnmic-operator:dev
make load-dev-image IMG=gnmic-operator:dev
make undeploy
make deploy IMG=gnmic-operator:dev
```

Then watch the controller logs:

```bash
kubectl logs -n gnmic-system deploy/gnmic-controller-manager -f
```

## Teardown

```bash
# Remove the lab topology
make undeploy-dev-lab

# Remove the operator from the cluster
make undeploy

# Delete the Kind cluster entirely
make undeploy-dev-cluster
```

## Makefile Reference

Run `make help` for the full list. Key targets grouped by category:

### Code Generation

```bash
# Regenerate CRD manifests, RBAC ClusterRole, and webhook configs
# from kubebuilder markers in the Go source.
make manifests

# Regenerate DeepCopy / DeepCopyInto methods after modifying *_types.go files.
make generate
```

> Always run `make manifests` after changing kubebuilder markers (`+kubebuilder:rbac`, `+kubebuilder:validation`, etc.) and `make generate` after modifying any type in `api/v1alpha1/`.

### Build

```bash
# Compile the operator binary to ./bin/manager
make build

# Build the container image
make docker-build IMG=gnmic-operator:dev

# Push to a remote registry (not needed for Kind)
make docker-push IMG=gnmic-operator:dev
```

### Deploy

```bash
# Install only the CRDs (no controller)
make install

# Deploy the full operator
make deploy IMG=gnmic-operator:dev

# Remove the operator
make undeploy

# Remove only the CRDs
make uninstall
```

`make deploy` uses Kustomize to render `config/default/` and applies it to whichever cluster `kubectl` is pointing at. The Kustomize overlay:

- Puts everything in the `gnmic-system` namespace
- Prefixes all resource names with `gnmic-`
- Wires up cert-manager certificates for the webhook and controller-to-pod TLS
- Patches the Deployment image to `$IMG`

### Running Locally (without a container)

For faster iteration you can run the operator process directly on your machine, which still talks to the cluster via your kubeconfig:

```bash
make install   # install CRDs first
make run       # blocks; Ctrl-C to stop
```

{{% alert title="Note" %}}
When running locally, admission webhooks won't work because the API server needs to reach the webhook over HTTPS. Either disable webhooks in `config/default/kustomization.yaml` or use `make deploy` for full functionality.
{{% /alert %}}

### Testing and Linting

```bash
make test       # run unit tests with envtest
make lint       # run golangci-lint
make lint-fix   # run golangci-lint with auto-fix
```

### Helm

```bash
make helm-crds      # copy generated CRDs into the Helm chart
make helm-lint      # lint the chart
make helm-template  # render templates locally for debugging
```

## Adding a New CRD

1. Define the types in `api/v1alpha1/<kind>_types.go`
2. Add kubebuilder markers for validation, defaulting, and printer columns
3. Create webhook handlers in `internal/webhook/v1alpha1/<kind>_webhook.go`
4. Run `make manifests generate`
5. Register the webhook/controller in `cmd/main.go`
6. Update RBAC markers on the controller and run `make manifests` again
