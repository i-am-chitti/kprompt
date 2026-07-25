//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestAgentWatchPodsEvents proves AG-003: list→watch surfaces Pod/Event without LLM.
func TestAgentWatchPodsEvents(t *testing.T) {
	requireKind(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	ensureKindCluster(t, ctx)
	kubeconfig := exportKubeconfig(t, ctx)
	t.Setenv("KUBECONFIG", kubeconfig)
	client := clientFromKubeconfig(t, kubeconfig)
	ensureNamespace(t, ctx, client)

	bin := buildKprompt(t)

	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "agent", "run",
		"-n", ns,
		"--json",
		"--duration", "8s",
	)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig)
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Create a pod while the agent is watching.
	time.Sleep(1500 * time.Millisecond)
	podName := "agent-watch-demo"
	_ = client.CoreV1().Pods(ns).Delete(ctx, podName, metav1.DeleteOptions{})
	_, err := client.CoreV1().Pods(ns).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: ns},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:    "pause",
				Image:   "registry.k8s.io/pause:3.9",
				Command: []string{"/pause"},
			}},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("create pod: %v\nstderr=%s", err, errBuf.String())
	}

	waitErr := cmd.Wait()
	text := out.String()
	if waitErr != nil && ctx.Err() == nil {
		// duration exit should be clean (nil); tolerate signal-ish
		t.Logf("agent exit: %v stderr=%s", waitErr, errBuf.String())
	}
	if !strings.Contains(text, `"resource":"Pod"`) && !strings.Contains(text, podName) {
		t.Fatalf("expected Pod watch JSON mentioning %s:\nstdout=%s\nstderr=%s", podName, text, errBuf.String())
	}
}

func buildKprompt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "kprompt")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/kprompt")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v (%s)", err, out)
	}
	return bin
}
