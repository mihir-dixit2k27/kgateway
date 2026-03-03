package trafficpolicy

import (
	"testing"

	envoyroutev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kgatewayv1 "github.com/kgateway-dev/kgateway/v2/api/v1alpha1/kgateway"
	"github.com/kgateway-dev/kgateway/v2/pkg/pluginsdk/ir"
)

// ---------------------------------------------------------------------------
// statPrefixIR.Equals() tests
// ---------------------------------------------------------------------------

func TestStatPrefixIREquals(t *testing.T) {
	tests := []struct {
		name     string
		a        *statPrefixIR
		b        *statPrefixIR
		expected bool
	}{
		{
			name:     "both nil are equal",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "nil vs non-nil are not equal",
			a:        nil,
			b:        &statPrefixIR{rawTemplate: "foo"},
			expected: false,
		},
		{
			name:     "non-nil vs nil are not equal",
			a:        &statPrefixIR{rawTemplate: "foo"},
			b:        nil,
			expected: false,
		},
		{
			name:     "same template values are equal",
			a:        &statPrefixIR{rawTemplate: "my_prefix"},
			b:        &statPrefixIR{rawTemplate: "my_prefix"},
			expected: true,
		},
		{
			name:     "different template values are not equal",
			a:        &statPrefixIR{rawTemplate: "prefix_a"},
			b:        &statPrefixIR{rawTemplate: "prefix_b"},
			expected: false,
		},
		{
			name:     "template strings with variables are compared literally",
			a:        &statPrefixIR{rawTemplate: "%NAMESPACE%_%NAME%"},
			b:        &statPrefixIR{rawTemplate: "%NAMESPACE%_%NAME%"},
			expected: true,
		},
		{
			name:     "different variable patterns are not equal",
			a:        &statPrefixIR{rawTemplate: "%NAMESPACE%"},
			b:        &statPrefixIR{rawTemplate: "%NAME%"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.a.Equals(tt.b)
			assert.Equal(t, tt.expected, result)

			// Equals must be symmetric: a.Equals(b) == b.Equals(a)
			reverseResult := tt.b.Equals(tt.a)
			assert.Equal(t, result, reverseResult, "Equals should be symmetric")
		})
	}

	// Reflexivity: x.Equals(x) must be true for non-nil
	t.Run("reflexivity", func(t *testing.T) {
		sp := &statPrefixIR{rawTemplate: "%NAMESPACE%_%NAME%"}
		assert.True(t, sp.Equals(sp))
	})
}

// ---------------------------------------------------------------------------
// constructStatPrefix: raw template stored without substitution
// ---------------------------------------------------------------------------

func TestConstructStatPrefix_RawTemplateStored(t *testing.T) {
	// This test explicitly asserts that NO template substitution happens at
	// construct time. The raw template string must be preserved verbatim,
	// as substitution only happens in handlePerRoutePolicies at apply-time.
	tests := []struct {
		name            string
		spec            kgatewayv1.TrafficPolicySpec
		wantRawTemplate string
		wantNilIR       bool
	}{
		{
			name:      "nil StatPrefix produces nil IR",
			spec:      kgatewayv1.TrafficPolicySpec{},
			wantNilIR: true,
		},
		{
			name: "static value stored verbatim",
			spec: kgatewayv1.TrafficPolicySpec{
				StatPrefix: &kgatewayv1.StatPrefixConfig{Value: "my_service"},
			},
			wantRawTemplate: "my_service",
		},
		{
			name: "NAMESPACE template variable NOT substituted at construct time",
			spec: kgatewayv1.TrafficPolicySpec{
				StatPrefix: &kgatewayv1.StatPrefixConfig{Value: "%NAMESPACE%"},
			},
			// Must remain the raw template, not the actual namespace
			wantRawTemplate: "%NAMESPACE%",
		},
		{
			name: "NAME template variable NOT substituted at construct time",
			spec: kgatewayv1.TrafficPolicySpec{
				StatPrefix: &kgatewayv1.StatPrefixConfig{Value: "%NAME%"},
			},
			wantRawTemplate: "%NAME%",
		},
		{
			name: "combined template variables stored verbatim",
			spec: kgatewayv1.TrafficPolicySpec{
				StatPrefix: &kgatewayv1.StatPrefixConfig{Value: "%NAMESPACE%_%NAME%_%RULE_NAME%"},
			},
			wantRawTemplate: "%NAMESPACE%_%NAME%_%RULE_NAME%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &trafficPolicySpecIr{}
			constructStatPrefix(tt.spec, out)

			if tt.wantNilIR {
				assert.Nil(t, out.statPrefix)
				return
			}

			require.NotNil(t, out.statPrefix)
			assert.Equal(t, tt.wantRawTemplate, out.statPrefix.rawTemplate,
				"raw template should be stored verbatim without substitution")
		})
	}
}

