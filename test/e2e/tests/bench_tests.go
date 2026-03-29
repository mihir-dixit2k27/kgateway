//go:build bench

package tests

import (
	"github.com/kgateway-dev/kgateway/v2/test/e2e"
	"github.com/kgateway-dev/kgateway/v2/test/e2e/features/inferencebench"
)

// InferenceBenchSuiteRunner returns a SuiteRunner for inference routing benchmarks.
// The runner is ordered because the baseline test must run first to establish
// the performance floor against which inference scenarios are compared.
func InferenceBenchSuiteRunner() e2e.SuiteRunner {
	runner := e2e.NewSuiteRunner(true) // ordered: baseline first, then inference, streaming, epp-isolation
	runner.Register("InferenceBench", inferencebench.NewInferenceBenchSuite)
	return runner
}
