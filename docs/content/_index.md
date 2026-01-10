---
title: "gNMIc Operator"
linkTitle: "Home"
---

{{< blocks/cover image_anchor="top" height="min" >}}
<h1 class="hero-title">
  {{< svg "icons/logo.svg" >}}<span class="hero-title-text">Operator</span>
</h1>
<p class="hero-subtitle">Deploy and manage gNMIc telemetry collectors on Kubernetes.</p>

<div class="d-flex flex-column flex-md-row justify-content-center align-items-center gap-3 mt-4">
  <a class="btn btn-lg btn-primary" href="{{< relref "/docs" >}}">
    Get Started <i class="fas fa-arrow-alt-circle-right ms-2"></i>
  </a>
  <a class="btn btn-lg btn-secondary" href="https://github.com/karimra/gnmic-operator">
    GitHub <i class="fab fa-github ms-2"></i>
  </a>
</div>

<p class="mt-4 mb-0 hero-kicker">
  Declarative telemetry • Automatic target distribution • Kubernetes-native
</p>
{{< /blocks/cover >}}

{{% blocks/lead color="primary" %}}
**gNMIc Operator** automates the deployment, configuration, and lifecycle of
[gNMIc](https://gnmic.openconfig.net) telemetry collectors on Kubernetes.

Describe your telemetry intent using Kubernetes Custom Resources and let the operator compute and apply the effective gNMIc configuration
to the right collector pods.
{{% /blocks/lead %}}

{{% blocks/section color="light" type="row" %}}
{{% blocks/feature icon="fa-rocket" title="Install" %}}
Install the operator in minutes using Helm or manifests.

[Get started →]({{< relref "/docs/getting-started/installation" >}})
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-book" title="Learn the Concepts" %}}
Understand the model: Cluster, Target, Subscription, Output, Processor, Pipeline.

[Read the docs →]({{< relref "/docs/concepts" >}})
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-code" title="Examples" %}}
Copy/paste working examples for Prometheus, Kafka, NATS, and more.

[View examples →]({{< relref "/docs/examples" >}})
{{% /blocks/feature %}}

{{% /blocks/section %}}

{{% blocks/section color="dark" type="row" %}}
{{% blocks/feature icon="fa-cubes" title="Declarative Configuration" %}}
Define targets, subscriptions, and outputs as Kubernetes resources.
The operator computes and applies the effective gNMIc configuration automatically.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-arrows-alt" title="Horizontal Scaling" %}}
Scale collectors by changing replica counts. Targets are deterministically distributed across pods using bounded-load [rendezvous hashing](https://en.wikipedia.org/wiki/Rendezvous_hashing).
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-sync" title="Hot Updates" %}}
Update configuration without restarts. The operator uses gNMIc’s REST API to apply changes safely.
{{% /blocks/feature %}}
{{% /blocks/section %}}


<!-- {{% blocks/section %}}
## How it works

1. **Create a Cluster**: Deploy a gNMIc StatefulSet and Services via a `Cluster` resource.
2. **Define building blocks**: Create `Target`, `Subscription`, `Output`, and `Processor` resources.
3. **Wire with Pipelines**: Use `Pipeline` resources to associate targets ↔ subscriptions ↔ outputs.
4. **Reconcile & apply**: The operator computes a plan and configures each gNMIc pod with the right assigned targets.

### Why this approach?
- **GitOps-friendly**: your telemetry infrastructure lives in YAML.
- **Safe scaling**: deterministic target placement reduces churn.
- **Fast iteration**: change subscriptions/outputs and apply instantly.

{{% /blocks/section %}} -->
