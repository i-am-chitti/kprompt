package autopilot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// LoadPolicyFile reads a RemediationPolicy JSON document (AG-040).
func LoadPolicyFile(path string) (Policy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	return ParsePolicy(raw)
}

// ParsePolicy unmarshals and normalizes a RemediationPolicy.
func ParsePolicy(raw []byte) (Policy, error) {
	var p Policy
	if err := json.Unmarshal(raw, &p); err != nil {
		return Policy{}, fmt.Errorf("remediation policy: %w", err)
	}
	p.Normalize()
	return p, nil
}

// LoadPolicyConfigMap loads policy.json from the namespace ConfigMap (AG-040).
func LoadPolicyConfigMap(ctx context.Context, client kubernetes.Interface, namespace string) (Policy, error) {
	if client == nil {
		return Policy{}, fmt.Errorf("remediation policy: kubernetes client is nil")
	}
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		return Policy{}, fmt.Errorf("remediation policy: namespace is required")
	}
	cm, err := client.CoreV1().ConfigMaps(ns).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		return Policy{}, err
	}
	raw, ok := cm.Data[ConfigMapKey]
	if !ok || strings.TrimSpace(raw) == "" {
		return Policy{}, fmt.Errorf("remediation policy: ConfigMap %s missing key %s", ConfigMapName, ConfigMapKey)
	}
	p, err := ParsePolicy([]byte(raw))
	if err != nil {
		return Policy{}, err
	}
	p.Namespace = ns
	return p, nil
}

// LoadPolicy prefers file, then ConfigMap, then DefaultPolicy.
func LoadPolicy(ctx context.Context, filePath, namespace string, client kubernetes.Interface) (Policy, string, error) {
	if p := strings.TrimSpace(filePath); p != "" {
		pol, err := LoadPolicyFile(p)
		return pol, "file:" + p, err
	}
	if client != nil && strings.TrimSpace(namespace) != "" {
		pol, err := LoadPolicyConfigMap(ctx, client, namespace)
		if err == nil {
			return pol, "configmap:" + ConfigMapName, nil
		}
		if !apierrors.IsNotFound(err) {
			return Policy{}, "", err
		}
	}
	return DefaultPolicy(), "default", nil
}

// EnsurePolicyConfigMap creates an empty propose-only policy CM when missing (optional helper).
func EnsurePolicyConfigMap(ctx context.Context, client kubernetes.Interface, namespace string, pol Policy) error {
	if client == nil {
		return fmt.Errorf("remediation policy: kubernetes client is nil")
	}
	pol.Normalize()
	pol.Namespace = namespace
	raw, err := json.MarshalIndent(pol, "", "  ")
	if err != nil {
		return err
	}
	ns := strings.TrimSpace(namespace)
	_, err = client.CoreV1().ConfigMaps(ns).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "kprompt-agent",
				"app.kubernetes.io/component":  "remediation-policy",
				"app.kubernetes.io/managed-by": "kprompt",
			},
		},
		Data: map[string]string{ConfigMapKey: string(raw)},
	}
	_, err = client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
	return err
}
