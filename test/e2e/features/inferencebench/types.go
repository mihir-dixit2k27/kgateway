//go:build bench

package inferencebench

import (
	"context"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/kgateway-dev/kgateway/v2/test/e2e"
)

// BenchScenario defines a named benchmark configuration.
type BenchScenario string

const (
	// ScenarioBaseline measures plain HTTPRoute without inference extensions.
	ScenarioBaseline BenchScenario = "baseline"

	// ScenarioInferencePool measures HTTPRoute with InferencePool + EPP ext-proc routing.
	ScenarioInferencePool BenchScenario = "inference-pool"

	// ScenarioStreaming measures SSE streaming with TTFT/TPOT/ITL metrics.
	ScenarioStreaming BenchScenario = "streaming"

	// ScenarioEPPIsolation measures pure EPP ext-proc overhead using direct-response
	// backends, eliminating model server latency from the measurement.
	ScenarioEPPIsolation BenchScenario = "epp-overhead-isolated"
)

const (
	// BenchNamespace is the namespace where benchmark resources are created.
	BenchNamespace = "kgateway-bench"

	// DefaultConcurrency is the default number of wrk2 connections.
	DefaultConcurrency = 10

	// DefaultDuration is the default benchmark duration.
	DefaultDuration = 30 * time.Second

	// DefaultTargetRPS is the default target requests per second for wrk2.
	DefaultTargetRPS = 1000

	// DefaultBodySize is the default request body size in bytes.
	DefaultBodySize = 256

	// DefaultTokenCount is the default number of tokens for streaming scenarios.
	DefaultTokenCount = 50

	// DefaultTokenDelay is the default inter-token delay for streaming scenarios.
	DefaultTokenDelay = 20 * time.Millisecond

	// DefaultEPPDelay is the default simulated EPP scheduling delay.
	DefaultEPPDelay = 2 * time.Millisecond
)

// InferenceBenchSuite is the top-level test suite for inference routing benchmarks.
type InferenceBenchSuite struct {
	suite.Suite
	ctx              context.Context
	testInstallation *e2e.TestInstallation
	runner           *BenchRunner
}

// BenchConfig drives a single benchmark run.
type BenchConfig struct {
	// Scenario identifies which benchmark scenario to run.
	Scenario BenchScenario

	// Concurrency is the number of parallel wrk2 connections.
	Concurrency int

	// Duration is how long to sustain load.
	Duration time.Duration

	// TargetRPS is the target requests per second for wrk2 open-loop mode.
	TargetRPS int

	// BodySize is the size of the mock request body in bytes.
	BodySize int

	// Streaming enables SSE/chunked responses from the mock model server.
	Streaming bool

	// TokenCount is the number of tokens to stream in SSE mode.
	TokenCount int

	// TokenDelay is the delay between SSE tokens.
	TokenDelay time.Duration

	// EPPDelay is the simulated EPP scheduling latency.
	EPPDelay time.Duration
}

// BenchResult captures aggregated metrics for one benchmark run.
type BenchResult struct {
	// Scenario identifies which benchmark scenario produced this result.
	Scenario BenchScenario `json:"scenario"`

	// Timestamp is when the benchmark was run.
	Timestamp time.Time `json:"timestamp"`

	// Config is the configuration that produced this result.
	Config BenchConfig `json:"config"`

	// LatencyP50 is the 50th percentile end-to-end latency.
	LatencyP50 time.Duration `json:"latencyP50"`

	// LatencyP95 is the 95th percentile end-to-end latency.
	LatencyP95 time.Duration `json:"latencyP95"`

	// LatencyP99 is the 99th percentile end-to-end latency.
	LatencyP99 time.Duration `json:"latencyP99"`

	// ThroughputRPS is the measured requests per second.
	ThroughputRPS float64 `json:"throughputRPS"`

	// ErrorRate is the fraction of requests that returned errors (0.0 to 1.0).
	ErrorRate float64 `json:"errorRate"`

	// ResourceMetrics contains CPU and memory measurements.
	ResourceMetrics ResourceMetrics `json:"resourceMetrics"`

	// EPPMetrics contains EPP-specific measurements. Populated only for inference scenarios.
	EPPMetrics *EPPMetrics `json:"eppMetrics,omitempty"`

	// StreamingMetrics contains TTFT/TPOT/ITL measurements. Populated only for streaming scenarios.
	StreamingMetrics *StreamingMetrics `json:"streamingMetrics,omitempty"`
}

