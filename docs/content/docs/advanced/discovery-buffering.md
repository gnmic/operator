---
title: "Discovery Buffering"
linkTitle: "Discovery Buffering"
weight: 3
description: >
  Configure target discovery message buffering through Go channels
---

## Overview

The target discovery subsystem uses Go channels to communicate between message senders (API server and target loader) and the receiver (message processor). Two parameters control how discovery messages are batched and buffered in the channel, directly affecting throughput, latency, and resource consumption.

## Parameters

### Discovery Chunk Size

**Flag**: `--discovery-chunk-size`  
**Helm Value**: `discovery.chunkSize`  
**Type**: Integer  
**Default**: `100`

**What it does**: The maximum number of targets/events bundled into a single discovery message before sending to the channel buffer.

**Performance Impact**:

| Factor | Larger Chunks | Smaller Chunks |
|--------|---------------|----------------|
| **CPU Overhead** | Lower (fewer messages) | Higher (more messages) |
| **Memory per Batch** | Higher | Lower |
| **Latency** | Higher (batch wait time) | Lower (immediate send) |
| **Channel Throughput** | Better (larger transfers) | Worse (many small transfers) |

**When to Adjust**:

- **Increase** (500-2000): Reduce CPU overhead, maximize channel throughput, when memory is available
- **Decrease** (10-50): Minimize latency, reduce memory per message, when resources are constrained

### Discovery Buffer Size

**Flag**: `--discovery-buffer-size`  
**Helm Value**: `discovery.bufferSize`  
**Type**: Integer  
**Default**: `10`

**What it does**: The buffered channel capacity - maximum number of discovery messages queued in the Go channel before blocking senders.

**Performance Impact**:

| Factor | Larger Buffer | Smaller Buffer |
|--------|---------------|---|
| **Memory Usage** | Higher | Lower |
| **Backpressure Response** | Delayed (absorbs spikes) | Immediate (strict rate limit) |
| **Sender Blocking** | Less frequent | More frequent |
| **Burst Absorption** | Better | Worse |

**When to Adjust**:

- **Increase**: Absorb burst discovery events, prevent sender blocking, when memory permits
- **Decrease**: Apply strict backpressure, minimize memory, for stable/low-frequency changes

## Examples

### Helm Installation
Also see the [Helm reference]({{< relref "../reference/helm-chart" >}}).

```bash
# Default settings
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator

# Custom buffering
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator \
  --set discovery.chunkSize=200 \
  --set discovery.bufferSize=40
```