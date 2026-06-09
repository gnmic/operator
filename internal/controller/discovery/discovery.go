package discovery

// Package discovery implements the discovery runtime subsystem.
//
// The discovery subsystem is responsible for:
// - Receiving discovery data from external providers (loaders, webhooks).
// - Applying discovered state to Kubernetes Targets.
//
// The package is structured into the following subpackages:
// - core: message contracts, snapshot/event types, and transport helpers.
// - message processor: snapshot + event target state application logic.
// - loaders: target discovery providers (HTTP, webhook, etc.).
// - registry: generic discovery runtime registry.
//
// The package also contains discovery helpers:
// - client helpers for applying/deleting targets and updating TargetSource status.
// - a loader factory for constructing discovery loaders.
// - target normalization and event generation logic.
// - a resource fetcher for resolving Secret/ConfigMap values used by loaders.
//
// At the moment, the targetsource controller imports specific subpackages explicitly.
