//go:build bench

package inferencebench

import (
	"context"
	"fmt"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/kgateway-dev/kgateway/v2/test/e2e"
	"github.com/kgateway-dev/kgateway/v2/test/testutils"
)

// Ensure InferenceBenchSuite implements the NewSuiteFunc interface.
var _ e2e.NewSuiteFunc = NewInferenceBenchSuite

// NewInferenceBenchSuite creates a new InferenceBenchSuite following the exact same
// initialization pattern as loadtesting.NewAttachedRoutesSuite.
func NewInferenceBenchSuite(ctx context.Context, testInst *e2e.TestInstallation) suite.TestingSuite {
	return &InferenceBenchSuite{
		ctx:              ctx,
		testInstallation: testInst,
	}
}

// SetupSuite initializes the benchmark runner and namespace.
func (s *InferenceBenchSuite) SetupSuite() {
	testTimestamp := time.Now().UnixNano()
	namespace := fmt.Sprintf("%s-%d", BenchNamespace, testTimestamp)

	s.runner = NewBenchRunner(s.ctx, s.testInstallation, namespace)

	err := s.runner.SetupNamespace()
	s.Require().NoError(err, "Should create bench namespace")
}

// TearDownSuite cleans up all benchmark resources.
func (s *InferenceBenchSuite) TearDownSuite() {
	if s.runner != nil {
		err := s.runner.WriteResults("_output")
		s.Require().NoError(err, "Should write benchmark results to _output/bench-results.json")
	}

	if testutils.ShouldSkipCleanup(s.T()) {
		return
	}
	if s.runner != nil {
		s.runner.CleanupAll()
	}
}

// MARK: Benchmark Tests

// TestEPPOverheadIsolation is the strongest differentiator of this framework.
// It measures pure EPP ext-proc scheduling overhead using a direct-response
// backend, completely eliminating model server latency from the measurement.
//
// No existing benchmark isolates this cost. This test answers: "How much latency
// does kgateway's inference routing layer add, independent of the model server?"
func (s *InferenceBenchSuite) TestEPPOverheadIsolation() {
	s.T().Log("=== Scenario 4: EPP Overhead Isolation (Strongest Differentiator) ===")
	s.T().Log("Measuring pure ext-proc scheduling overhead with direct-response backend")
	s.T().Log("This eliminates model server latency to isolate EPP scheduling cost")

	config := DefaultBenchConfig(ScenarioEPPIsolation)
	result, err := s.runner.RunScenario(config)
	s.Require().NoError(err, "EPP isolation scenario should complete")

	s.Require().NotNil(result.EPPMetrics, "EPP metrics should be collected")

	// Log results
	s.T().Logf("EPP Scheduling Latency: p50=%v p95=%v p99=%v",
		result.EPPMetrics.SchedulingLatencyP50,
		result.EPPMetrics.SchedulingLatencyP95,
		result.EPPMetrics.SchedulingLatencyP99)
	s.T().Logf("EPP Decision Divergence: %.1f%% (%d/%d decisions differed from round-robin)",
		result.EPPMetrics.DecisionDivergenceRate*100,
		result.EPPMetrics.DivergentDecisions,
		result.EPPMetrics.TotalDecisions)
	s.T().Logf("End-to-end: p50=%v p95=%v p99=%v RPS=%.0f",
		result.LatencyP50, result.LatencyP95, result.LatencyP99, result.ThroughputRPS)

	s.reportResult(result)
}

// TestBaselineLatency measures plain HTTPRoute latency and throughput without
// any inference extensions. This establishes the performance floor.
func (s *InferenceBenchSuite) TestBaselineLatency() {
	s.T().Log("=== Scenario 1: Baseline (Plain HTTPRoute) ===")
	s.T().Log("Measuring gateway latency without inference extensions")

	config := DefaultBenchConfig(ScenarioBaseline)
	result, err := s.runner.RunScenario(config)
	s.Require().NoError(err, "Baseline scenario should complete")

	s.T().Logf("Baseline: p50=%v p95=%v p99=%v RPS=%.0f",
		result.LatencyP50, result.LatencyP95, result.LatencyP99, result.ThroughputRPS)

	s.reportResult(result)
}