// ResourceMetrics captures CPU and memory usage for each component.
type ResourceMetrics struct {
	// KGatewayCPUMillicores is the kgateway controller pod CPU usage in millicores.
	KGatewayCPUMillicores float64 `json:"kgatewayCPUMillicores"`

	// KGatewayMemoryMiB is the kgateway controller pod memory usage in MiB.
	KGatewayMemoryMiB float64 `json:"kgatewayMemoryMiB"`

	// EnvoyCPUMillicores is the Envoy proxy pod CPU usage in millicores.
	EnvoyCPUMillicores float64 `json:"envoyCPUMillicores"`

	// EnvoyMemoryMiB is the Envoy proxy pod memory usage in MiB.
	EnvoyMemoryMiB float64 `json:"envoyMemoryMiB"`

	// EPPCPUMillicores is the EPP pod CPU usage in millicores.
	EPPCPUMillicores float64 `json:"eppCPUMillicores,omitempty"`

	// EPPMemoryMiB is the EPP pod memory usage in MiB.
	EPPMemoryMiB float64 `json:"eppMemoryMiB,omitempty"`
}

// EPPMetrics captures EPP-specific performance and scheduling data.
type EPPMetrics struct {
	// SchedulingLatencyP50 is the 50th percentile EPP ext-proc roundtrip time.
	SchedulingLatencyP50 time.Duration `json:"schedulingLatencyP50"`

	// SchedulingLatencyP95 is the 95th percentile EPP ext-proc roundtrip time.
	SchedulingLatencyP95 time.Duration `json:"schedulingLatencyP95"`

	// SchedulingLatencyP99 is the 99th percentile EPP ext-proc roundtrip time.
	SchedulingLatencyP99 time.Duration `json:"schedulingLatencyP99"`

	// DecisionDivergenceRate is the fraction of requests where EPP chose
	// a different endpoint than simple round-robin would have chosen.
	// This quantifies whether the EPP scheduling is doing useful work.
	DecisionDivergenceRate float64 `json:"decisionDivergenceRate"`

	// TotalDecisions is the total number of EPP scheduling decisions made.
	TotalDecisions int64 `json:"totalDecisions"`

	// DivergentDecisions is the number of decisions that differed from round-robin.
	DivergentDecisions int64 `json:"divergentDecisions"`
}

// StreamingMetrics captures inference-specific streaming metrics.
type StreamingMetrics struct {
	// TTFTP50 is the 50th percentile Time to First Token.
	TTFTP50 time.Duration `json:"ttftP50"`

	// TTFTP95 is the 95th percentile Time to First Token.
	TTFTP95 time.Duration `json:"ttftP95"`

	// TTFTP99 is the 99th percentile Time to First Token.
	TTFTP99 time.Duration `json:"ttftP99"`

	// TPOTP50 is the 50th percentile Time Per Output Token.
	TPOTP50 time.Duration `json:"tpotP50"`

	// TPOTP95 is the 95th percentile Time Per Output Token.
	TPOTP95 time.Duration `json:"tpotP95"`

	// TPOTP99 is the 99th percentile Time Per Output Token.
	TPOTP99 time.Duration `json:"tpotP99"`

	// ITLP50 is the 50th percentile Inter-Token Latency.
	ITLP50 time.Duration `json:"itlP50"`

	// ITLP95 is the 95th percentile Inter-Token Latency.
	ITLP95 time.Duration `json:"itlP95"`

	// ITLP99 is the 99th percentile Inter-Token Latency.
	ITLP99 time.Duration `json:"itlP99"`

	// TotalTokens is the total number of tokens received across all streams.
	TotalTokens int64 `json:"totalTokens"`

	// TotalStreams is the total number of completed streaming requests.
	TotalStreams int64 `json:"totalStreams"`
}

// BenchReport is the top-level structure serialized to bench-results.json.
type BenchReport struct {
	// Metadata describes the benchmark run environment.
	Metadata BenchMetadata `json:"metadata"`

	// Results contains all scenario results.
	Results []BenchResult `json:"results"`
}

// BenchMetadata provides context about the benchmark run.
type BenchMetadata struct {
	// KgatewayVersion is the kgateway version being benchmarked.
	KgatewayVersion string `json:"kgatewayVersion"`

	// ClusterType describes the cluster (e.g., "kind").
	ClusterType string `json:"clusterType"`

	// RunTimestamp is when the benchmark suite started.
	RunTimestamp time.Time `json:"runTimestamp"`

	// GitSHA is the git commit hash being benchmarked.
	GitSHA string `json:"gitSHA,omitempty"`
}

// DefaultBenchConfig returns a BenchConfig with default values for the given scenario.
func DefaultBenchConfig(scenario BenchScenario) BenchConfig {
	config := BenchConfig{
		Scenario:    scenario,
		Concurrency: DefaultConcurrency,
		Duration:    DefaultDuration,
		TargetRPS:   DefaultTargetRPS,
		BodySize:    DefaultBodySize,
		EPPDelay:    DefaultEPPDelay,
	}

	switch scenario {
	case ScenarioStreaming:
		config.Streaming = true
		config.TokenCount = DefaultTokenCount
		config.TokenDelay = DefaultTokenDelay
		config.TargetRPS = 100 // Lower RPS for streaming (each request is long-lived)
	case ScenarioEPPIsolation:
		config.TargetRPS = 5000 // Higher RPS since no real backend
	}

	return config
}
