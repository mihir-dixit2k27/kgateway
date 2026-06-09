package irtranslator

import (
	"errors"
	"testing"

	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyroutev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/pkg/pluginsdk/ir"
)

func TestValidateWeightedClusters(t *testing.T) {
	tests := []struct {
		name     string
		clusters []*envoyroutev3.WeightedCluster_ClusterWeight
		wantErr  bool
	}{
		{
			name:     "no clusters",
			clusters: []*envoyroutev3.WeightedCluster_ClusterWeight{},
			wantErr:  false,
		},
		{
			name: "single cluster with weight 0",
			clusters: []*envoyroutev3.WeightedCluster_ClusterWeight{
				{
					Weight: wrapperspb.UInt32(0),
				},
			},
			wantErr: true,
		},
		{
			name: "single cluster with weight > 0",
			clusters: []*envoyroutev3.WeightedCluster_ClusterWeight{
				{
					Weight: wrapperspb.UInt32(100),
				},
			},
			wantErr: false,
		},
		{
			name: "multiple clusters all with weight 0",
			clusters: []*envoyroutev3.WeightedCluster_ClusterWeight{
				{
					Weight: wrapperspb.UInt32(0),
				},
				{
					Weight: wrapperspb.UInt32(0),
				},
			},
			wantErr: true,
		},
		{
			name: "multiple clusters with mixed weights",
			clusters: []*envoyroutev3.WeightedCluster_ClusterWeight{
				{
					Weight: wrapperspb.UInt32(0),
				},
				{
					Weight: wrapperspb.UInt32(100),
				},
			},
			wantErr: false,
		},
		{
			name: "multiple clusters all with weight > 0",
			clusters: []*envoyroutev3.WeightedCluster_ClusterWeight{
				{
					Weight: wrapperspb.UInt32(50),
				},
				{
					Weight: wrapperspb.UInt32(50),
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var errs []error
			validateWeightedClusters(tt.clusters, &errs)

			if tt.wantErr {
				assert.Len(t, errs, 1)
				assert.Contains(t, errs[0].Error(), "All backend weights are 0. At least one backendRef in the HTTPRoute rule must specify a non-zero weight")
			} else {
				assert.Len(t, errs, 0)
			}
		})
	}
}

func TestSetEnvoyPathMatcher_PathPrefix(t *testing.T) {
	pathPrefix := gwv1.PathMatchPathPrefix

	tests := []struct {
		name         string
		path         string
		wantPrefix   string
		wantSeparate bool
	}{
		{
			name:         "uses path separated prefix for clean prefix",
			path:         "/foo",
			wantPrefix:   "/foo",
			wantSeparate: true,
		},
		{
			name:         "ignores trailing slash for non root prefix",
			path:         "/foo/",
			wantPrefix:   "/foo",
			wantSeparate: true,
		},
		{
			name:         "keeps root prefix unchanged",
			path:         "/",
			wantPrefix:   "/",
			wantSeparate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &envoyroutev3.RouteMatch{}

			setEnvoyPathMatcher(gwv1.HTTPRouteMatch{
				Path: &gwv1.HTTPPathMatch{
					Type:  &pathPrefix,
					Value: &tt.path,
				},
			}, out)

			if tt.wantSeparate {
				spec, ok := out.PathSpecifier.(*envoyroutev3.RouteMatch_PathSeparatedPrefix)
				assert.True(t, ok)
				assert.Equal(t, tt.wantPrefix, spec.PathSeparatedPrefix)
				return
			}

			spec, ok := out.PathSpecifier.(*envoyroutev3.RouteMatch_Prefix)
			assert.True(t, ok)
			assert.Equal(t, tt.wantPrefix, spec.Prefix)
		})
	}
}

func refFor(name string) *ir.AttachedPolicyRef {
	return &ir.AttachedPolicyRef{
		Group:     "gateway.kgateway.dev",
		Kind:      "TrafficPolicy",
		Namespace: "ns",
		Name:      name,
	}
}

func TestSummarizeRuleErrors_NilReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", summarizeRuleErrors(nil))
}

func TestSummarizeRuleErrors_BareErrorPassesThrough(t *testing.T) {
	got := summarizeRuleErrors(errors.New("plain"))
	assert.Equal(t, "plain", got)
}