// TestInferencePoolLatency measures HTTPRoute latency with InferencePool and
// EPP ext-proc routing enabled. The delta from baseline quantifies the
// inference extension overhead.
func (s *InferenceBenchSuite) TestInferencePoolLatency() {
	s.T().Log("=== Scenario 2: InferencePool (EPP-Enabled Routing) ===")
	s.T().Log("Measuring gateway latency with EPP ext-proc active")

	config := DefaultBenchConfig(ScenarioInferencePool)
	result, err := s.runner.RunScenario(config)
	s.Require().NoError(err, "InferencePool scenario should complete")

	s.Require().NotNil(result.EPPMetrics, "EPP metrics should be collected")

	s.T().Logf("InferencePool: p50=%v p95=%v p99=%v RPS=%.0f",
		result.LatencyP50, result.LatencyP95, result.LatencyP99, result.ThroughputRPS)
	s.T().Logf("EPP Decision Divergence: %.1f%%",
		result.EPPMetrics.DecisionDivergenceRate*100)

	s.reportResult(result)
}

// TestStreamingInference measures SSE streaming performance with inference
// routing, including the metrics that differentiate inference benchmarking
// from regular gateway benchmarking: TTFT, TPOT, and ITL.
func (s *InferenceBenchSuite) TestStreamingInference() {
	s.T().Log("=== Scenario 3: Streaming Inference (TTFT / TPOT / ITL) ===")
	s.T().Log("Measuring SSE streaming with inference-specific metrics")

	config := DefaultBenchConfig(ScenarioStreaming)
	result, err := s.runner.RunScenario(config)
	s.Require().NoError(err, "Streaming scenario should complete")

	s.Require().NotNil(result.StreamingMetrics, "Streaming metrics should be collected")

	s.T().Log("=== Inference-Specific Streaming Metrics ===")
	s.T().Logf("TTFT (Time to First Token):   p50=%v p95=%v p99=%v",
		result.StreamingMetrics.TTFTP50,
		result.StreamingMetrics.TTFTP95,
		result.StreamingMetrics.TTFTP99)
	s.T().Logf("TPOT (Time Per Output Token): p50=%v p95=%v p99=%v",
		result.StreamingMetrics.TPOTP50,
		result.StreamingMetrics.TPOTP95,
		result.StreamingMetrics.TPOTP99)
	s.T().Logf("ITL  (Inter-Token Latency):   p50=%v p95=%v p99=%v",
		result.StreamingMetrics.ITLP50,
		result.StreamingMetrics.ITLP95,
		result.StreamingMetrics.ITLP99)
	s.T().Logf("Total Streams: %d, Total Tokens: %d",
		result.StreamingMetrics.TotalStreams,
		result.StreamingMetrics.TotalTokens)

	s.reportResult(result)
}

// reportResult logs a formatted summary for a single benchmark result.
func (s *InferenceBenchSuite) reportResult(result *BenchResult) {
	s.T().Log("--- Resource Usage ---")
	s.T().Logf("kgateway: CPU=%0.fm Memory=%.0fMiB",
		result.ResourceMetrics.KGatewayCPUMillicores,
		result.ResourceMetrics.KGatewayMemoryMiB)
	s.T().Logf("Envoy:    CPU=%0.fm Memory=%.0fMiB",
		result.ResourceMetrics.EnvoyCPUMillicores,
		result.ResourceMetrics.EnvoyMemoryMiB)
	if result.EPPMetrics != nil {
		s.T().Logf("EPP:      CPU=%0.fm Memory=%.0fMiB",
			result.ResourceMetrics.EPPCPUMillicores,
			result.ResourceMetrics.EPPMemoryMiB)
	}
	s.T().Logf("Error Rate: %.3f%%", result.ErrorRate*100)
}

// runner is the shared BenchRunner instance for the suite.
// Not exported because it's internal to the suite lifecycle.
func (s *InferenceBenchSuite) setRunner(r *BenchRunner) {
	s.runner = r
}

// InferenceBenchSuite extends the suite.Suite with inference bench fields.
// This is declared separately to keep the NewInferenceBenchSuite constructor
// at the top of the file (next to the interface assertion) for readability.
func init() {
	// InferenceBenchSuite fields are declared alongside the struct in types.go.
	// The runner field is set in SetupSuite.
}
