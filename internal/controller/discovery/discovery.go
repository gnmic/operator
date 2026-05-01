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
// - registry: key -> channel registry.
//
// At the moment, the targetsource controller imports specific subpackages explicitly.