func TestSummarizeRuleErrors_AttributedAndSorted(t *testing.T) {
	// Insert in reverse-alphabetical order to verify the formatter sorts.
	errs := []error{
		&ir.PolicyError{Ref: refFor("z-pol"), Err: errors.New("z msg")},
		&ir.PolicyError{Ref: refFor("a-pol"), Err: errors.New("a msg")},
	}
	got := summarizeRuleErrors(errors.Join(errs...))
	want := "gateway.kgateway.dev/TrafficPolicy/ns/a-pol: a msg\n" +
		"gateway.kgateway.dev/TrafficPolicy/ns/z-pol: z msg"
	assert.Equal(t, want, got)
}

func TestSummarizeRuleErrors_DedupesIdenticalEntries(t *testing.T) {
	r := refFor("p")
	errs := []error{
		&ir.PolicyError{Ref: r, Err: errors.New("dup")},
		&ir.PolicyError{Ref: r, Err: errors.New("dup")},
		&ir.PolicyError{Ref: r, Err: errors.New("unique")},
	}
	got := summarizeRuleErrors(errors.Join(errs...))
	want := "gateway.kgateway.dev/TrafficPolicy/ns/p: dup\n" +
		"gateway.kgateway.dev/TrafficPolicy/ns/p: unique"
	assert.Equal(t, want, got)
}

func TestSummarizeRuleErrors_MixedAttributedAndBare(t *testing.T) {
	errs := []error{
		&ir.PolicyError{Ref: refFor("p"), Err: errors.New("attributed")},
		errors.New("bare"),
	}
	got := summarizeRuleErrors(errors.Join(errs...))
	// Bare entry sorts first because its refID is the empty string.
	want := "bare\n" +
		"gateway.kgateway.dev/TrafficPolicy/ns/p: attributed"
	assert.Equal(t, want, got)
}

func TestSummarizeRuleErrors_DistinguishesBySection(t *testing.T) {
	// Same policy ref but two different SectionName values producing the same
	// underlying error must NOT be deduped — they correspond to distinct
	// attachments (e.g. two different Gateway listeners).
	mkRef := func(section string) *ir.AttachedPolicyRef {
		return &ir.AttachedPolicyRef{
			Group:       "gateway.kgateway.dev",
			Kind:        "TrafficPolicy",
			Namespace:   "ns",
			Name:        "p",
			SectionName: section,
		}
	}
	errs := []error{
		&ir.PolicyError{Ref: mkRef("http-b"), Err: errors.New("ext not found")},
		&ir.PolicyError{Ref: mkRef("http-a"), Err: errors.New("ext not found")},
	}
	got := summarizeRuleErrors(errors.Join(errs...))
	want := "gateway.kgateway.dev/TrafficPolicy/ns/p/http-a: ext not found\n" +
		"gateway.kgateway.dev/TrafficPolicy/ns/p/http-b: ext not found"
	assert.Equal(t, want, got)
}

func TestSummarizeRuleErrors_FlattensNestedJoins(t *testing.T) {
	inner := errors.Join(
		&ir.PolicyError{Ref: refFor("a-pol"), Err: errors.New("a")},
		&ir.PolicyError{Ref: refFor("b-pol"), Err: errors.New("b")},
	)
	outer := errors.Join(inner, &ir.PolicyError{Ref: refFor("c-pol"), Err: errors.New("c")})
	got := summarizeRuleErrors(outer)
	want := "gateway.kgateway.dev/TrafficPolicy/ns/a-pol: a\n" +
		"gateway.kgateway.dev/TrafficPolicy/ns/b-pol: b\n" +
		"gateway.kgateway.dev/TrafficPolicy/ns/c-pol: c"
	assert.Equal(t, want, got)
}

