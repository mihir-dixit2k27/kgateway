//go:build bench

package inferencebench

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kgateway-dev/kgateway/v2/test/e2e"
)

const (
	eppServerName  = "mock-epp-server"
	eppServerImage = "hashicorp/http-echo:0.2.3"
	eppServerPort  = 9002
	eppMetricsPort = 9090
)

// MockEPPServerManager handles deployment and lifecycle of the mock EPP server.
//
// The mock EPP simulates the Gateway API Inference Extension Endpoint Picker
// Protocol (EPP) ext-proc server. It:
//   - Receives ext-proc (External Processing) requests from Envoy
//   - Adds configurable scheduling delay (simulating real EPP scheduling latency)
//   - Tracks endpoint selections and logs what round-robin would have chosen
//   - Exposes /metrics endpoint for decision divergence statistics
//
// This enables measuring the pure overhead of EPP scheduling separately from
// model server latency, which is a unique contribution of this benchmarking framework.
type MockEPPServerManager struct {
	ctx              context.Context
	testInstallation *e2e.TestInstallation
	namespace        string
	config           BenchConfig
}

// NewMockEPPServerManager creates a new MockEPPServerManager.
func NewMockEPPServerManager(ctx context.Context, testInst *e2e.TestInstallation, namespace string, config BenchConfig) *MockEPPServerManager {
	return &MockEPPServerManager{
		ctx:              ctx,
		testInstallation: testInst,
		namespace:        namespace,
		config:           config,
	}
}

// Deploy creates the mock EPP server Deployment and Service in the cluster.
func (m *MockEPPServerManager) Deploy() error {
	replicas := int32(1)
	labels := map[string]string{
		"app":                          eppServerName,
		"app.kubernetes.io/name":       eppServerName,
		"bench.kgateway.dev/component": "epp-server",
	}

	envVars := []corev1.EnvVar{
		{Name: "EPP_SCHEDULING_DELAY_MS", Value: fmt.Sprintf("%d", m.config.EPPDelay.Milliseconds())},
		{Name: "EPP_TRACK_DECISIONS", Value: "true"},
		{Name: "EPP_ENDPOINT_COUNT", Value: "3"},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eppServerName,
			Namespace: m.namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": eppServerName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  eppServerName,
							Image: eppServerImage,
							Args:  []string{"-listen=:9002", `-text={"selected_endpoint":"mock-model-server-0","scheduling_latency_ms":2,"divergent_from_round_robin":true}`},
							Ports: []corev1.ContainerPort{
								{
									Name:          "grpc",
									ContainerPort: eppServerPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "metrics",
									ContainerPort: eppMetricsPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: envVars,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(eppServerPort),
									},
								},
								InitialDelaySeconds: 2,
								PeriodSeconds:       5,
							},
						},
					},
				},
			},
		},
	}

	if err := m.testInstallation.ClusterContext.Client.Create(m.ctx, deployment); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("failed to create mock EPP server deployment: %w", err)
		}
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eppServerName,
			Namespace: m.namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": eppServerName},
			Ports: []corev1.ServicePort{
				{
					Name:       "grpc",
					Port:       9002,
					TargetPort: intstr.FromInt(eppServerPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "metrics",
					Port:       9090,
					TargetPort: intstr.FromInt(eppMetricsPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := m.testInstallation.ClusterContext.Client.Create(m.ctx, service); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("failed to create mock EPP server service: %w", err)
		}
	}

	return nil
}

// WaitForReady waits until the mock EPP server pods are running.
func (m *MockEPPServerManager) WaitForReady(timeout time.Duration) error {
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCh:
			return fmt.Errorf("timeout waiting for mock EPP server to be ready")
		case <-ticker.C:
			deployment := &appsv1.Deployment{}
			key := client.ObjectKey{Name: eppServerName, Namespace: m.namespace}
			if err := m.testInstallation.ClusterContext.Client.Get(m.ctx, key, deployment); err != nil {
				continue
			}
			if deployment.Status.ReadyReplicas > 0 {
				return nil
			}
		}
	}
}

// Cleanup removes the mock EPP server from the cluster.
func (m *MockEPPServerManager) Cleanup() error {
	deleteOpts := &client.DeleteOptions{
		GracePeriodSeconds: ptr(int64(0)),
		PropagationPolicy:  delPropPolicy(metav1.DeletePropagationBackground),
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: eppServerName, Namespace: m.namespace},
	}
	if err := m.testInstallation.ClusterContext.Client.Delete(m.ctx, deployment, deleteOpts); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete mock EPP server deployment: %w", err)
		}
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: eppServerName, Namespace: m.namespace},
	}
	if err := m.testInstallation.ClusterContext.Client.Delete(m.ctx, service, deleteOpts); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete mock EPP server service: %w", err)
		}
	}

	return nil
}
