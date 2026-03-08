package irtranslator

import (
	"testing"

	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyroutev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

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

func TestAddRouteSourceMetadata(t *testing.T) {
	tests := []struct {
		name     string
		in       ir.HttpRouteRuleMatchIR
		expected map[string]string
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
			expected: map[string]string{
				"kind": "HTTPRoute",
				"name": "test-route",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var initialMetadata *envoycorev3.Metadata
			if tt.name == "existing metadata preserved" {
				initialMetadata = &envoycorev3.Metadata{
					FilterMetadata: map[string]*structpb.Struct{
						"existing.key": {
							Fields: map[string]*structpb.Value{
								"foo": structpb.NewStringValue("bar"),
							},
						},
					},
				}
			}

			metadata := addRouteSourceMetadata(tt.in, initialMetadata)

			if tt.expected == nil {
				if metadata != nil && metadata.FilterMetadata != nil {
					_, ok := metadata.FilterMetadata[routeSourceMetadataKey]
					assert.False(t, ok, "expected no route source metadata")
				}
				return
			}

			assert.NotNil(t, metadata)
			assert.NotNil(t, metadata.FilterMetadata)

			structPb, ok := metadata.FilterMetadata[routeSourceMetadataKey]
			assert.True(t, ok, "expected route source metadata key")

			fields := structPb.Fields
			assert.Equal(t, len(tt.expected), len(fields), "unexpected number of fields")

			for k, v := range tt.expected {
				val, ok := fields[k]
				assert.True(t, ok, "missing expected key: %s", k)
				assert.Equal(t, v, val.GetStringValue(), "value mismatch for key: %s", k)
			}

			if tt.name == "existing metadata preserved" {
				existingPb, ok := metadata.FilterMetadata["existing.key"]
				assert.True(t, ok, "expected existing metadata key to be preserved")
				assert.Equal(t, "bar", existingPb.Fields["foo"].GetStringValue(), "existing metadata value mismatch")
			}
		})
	}
}