// ---------------------------------------------------------------------------
// handlePerRoutePolicies: apply-time stat_prefix template substitution
// ---------------------------------------------------------------------------

func makeRouteWithAction() *envoyroutev3.Route {
	return &envoyroutev3.Route{
		Action: &envoyroutev3.Route_Route{
			Route: &envoyroutev3.RouteAction{},
		},
	}
}

func makeRouteMatchIR(namespace, name, ruleName string) ir.HttpRouteRuleMatchIR {
	parent := &ir.HttpRouteIR{}
	parent.Namespace = namespace
	parent.Name = name
	return ir.HttpRouteRuleMatchIR{
		Parent: parent,
		Name:   ruleName,
	}
}

func TestApplyForRoute_SetsStatPrefix_Static(t *testing.T) {
	plugin := &trafficPolicyPluginGwPass{}
	policy := &TrafficPolicy{
		spec: trafficPolicySpecIr{
			statPrefix: &statPrefixIR{rawTemplate: "my_static_prefix"},
		},
	}

	pCtx := &ir.RouteContext{
		Policy: policy,
		In:     makeRouteMatchIR("default", "my-route", ""),
	}
	out := makeRouteWithAction()

	require.NoError(t, plugin.ApplyForRoute(pCtx, out))

	ra := out.GetRoute()
	require.NotNil(t, ra)
	assert.Equal(t, "my_static_prefix", ra.StatPrefix)
}

func TestApplyForRoute_SetsStatPrefix_NamespaceAndName(t *testing.T) {
	plugin := &trafficPolicyPluginGwPass{}
	policy := &TrafficPolicy{
		spec: trafficPolicySpecIr{
			statPrefix: &statPrefixIR{rawTemplate: "%NAMESPACE%_%NAME%"},
		},
	}

	pCtx := &ir.RouteContext{
		Policy: policy,
		In:     makeRouteMatchIR("production", "checkout-route", ""),
	}
	out := makeRouteWithAction()

	require.NoError(t, plugin.ApplyForRoute(pCtx, out))

	ra := out.GetRoute()
	require.NotNil(t, ra)
	assert.Equal(t, "production_checkout-route", ra.StatPrefix)
}

func TestApplyForRoute_SetsStatPrefix_AllVariables(t *testing.T) {
	plugin := &trafficPolicyPluginGwPass{}
	policy := &TrafficPolicy{
		spec: trafficPolicySpecIr{
			statPrefix: &statPrefixIR{rawTemplate: "%NAMESPACE%_%NAME%_%RULE_NAME%"},
		},
	}

	pCtx := &ir.RouteContext{
		Policy: policy,
		In:     makeRouteMatchIR("staging", "api-route", "v1-rule"),
	}
	out := makeRouteWithAction()

	require.NoError(t, plugin.ApplyForRoute(pCtx, out))

	ra := out.GetRoute()
	require.NotNil(t, ra)
	assert.Equal(t, "staging_api-route_v1-rule", ra.StatPrefix)
}

func TestApplyForRoute_NilStatPrefix_RouteUnchanged(t *testing.T) {
	plugin := &trafficPolicyPluginGwPass{}
	policy := &TrafficPolicy{
		spec: trafficPolicySpecIr{statPrefix: nil},
	}

	pCtx := &ir.RouteContext{
		Policy: policy,
		In:     makeRouteMatchIR("default", "my-route", ""),
	}
	out := makeRouteWithAction()

	require.NoError(t, plugin.ApplyForRoute(pCtx, out))

	ra := out.GetRoute()
	require.NotNil(t, ra)
	assert.Empty(t, ra.StatPrefix, "stat_prefix should be empty when not configured")
}

func TestApplyForRoute_NilParent_TemplateVariablesUnsubstituted(t *testing.T) {
	// If Parent is nil (e.g., synthetic route with no parent), template variables
	// that reference namespace/name should remain unsubstituted — the raw template
	// should still be applied (no nil dereference).
	plugin := &trafficPolicyPluginGwPass{}
	policy := &TrafficPolicy{
		spec: trafficPolicySpecIr{
			statPrefix: &statPrefixIR{rawTemplate: "static_value"},
		},
	}

	pCtx := &ir.RouteContext{
		Policy: policy,
		In: ir.HttpRouteRuleMatchIR{
			Parent: nil, // no parent
			Name:   "",
		},
	}
	out := makeRouteWithAction()

	require.NoError(t, plugin.ApplyForRoute(pCtx, out))

	ra := out.GetRoute()
	require.NotNil(t, ra)
	// Static value (no template variables) should still be applied
	assert.Equal(t, "static_value", ra.StatPrefix)
}

