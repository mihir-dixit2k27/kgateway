# Inference Routing Benchmarking Framework

This document describes the kgateway inference routing benchmarking framework,
which measures the performance impact of the Gateway API Inference Extension (GIE)
on kgateway's data plane.

## Overview

kgateway integrates the Gateway API Inference Extension to enable model-aware,
EPP-driven routing of Generative AI workloads. This benchmarking framework
measures the latency, throughput, and resource overhead introduced by these
inference routing extensions compared to plain gateway routing.

**Key differentiator:** This framework includes an EPP Overhead Isolation
scenario that measures pure EPP scheduling cost independently of model server
latency — a measurement no existing benchmark provides.

## Quick Start

### Prerequisites

- A running Kubernetes cluster with kgateway installed
- `kubectl` configured for the target cluster
- Go 1.22+

### Running Benchmarks Locally

```bash
# Create a kind cluster (if you don't have one)
./hack/kind/setup-kind.sh

# Install kgateway
VERSION=v1.0.0-ci1 CLUSTER_NAME=kind make kind-build-and-load

# Run all inference benchmarks
go test -tags e2e -timeout 30m \
  -run '^TestInferenceBench$$' \
  ./test/e2e/tests/... -v

# Generate a markdown report from results
go run ./hack/benchreport/main.go _output/bench-results.json
```

### Running via Make Targets

```bash
make run-inference-bench    # Run all benchmarks
make bench-report           # Generate markdown report
```

## Scenario Reference

### Scenario 1: Baseline (Plain HTTPRoute)

```
Client (wrk2) -> Gateway (Envoy) -> mock-model-server
```

- No inference extension, no ext-proc filter
- Establishes the latency and throughput floor
- All other scenarios are compared against this baseline

### Scenario 2: InferencePool (EPP-Enabled Routing)

```
Client (wrk2) -> Gateway (Envoy) -> ext-proc -> mock-epp-server
                                 -> mock-model-server (selected by EPP)
```

- Enables the GIE plugin with `inferenceExtension.enabled: true`
- `InferencePool` CR bound to mock-epp-server
- Measures end-to-end overhead with EPP scheduling in the path
- Tracks EPP decision divergence from round-robin

### Scenario 3: Streaming Inference (TTFT / TPOT / ITL)

```
Client (Go SSE streamer) -> Gateway (Envoy) -> ext-proc -> mock-epp-server
                                             -> mock-model-server (SSE stream)
```

- Mock server returns `text/event-stream` with configurable tokens
- Client records timestamp of each SSE event
- Computes inference-specific streaming metrics

### Scenario 4: EPP Overhead Isolation (Strongest Differentiator)

```
Client (wrk2) -> Gateway (Envoy) -> ext-proc -> mock-epp-server
                                 -> direct-response (200 OK)
```

- **No model server** — uses Envoy direct-response to eliminate backend latency
- Pure measurement of EPP scheduling overhead (ext-proc roundtrip)
- Compared against same direct-response route without ext-proc
- Answers: "How much latency does kgateway's inference layer add?"

## Metrics Reference

### Standard Gateway Metrics

| Metric | Unit | Source |
|---|---|---|
| End-to-end latency (p50/p95/p99) | milliseconds | wrk2 client-side |
| Throughput | RPS | wrk2 client-side |
| Error rate | percentage | wrk2 client-side |
| CPU usage | millicores | container metrics |
| Memory usage | MiB | container metrics |

### Inference-Specific Metrics

| Metric | Unit | Description |
|---|---|---|
| TTFT (Time to First Token) | milliseconds | Time from request send to first SSE event |
| TPOT (Time Per Output Token) | milliseconds | Average time between consecutive tokens |
| ITL (Inter-Token Latency) | milliseconds | p50/p95/p99 of inter-token gaps |
| EPP scheduling latency | milliseconds | Pure ext-proc roundtrip time |
| EPP decision divergence | percentage | How often EPP differs from round-robin |

### Understanding EPP Decision Divergence

EPP Decision Divergence measures how often the EPP scheduler chose a different
endpoint than simple round-robin would have chosen. This metric answers a
fundamental question: **"Is the EPP doing useful work?"**

- **0% divergence:** EPP always agrees with round-robin (no scheduling benefit)
- **30-50% divergence:** EPP is actively making different scheduling decisions
- **>70% divergence:** EPP has significantly different scheduling logic

## Load Generator: wrk2

This framework uses **wrk2** for all non-streaming scenarios because it employs
an open-loop (constant arrival rate) load model, which prevents coordinated
omission:

- **wrk2 (open-loop):** Sends requests at a fixed target rate regardless of
  response time. When EPP adds latency, requests queue up, and tail latency
  is accurately captured.
- **hey (closed-loop):** Maintains fixed concurrency, so when EPP adds latency,
  the effective sending rate drops, masking the queuing effect.

For inference benchmarking, accurate p99 measurement is critical because EPP
scheduling decisions can occasionally take longer (e.g., when evaluating model
server load), and closed-loop generators hide this.

## Extending the Framework

### Adding a New Scenario

1. Add a new `BenchScenario` constant in `types.go`
2. Add a default config in `DefaultBenchConfig()`
3. Implement the test method in `suite.go`
4. Add any required testdata manifests
5. Update the report generator if new metric types are needed

### Adding New Metrics

1. Add fields to the appropriate metrics struct in `types.go`
2. Implement collection in `metrics_collector.go`
3. Add display logic in `hack/benchreport/main.go`

## Results Interpretation

### What "Good" Numbers Look Like

| Metric | Good | Acceptable | Investigate |
|---|---|---|---|
| EPP scheduling p99 | < 5 ms | < 15 ms | > 15 ms |
| InferencePool p99 delta vs baseline | < 10 ms | < 25 ms | > 25 ms |
| TTFT p99 | < 50 ms | < 100 ms | > 100 ms |
| EPP decision divergence | > 20% | > 5% | < 5% |
| Error rate | < 0.1% | < 1% | > 1% |

### Regression Detection

When running in CI, the framework can compare results against a stored baseline:

```bash
go run ./hack/benchreport/main.go _output/bench-results.json baseline.json
```

Regressions are flagged with visual indicators:
- ✅ Delta < 10%
- ⚠️ Delta 10-25%
- 🔴 Delta > 25%

## Benchmark Methodology

### Design Rationale

1. **Mock servers only:** No real GPU or LLM required, making benchmarks
   reproducible in CI on any cluster
2. **Open-loop load generation:** wrk2 prevents coordinated omission for
   accurate tail latency measurement
3. **EPP isolation:** Direct-response backends eliminate model server
   confounding for pure overhead measurement
4. **Decision tracking:** EPP decision divergence quantifies scheduling
   value, not just cost
5. **Extends existing framework:** Reuses kgateway's e2e test infrastructure
   (`TestInstallation`, `LoadTestManager`, `NewSuiteFunc` pattern)
