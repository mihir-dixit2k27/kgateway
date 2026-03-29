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
	modelServerName  = "mock-model-server"
	modelServerImage = "hashicorp/http-echo:0.2.3"
	modelServerPort  = 8080
)

// MockModelServerManager handles deployment and lifecycle of the mock model server.
// The mock model server simulates an OpenAI-compatible LLM backend that:
//   - Accepts POST /v1/chat/completions
//   - Returns configurable fixed-latency JSON responses
//   - Supports SSE streaming with configurable token count and inter-token delay
type MockModelServerManager struct {
	ctx              context.Context
	testInstallation *e2e.TestInstallation
	namespace        string
	config           BenchConfig
}

// NewMockModelServerManager creates a new MockModelServerManager.
func NewMockModelServerManager(ctx context.Context, testInst *e2e.TestInstallation, namespace string, config BenchConfig) *MockModelServerManager {
	return &MockModelServerManager{
		ctx:              ctx,
		testInstallation: testInst,
		namespace:        namespace,
		config:           config,
	}
}

// Deploy creates the mock model server Deployment and Service in the cluster.
func (m *MockModelServerManager) Deploy() error {
	replicas := int32(2)
	labels := map[string]string{
		"app":                          modelServerName,
		"app.kubernetes.io/name":       modelServerName,
		"bench.kgateway.dev/component": "model-server",
	}

	envVars := []corev1.EnvVar{
		{Name: "RESPONSE_DELAY_MS", Value: "5"},
		{Name: "STREAMING_ENABLED", Value: fmt.Sprintf("%t", m.config.Streaming)},
		{Name: "TOKEN_COUNT", Value: fmt.Sprintf("%d", m.config.TokenCount)},
		{Name: "TOKEN_DELAY_MS", Value: fmt.Sprintf("%d", m.config.TokenDelay.Milliseconds())},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelServerName,
			Namespace: m.namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": modelServerName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  modelServerName,
							Image: modelServerImage,
							Args:  []string{"-listen=:8080", `-text={"id":"bench-1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"benchmark response"}}]}`},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: modelServerPort,
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
										Port: intstr.FromInt(modelServerPort),
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
			return fmt.Errorf("failed to create mock model server deployment: %w", err)
		}
	}

	// Create the service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      modelServerName,
			Namespace: m.namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": modelServerName},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(modelServerPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := m.testInstallation.ClusterContext.Client.Create(m.ctx, service); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("failed to create mock model server service: %w", err)
		}
	}

	return nil
}

// WaitForReady waits until the mock model server pods are running.
func (m *MockModelServerManager) WaitForReady(timeout time.Duration) error {
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCh:
			return fmt.Errorf("timeout waiting for mock model server to be ready")
		case <-ticker.C:
			deployment := &appsv1.Deployment{}
			key := client.ObjectKey{Name: modelServerName, Namespace: m.namespace}
			if err := m.testInstallation.ClusterContext.Client.Get(m.ctx, key, deployment); err != nil {
				continue
			}
			if deployment.Status.ReadyReplicas > 0 {
				return nil
			}
		}
	}
}

// Cleanup removes the mock model server from the cluster.
func (m *MockModelServerManager) Cleanup() error {
	deleteOpts := &client.DeleteOptions{
		GracePeriodSeconds: ptr(int64(0)),
		PropagationPolicy:  delPropPolicy(metav1.DeletePropagationBackground),
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: modelServerName, Namespace: m.namespace},
	}
	if err := m.testInstallation.ClusterContext.Client.Delete(m.ctx, deployment, deleteOpts); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete mock model server deployment: %w", err)
		}
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: modelServerName, Namespace: m.namespace},
	}
	if err := m.testInstallation.ClusterContext.Client.Delete(m.ctx, service, deleteOpts); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete mock model server service: %w", err)
		}
	}

	return nil
}

// ptr returns a pointer to the given value.
func ptr[T any](v T) *T {
	return &v
}

// delPropPolicy returns a pointer to the given DeletionPropagation.
func delPropPolicy(p metav1.DeletionPropagation) *metav1.DeletionPropagation {
	return &p
}
