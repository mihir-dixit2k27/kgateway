package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// BenchReport mirrors the test framework's BenchReport structure.
type BenchReport struct {
	Metadata BenchMetadata `json:"metadata"`
	Results  []BenchResult `json:"results"`
}

// BenchMetadata provides context about the benchmark run.
type BenchMetadata struct {
	KgatewayVersion string    `json:"kgatewayVersion"`
	ClusterType     string    `json:"clusterType"`
	RunTimestamp     time.Time `json:"runTimestamp"`
	GitSHA          string    `json:"gitSHA,omitempty"`
}

// BenchResult captures aggregated metrics for one benchmark run.
type BenchResult struct {
	Scenario         string           `json:"scenario"`
	Timestamp        time.Time        `json:"timestamp"`
	LatencyP50       time.Duration    `json:"latencyP50"`
	LatencyP95       time.Duration    `json:"latencyP95"`
	LatencyP99       time.Duration    `json:"latencyP99"`
	ThroughputRPS    float64          `json:"throughputRPS"`
	ErrorRate        float64          `json:"errorRate"`
	ResourceMetrics  ResourceMetrics  `json:"resourceMetrics"`
	EPPMetrics       *EPPMetrics      `json:"eppMetrics,omitempty"`
	StreamingMetrics *StreamingMetrics `json:"streamingMetrics,omitempty"`
}

// ResourceMetrics captures CPU and memory usage.
type ResourceMetrics struct {
	KGatewayCPUMillicores float64 `json:"kgatewayCPUMillicores"`
	KGatewayMemoryMiB     float64 `json:"kgatewayMemoryMiB"`
	EnvoyCPUMillicores    float64 `json:"envoyCPUMillicores"`
	EnvoyMemoryMiB        float64 `json:"envoyMemoryMiB"`
	EPPCPUMillicores      float64 `json:"eppCPUMillicores,omitempty"`
	EPPMemoryMiB          float64 `json:"eppMemoryMiB,omitempty"`
}

// EPPMetrics captures EPP-specific data.
type EPPMetrics struct {
	SchedulingLatencyP50   time.Duration `json:"schedulingLatencyP50"`
	SchedulingLatencyP95   time.Duration `json:"schedulingLatencyP95"`
	SchedulingLatencyP99   time.Duration `json:"schedulingLatencyP99"`
	DecisionDivergenceRate float64       `json:"decisionDivergenceRate"`
	TotalDecisions         int64         `json:"totalDecisions"`
	DivergentDecisions     int64         `json:"divergentDecisions"`
}

// StreamingMetrics captures inference-specific streaming data.
type StreamingMetrics struct {
	TTFTP50     time.Duration `json:"ttftP50"`
	TTFTP95     time.Duration `json:"ttftP95"`
	TTFTP99     time.Duration `json:"ttftP99"`
	TPOTP50     time.Duration `json:"tpotP50"`
	TPOTP95     time.Duration `json:"tpotP95"`
	TPOTP99     time.Duration `json:"tpotP99"`
	ITLP50      time.Duration `json:"itlP50"`
	ITLP95      time.Duration `json:"itlP95"`
	ITLP99      time.Duration `json:"itlP99"`
	TotalTokens int64         `json:"totalTokens"`
	TotalStreams int64         `json:"totalStreams"`
}