func TestAddRouteSourceMetadata(t *testing.T) {
	tests := []struct {
		name            string
		in              ir.HttpRouteRuleMatchIR
		initialMetadata *envoycorev3.Metadata
		expected        map[string]string
		expectPreserved bool
	}{
		{
			name: "full metadata",
			in: ir.HttpRouteRuleMatchIR{
				Name: "test-rule",
				Parent: &ir.HttpRouteIR{
					ObjectSource: ir.ObjectSource{
						Kind:      "HTTPRoute",
						Group:     "gateway.networking.k8s.io",
						Name:      "test-route",
						Namespace: "default",
					},
				},
			},
			expected: map[string]string{
				"kind":      "HTTPRoute",
				"group":     "gateway.networking.k8s.io",
				"name":      "test-route",
				"namespace": "default",
				"rule":      "test-rule",
			},
		},
		{
			name: "missing rule and kind",
			in: ir.HttpRouteRuleMatchIR{
				Parent: &ir.HttpRouteIR{
					ObjectSource: ir.ObjectSource{
						Group:     "gateway.networking.k8s.io",
						Name:      "test-route",
						Namespace: "default",
					},
				},
			},
			expected: map[string]string{
				"group":     "gateway.networking.k8s.io",
				"name":      "test-route",
				"namespace": "default",
			},
		},
		{
			name: "nil parent",
			in: ir.HttpRouteRuleMatchIR{
				Name: "test-rule",
			},
			expected: nil,
		},
		{
			name: "existing metadata preserved",
			in: ir.HttpRouteRuleMatchIR{
				Parent: &ir.HttpRouteIR{
					ObjectSource: ir.ObjectSource{
						Kind: "HTTPRoute",
						Name: "test-route",
					},
				},
			},
			initialMetadata: &envoycorev3.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					"existing.key": {
						Fields: map[string]*structpb.Value{
							"foo": structpb.NewStringValue("bar"),
						},
					},
				},
			},
			expectPreserved: true,
			expected: map[string]string{
				"kind": "HTTPRoute",
				"name": "test-route",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := addRouteSourceMetadata(tt.in, tt.initialMetadata)

			if tt.expected == nil {
				if metadata != nil && metadata.FilterMetadata != nil {
					_, ok := metadata.FilterMetadata[routeSourceMetadataKey]
					assert.False(t, ok, "expected no route source metadata")
				}
				return
			}

			require.NotNil(t, metadata, "metadata should not be nil")
			require.NotNil(t, metadata.FilterMetadata, "filter metadata should not be nil")

			structPb, ok := metadata.FilterMetadata[routeSourceMetadataKey]
			require.True(t, ok, "expected route source metadata key %q", routeSourceMetadataKey)

			fields := structPb.Fields
			assert.Equal(t, len(tt.expected), len(fields), "unexpected number of fields")

			for k, v := range tt.expected {
				val, ok := fields[k]
				assert.True(t, ok, "missing expected key: %s", k)
				assert.Equal(t, v, val.GetStringValue(), "value mismatch for key: %s", k)
			}

			if tt.expectPreserved {
				existingPb, ok := metadata.FilterMetadata["existing.key"]
				require.True(t, ok, "expected existing metadata key to be preserved")
				assert.Equal(t, "bar", existingPb.Fields["foo"].GetStringValue(), "existing metadata value mismatch")
			}
		})
	}
}

func TestRouteSourceMetadataFlag(t *testing.T) {
	in := ir.HttpRouteRuleMatchIR{
		Name: "my-rule",
		Parent: &ir.HttpRouteIR{
			ObjectSource: ir.ObjectSource{
				Kind:      "HTTPRoute",
				Group:     "gateway.networking.k8s.io",
				Name:      "my-route",
				Namespace: "default",
			},
		},
	}

	tests := []struct {
		name        string
		flagEnabled bool
		wantMeta    bool
	}{
		{
			name:        "disabled by default, no route_source metadata",
			flagEnabled: false,
			wantMeta:    false,
		},
		{
			name:        "enabled, route_source metadata is attached",
			flagEnabled: true,
			wantMeta:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &httpRouteConfigurationTranslator{
				pluginPass:                 TranslationPassPlugins{},
				routeSourceMetadataEnabled: tt.flagEnabled,
			}

			// Replicate the flag gate in envoyRoutes() without needing a
			// fully-wired translator (no backends, validator, or GatewayIR required).
			out := h.initRoutes(in, "generated-name")
			if h.routeSourceMetadataEnabled {
				out.Metadata = addRouteSourceMetadata(in, out.GetMetadata())
			}

			if !tt.wantMeta {
				if out.GetMetadata() != nil {
					_, ok := out.GetMetadata().GetFilterMetadata()[routeSourceMetadataKey]
					assert.False(t, ok, "route_source metadata should be absent when the flag is off")
				}
				return
			}

			require.NotNil(t, out.GetMetadata(), "metadata must not be nil when the flag is on")
			srcMeta, ok := out.GetMetadata().GetFilterMetadata()[routeSourceMetadataKey]
			require.True(t, ok, "filter metadata must contain key %q", routeSourceMetadataKey)

			fields := srcMeta.GetFields()
			assert.Equal(t, "HTTPRoute", fields["kind"].GetStringValue())
			assert.Equal(t, "gateway.networking.k8s.io", fields["group"].GetStringValue())
			assert.Equal(t, "my-route", fields["name"].GetStringValue())
			assert.Equal(t, "default", fields["namespace"].GetStringValue())
			assert.Equal(t, "my-rule", fields["rule"].GetStringValue())
		})
	}
}
