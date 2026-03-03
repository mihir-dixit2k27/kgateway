package trafficpolicy

import (
	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1/kgateway"
)

// statPrefixIR holds the raw template string for the Envoy route stat_prefix.
//
// Template variables (%NAMESPACE%, %NAME%, %RULE_NAME%) are NOT substituted here;
// substitution happens at apply-time in handlePerRoutePolicies() where full route
// context (namespace, name, rule name) is available via ir.RouteContext.In.Parent.
// This keeps the IR pure and avoids premature context binding.
type statPrefixIR struct {
	rawTemplate string
}

var _ PolicySubIR = &statPrefixIR{}

func (s *statPrefixIR) Equals(other PolicySubIR) bool {
	o, ok := other.(*statPrefixIR)
	if !ok {
		return false
	}
	if s == nil && o == nil {
		return true
	}
	if s == nil || o == nil {
		return false
	}
	return s.rawTemplate == o.rawTemplate
}

// Validate performs validation on the stat prefix IR. Validation of the template
// value format is handled at admission time by the kubebuilder validation markers
// on StatPrefixConfig.Value, so no further validation is needed here.
func (s *statPrefixIR) Validate() error { return nil }

// constructStatPrefix builds the statPrefixIR from the policy CR, storing the
// raw template string without any substitution. Template variables are resolved
// at apply-time when route metadata is available.
func constructStatPrefix(spec kgateway.TrafficPolicySpec, out *trafficPolicySpecIr) {
	if spec.StatPrefix == nil {
		return
	}
	out.statPrefix = &statPrefixIR{
		rawTemplate: spec.StatPrefix.Value,
	}
}