func TestApplyForRoute_NilAction_StatPrefixSkipped(t *testing.T) {
	// Routes with no action (delegating parent routes) should not panic.
	plugin := &trafficPolicyPluginGwPass{}
	policy := &TrafficPolicy{
		spec: trafficPolicySpecIr{
			statPrefix: &statPrefixIR{rawTemplate: "my_prefix"},
		},
	}

	// route with NO action set (delegation scenario)
	pCtx := &ir.RouteContext{
		Policy: policy,
		In: ir.HttpRouteRuleMatchIR{
			Parent: &ir.HttpRouteIR{},
		},
	}
	out := &envoyroutev3.Route{} // no Action

	require.NoError(t, plugin.ApplyForRoute(pCtx, out))
	// No panic, and since there's no RouteAction, StatPrefix can't be set
	assert.Nil(t, out.GetRoute())
}

// ---------------------------------------------------------------------------
// Validate always returns nil
// ---------------------------------------------------------------------------

func TestStatPrefixIRValidate(t *testing.T) {
	assert.NoError(t, (*statPrefixIR)(nil).Validate())
	assert.NoError(t, (&statPrefixIR{rawTemplate: "prefix"}).Validate())
}

// ---------------------------------------------------------------------------
// TrafficPolicy.Equals and TrafficPolicy.Validate include statPrefix
// ---------------------------------------------------------------------------

func TestTrafficPolicyEquals_StatPrefix(t *testing.T) {
	p1 := &TrafficPolicy{
		spec: trafficPolicySpecIr{
			statPrefix: &statPrefixIR{rawTemplate: "prefix_a"},
		},
	}
	p2 := &TrafficPolicy{
		spec: trafficPolicySpecIr{
			statPrefix: &statPrefixIR{rawTemplate: "prefix_b"},
		},
	}
	p3 := &TrafficPolicy{
		spec: trafficPolicySpecIr{
			statPrefix: &statPrefixIR{rawTemplate: "prefix_a"},
		},
	}

	assert.False(t, p1.Equals(p2), "different stat_prefix templates should not be equal")
	assert.True(t, p1.Equals(p3), "same stat_prefix templates should be equal")
}

func TestTrafficPolicyValidate_StatPrefix(t *testing.T) {
	p := &TrafficPolicy{
		spec: trafficPolicySpecIr{
			statPrefix: &statPrefixIR{rawTemplate: "valid_prefix"},
		},
	}
	assert.NoError(t, p.Validate())
}

// ---------------------------------------------------------------------------
// GRPCRoute rule name (Name field on HttpRouteRuleMatchIR)
// ---------------------------------------------------------------------------

func TestApplyForRoute_RuleNameSubstitution_OnlyWhenSet(t *testing.T) {
	plugin := &trafficPolicyPluginGwPass{}

	t.Run("RULE_NAME substituted when rule name is set", func(t *testing.T) {
		policy := &TrafficPolicy{
			spec: trafficPolicySpecIr{
				statPrefix: &statPrefixIR{rawTemplate: "svc_%RULE_NAME%"},
			},
		}
		pCtx := &ir.RouteContext{
			Policy: policy,
			In: ir.HttpRouteRuleMatchIR{
				Parent: &ir.HttpRouteIR{},
				Name:   "rule-one", // plain string
			},
		}
		out := makeRouteWithAction()
		require.NoError(t, plugin.ApplyForRoute(pCtx, out))
		assert.Equal(t, "svc_rule-one", out.GetRoute().StatPrefix)
	})

	t.Run("RULE_NAME not substituted when rule name is empty — warning is logged", func(t *testing.T) {
		policy := &TrafficPolicy{
			spec: trafficPolicySpecIr{
				statPrefix: &statPrefixIR{rawTemplate: "svc_%RULE_NAME%"},
			},
		}
		pCtx := &ir.RouteContext{
			Policy: policy,
			In: ir.HttpRouteRuleMatchIR{
				Parent: &ir.HttpRouteIR{},
				Name:   "", // no name — token stays, controller logs a warning
			},
		}
		out := makeRouteWithAction()
		require.NoError(t, plugin.ApplyForRoute(pCtx, out))
		// %RULE_NAME% is NOT replaced because Name is empty.
		// The literal token stays in the string and a warning is logged by the controller.
		// Operators should either name their HTTPRoute rules or avoid %RULE_NAME% for unnamed rules.
		assert.Equal(t, "svc_%RULE_NAME%", out.GetRoute().StatPrefix,
			"unresolved token should remain in the stat_prefix string (operator must check logs for warning)")
	})
}