// CompactResult supports a lightweight JSON format for quick PoW demos:
// [
//   {"scenario":"baseline","p50_ms":3.2,"p95_ms":8.1,"p99_ms":14.5,"rps":4820}
// ]
type CompactResult struct {
	Scenario      string  `json:"scenario"`
	P50Ms         float64 `json:"p50_ms"`
	P95Ms         float64 `json:"p95_ms"`
	P99Ms         float64 `json:"p99_ms"`
	RPS           float64 `json:"rps"`
	ThroughputRPS float64 `json:"throughput_rps"`
	ErrorRate     float64 `json:"error_rate"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: benchreport <results.json> [baseline.json]")
		os.Exit(1)
	}

	resultsPath := os.Args[1]
	report, err := loadReport(resultsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading results: %v\n", err)
		os.Exit(1)
	}

	var baseline *BenchReport
	if len(os.Args) >= 3 {
		baseline, err = loadReport(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load baseline: %v\n", err)
		}
	}

	generateReport(report, baseline)
}

func loadReport(path string) (*BenchReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var compact []CompactResult
		if err := json.Unmarshal(trimmed, &compact); err != nil {
			return nil, fmt.Errorf("failed to parse compact JSON array: %w", err)
		}
		return compactToReport(compact), nil
	}

	var report BenchReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if len(report.Results) == 0 {
		return nil, fmt.Errorf("parsed report has no results")
	}

	return &report, nil
}

func compactToReport(compact []CompactResult) *BenchReport {
	report := &BenchReport{
		Metadata: BenchMetadata{
			KgatewayVersion: "proof-of-work",
			ClusterType:     "local",
			RunTimestamp:    time.Now().UTC(),
		},
		Results: make([]BenchResult, 0, len(compact)),
	}

	for _, c := range compact {
		rps := c.RPS
		if rps == 0 {
			rps = c.ThroughputRPS
		}
		report.Results = append(report.Results, BenchResult{
			Scenario:      c.Scenario,
			LatencyP50:    time.Duration(c.P50Ms * float64(time.Millisecond)),
			LatencyP95:    time.Duration(c.P95Ms * float64(time.Millisecond)),
			LatencyP99:    time.Duration(c.P99Ms * float64(time.Millisecond)),
			ThroughputRPS: rps,
			ErrorRate:     c.ErrorRate,
		})
	}

	return report
}

func generateReport(report *BenchReport, baseline *BenchReport) {
	var sb strings.Builder

	sb.WriteString("## Inference Routing Benchmark Results\n\n")
	sb.WriteString(fmt.Sprintf("**kgateway version:** %s | **Cluster:** %s | **Date:** %s\n\n",
		report.Metadata.KgatewayVersion,
		report.Metadata.ClusterType,
		report.Metadata.RunTimestamp.Format("2006-01-02 15:04:05 UTC")))

	if report.Metadata.GitSHA != "" {
		sb.WriteString(fmt.Sprintf("**Git SHA:** `%s`\n\n", report.Metadata.GitSHA))
	}

	// Latency and throughput table
	sb.WriteString("### Latency and Throughput\n\n")
	sb.WriteString("| Scenario | p50 (ms) | p95 (ms) | p99 (ms) | RPS | Error Rate | CPU (m) | Mem (MiB) |\n")
	sb.WriteString("|:---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|\n")

	for _, r := range report.Results {
		sb.WriteString(fmt.Sprintf("| %s | %.1f | %.1f | %.1f | %.0f | %.3f%% | %.0f | %.0f |\n",
			r.Scenario,
			float64(r.LatencyP50.Microseconds())/1000,
			float64(r.LatencyP95.Microseconds())/1000,
			float64(r.LatencyP99.Microseconds())/1000,
			r.ThroughputRPS,
			r.ErrorRate*100,
			r.ResourceMetrics.KGatewayCPUMillicores+r.ResourceMetrics.EnvoyCPUMillicores+r.ResourceMetrics.EPPCPUMillicores,
			r.ResourceMetrics.KGatewayMemoryMiB+r.ResourceMetrics.EnvoyMemoryMiB+r.ResourceMetrics.EPPMemoryMiB,
		))
	}
	sb.WriteString("\n")

	// EPP metrics section
	hasEPP := false
	for _, r := range report.Results {
		if r.EPPMetrics != nil {
			hasEPP = true
			break
		}
	}

	if hasEPP {
		sb.WriteString("### EPP Scheduling Analysis\n\n")
		sb.WriteString("| Scenario | Sched p50 (ms) | Sched p95 (ms) | Sched p99 (ms) | Divergence | Decisions |\n")
		sb.WriteString("|:---|:---:|:---:|:---:|:---:|:---:|\n")

		for _, r := range report.Results {
			if r.EPPMetrics == nil {
				continue
			}
			sb.WriteString(fmt.Sprintf("| %s | %.1f | %.1f | %.1f | %.1f%% | %d |\n",
				r.Scenario,
				float64(r.EPPMetrics.SchedulingLatencyP50.Microseconds())/1000,
				float64(r.EPPMetrics.SchedulingLatencyP95.Microseconds())/1000,
				float64(r.EPPMetrics.SchedulingLatencyP99.Microseconds())/1000,
				r.EPPMetrics.DecisionDivergenceRate*100,
				r.EPPMetrics.TotalDecisions,
			))
		}
		sb.WriteString("\n")
		sb.WriteString("> **EPP Decision Divergence** measures how often the EPP scheduler chose a different\n")
		sb.WriteString("> endpoint than simple round-robin would have chosen. Higher divergence indicates the\n")
		sb.WriteString("> EPP is making active scheduling decisions based on model server state.\n\n")
	}

	// Streaming metrics section
	hasStreaming := false
	for _, r := range report.Results {
		if r.StreamingMetrics != nil {
			hasStreaming = true
			break
		}
	}

	if hasStreaming {
		sb.WriteString("### Streaming Inference Metrics\n\n")
		sb.WriteString("| Metric | p50 (ms) | p95 (ms) | p99 (ms) |\n")
		sb.WriteString("|:---|:---:|:---:|:---:|\n")

		for _, r := range report.Results {
			if r.StreamingMetrics == nil {
				continue
			}
			sb.WriteString(fmt.Sprintf("| TTFT (Time to First Token) | %.1f | %.1f | %.1f |\n",
				float64(r.StreamingMetrics.TTFTP50.Microseconds())/1000,
				float64(r.StreamingMetrics.TTFTP95.Microseconds())/1000,
				float64(r.StreamingMetrics.TTFTP99.Microseconds())/1000))
			sb.WriteString(fmt.Sprintf("| TPOT (Time Per Output Token) | %.1f | %.1f | %.1f |\n",
				float64(r.StreamingMetrics.TPOTP50.Microseconds())/1000,
				float64(r.StreamingMetrics.TPOTP95.Microseconds())/1000,
				float64(r.StreamingMetrics.TPOTP99.Microseconds())/1000))
			sb.WriteString(fmt.Sprintf("| ITL (Inter-Token Latency) | %.1f | %.1f | %.1f |\n",
				float64(r.StreamingMetrics.ITLP50.Microseconds())/1000,
				float64(r.StreamingMetrics.ITLP95.Microseconds())/1000,
				float64(r.StreamingMetrics.ITLP99.Microseconds())/1000))
			sb.WriteString(fmt.Sprintf("\nStreams: %d | Total Tokens: %d\n\n",
				r.StreamingMetrics.TotalStreams,
				r.StreamingMetrics.TotalTokens))
		}
	}

	// Baseline comparison
	if baseline != nil {
		sb.WriteString("### Regression Comparison\n\n")
		sb.WriteString("| Scenario | p99 Current (ms) | p99 Baseline (ms) | Delta |\n")
		sb.WriteString("|:---|:---:|:---:|:---:|\n")

		baselineMap := make(map[string]BenchResult)
		for _, r := range baseline.Results {
			baselineMap[r.Scenario] = r
		}

		for _, r := range report.Results {
			if br, ok := baselineMap[r.Scenario]; ok {
				currentP99 := float64(r.LatencyP99.Microseconds()) / 1000
				baseP99 := float64(br.LatencyP99.Microseconds()) / 1000
				delta := ((currentP99 - baseP99) / baseP99) * 100
				emoji := "✅"
				if delta > 10 {
					emoji = "⚠️"
				}
				if delta > 25 {
					emoji = "🔴"
				}
				sb.WriteString(fmt.Sprintf("| %s | %.1f | %.1f | %+.1f%% %s |\n",
					r.Scenario, currentP99, baseP99, delta, emoji))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n")
	sb.WriteString("*Generated by `hack/benchreport`. Load generator: wrk2 (open-loop, constant arrival rate).*\n")

	fmt.Print(sb.String())
}
