# gNMIc Operator

[![Build Status](https://github.com/gnmic/operator/actions/workflows/ci.yaml/badge.svg)](https://github.com/gnmic/operator/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/gnmic/operator)](https://goreportcard.com/report/github.com/gnmic/operator)
[![github release](https://img.shields.io/github/release/gnmic/operator.svg?style=flat-square&color=00c9ff&labelColor=bec8d2)](https://github.com/gnmic/operator/releases/)
[![Doc](https://img.shields.io/badge/Docs-operator.gnmic.dev-blue?style=flat-square&color=00c9ff&labelColor=bec8d2)](https://operator.gnmic.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

A Kubernetes operator for deploying and managing [gNMIc](https://gnmic.openconfig.net) telemetry collectors at scale.

## Overview

gNMIc Operator automates the deployment, configuration, and lifecycle management of gNMIc collectors on Kubernetes. Define your telemetry infrastructure as Custom Resources and let the operator handle StatefulSet creation, configuration generation, target distribution, and dynamic updates.

## Features

- **Declarative Configuration** - Define targets, subscriptions, outputs, and pipelines as Kubernetes resources
- **Horizontal Scaling** - Scale collectors by adjusting replica counts; targets automatically redistribute
- **Dynamic Updates** - Configuration changes apply without pod restarts via gNMIc's REST API
- **Target Distribution** - Bounded-load rendezvous hashing ensures even distribution across pods
- **gRPC Tunnel Support** - Accept connections from devices behind NAT/firewalls
- **TLS/mTLS** - Secure communication with cert-manager integration
- **Multiple Pipelines** - Route different telemetry flows to different destinations

## Quick Start

### Prerequisites

- Kubernetes cluster (v1.25+)
- kubectl configured
- [cert-manager](https://cert-manager.io/)

### Install

Quick install (recommended)

```bash
kubectl apply -f https://github.com/gnmic/operator/releases/download/v0.1.0/install.yaml
```

Or using Helm

```bash
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator --version 0.1.0
```

Or using Kustomize with custom overlay

```bash
cat <<EOF > kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/gnmic/operator/config/default?ref=v0.1.0
images:
  - name: controller
    newName: ghcr.io/gnmic/operator
    newTag: "0.1.0"
EOF
kubectl apply -k .
```

### Deploy a Collector

```yaml
# 1. Create a Cluster
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: telemetry
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
---
# 2. Define a Target
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: router1
  labels:
    role: core
spec:
  address: 10.0.0.1:57400
  profile: default-profile
---
# 3. Create a Subscription
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interfaces
spec:
  paths:
    - /interfaces/interface/state/counters
  mode: STREAM/SAMPLE
  sampleInterval: 10s
---
# 4. Configure an Output
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus
spec:
  type: prometheus
  config:
    listen: ":9804"
    path: /metrics
---
# 5. Wire everything with a Pipeline
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: core-telemetry
spec:
  clusterRef: telemetry
  enabled: true
  targetSelectors:
    - matchLabels:
        role: core
  subscriptionRefs:
    - interfaces
  outputs:
    outputRefs:
      - prometheus
```

## Custom Resources

| CRD | Description |
|-----|-------------|
| **Cluster** | gNMIc collector deployment (StatefulSet, Services, ConfigMap) |
| **Pipeline** | Connects targets, subscriptions, and outputs together |
| **Target** | Network device to collect telemetry from |
| **TargetSource** | Dynamic target discovery (HTTP, Consul, ConfigMap, K8s) |
| **TargetProfile** | Shared credentials and connection settings |
| **TunnelTargetPolicy** | Matching rules for gRPC tunnel-connected devices |
| **Subscription** | gNMI subscription configuration (paths, mode, interval) |
| **Output** | Telemetry destination (Prometheus, Kafka, InfluxDB, etc.) |
| **Input** | External data source (Kafka, NATS consumers) |
| **Processor** | Data transformation (add tags, filter, convert) |

## TLS Configuration

Enable secure communication between the controller and the cluster pods (with cert-manager):

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: secure-cluster
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  api:
    tls:
      issuerRef: gnmic-ca-issuer
```

## gRPC Tunnel Mode

For devices that initiate connections to the collector usng [Openconfig gRPC Tunnel](https://github.com/openconfig/grpc-tunnel)

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: tunnel-cluster
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  grpcTunnel:
    port: 57400
    service:
      type: LoadBalancer
```

## Documentation

Full documentation available at: **[operator.gnmic.dev](https://operator.gnmic.dev)**

- [Installation Guide](docs/content/docs/getting-started/installation.md)
- [User Guide](docs/content/docs/user-guide/)
- [API Reference](docs/content/docs/reference/api.md)
- [Design Documents](design/)

## Development

```bash
# Clone the repository
git clone https://github.com/gnmic/operator.git
cd gnmic-operator

# Install CRDs
make install

# Run the operator locally
make run

# Build and push image
make docker-build docker-push IMG=<your-registry>/gnmic-operator:tag

# Deploy to cluster
make deploy IMG=<your-registry>/gnmic-operator:tag
```

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.

## Related Projects

- [gNMIc](https://gnmic.openconfig.net) - gNMI CLI client and collector
- [OpenConfig](https://openconfig.net) - Vendor-neutral network configuration models
