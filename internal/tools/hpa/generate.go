package hpa

import (
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// Request is the structured input for HorizontalPodAutoscaler generation.
type Request struct {
	Name           string
	Namespace      string
	TargetName     string // Deployment to scale
	TargetKind     string // default Deployment
	MinReplicas    int32
	MaxReplicas    int32
	CPUPercent     int32 // averageUtilization; 0 = omit
	MemoryPercent  int32 // averageUtilization; 0 = omit
}

// Generate builds an autoscaling/v2 HorizontalPodAutoscaler YAML.
func Generate(req Request) (manifest string, summary string, err error) {
	req = normalizeRequest(req)
	if req.TargetName == "" {
		return "", "", fmt.Errorf("hpa scaleTargetRef.name is required")
	}
	if req.Name == "" {
		req.Name = DefaultHPAName(req.TargetName)
	}
	if req.Namespace == "" {
		req.Namespace = "default"
	}
	if req.TargetKind == "" {
		req.TargetKind = "Deployment"
	}
	if req.MinReplicas < 1 {
		req.MinReplicas = 1
	}
	if req.MaxReplicas < 1 {
		req.MaxReplicas = 10
	}
	if req.MaxReplicas < req.MinReplicas {
		return "", "", fmt.Errorf("hpa maxReplicas (%d) must be >= minReplicas (%d)", req.MaxReplicas, req.MinReplicas)
	}
	if req.CPUPercent <= 0 && req.MemoryPercent <= 0 {
		req.CPUPercent = 70
	}

	min := req.MinReplicas
	h := &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "autoscaling/v2",
			Kind:       "HorizontalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kprompt",
			},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       req.TargetKind,
				Name:       req.TargetName,
			},
			MinReplicas: &min,
			MaxReplicas: req.MaxReplicas,
			Metrics:     buildMetrics(req),
		},
	}

	raw, err := yaml.Marshal(h)
	if err != nil {
		return "", "", err
	}

	parts := []string{
		fmt.Sprintf("HPA/%s: scale %s/%s min=%d max=%d",
			req.Name, req.TargetKind, req.TargetName, req.MinReplicas, req.MaxReplicas),
	}
	if req.CPUPercent > 0 {
		parts = append(parts, fmt.Sprintf("cpu=%d%%", req.CPUPercent))
	}
	if req.MemoryPercent > 0 {
		parts = append(parts, fmt.Sprintf("memory=%d%%", req.MemoryPercent))
	}
	summary = strings.Join(parts, " ")
	return string(raw), summary, nil
}

// DefaultHPAName builds a DNS-safe HPA name for a workload.
func DefaultHPAName(target string) string {
	return sanitizeName(target + "-hpa")
}

func buildMetrics(req Request) []autoscalingv2.MetricSpec {
	var metrics []autoscalingv2.MetricSpec
	if req.CPUPercent > 0 {
		cpu := req.CPUPercent
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &cpu,
				},
			},
		})
	}
	if req.MemoryPercent > 0 {
		mem := req.MemoryPercent
		metrics = append(metrics, autoscalingv2.MetricSpec{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceMemory,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &mem,
				},
			},
		})
	}
	return metrics
}

func normalizeRequest(req Request) Request {
	req.Name = sanitizeName(req.Name)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.TargetName = sanitizeName(req.TargetName)
	req.TargetKind = strings.TrimSpace(req.TargetKind)
	if req.TargetKind == "" {
		req.TargetKind = "Deployment"
	}
	return req
}

func sanitizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		return "kprompt-hpa"
	}
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}
