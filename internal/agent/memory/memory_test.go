package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFileStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := New(FileStore{Dir: dir})
	_, err := m.Upsert("payments", Fact{Kind: KindDependency, Key: "redis", Value: "service/redis", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := m.List("payments")
	if err != nil || len(snap.Facts) != 1 || snap.Facts[0].Key != "redis" {
		t.Fatalf("%+v %v", snap, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "payments.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRelevantFilters(t *testing.T) {
	facts := []Fact{
		{ID: "dependency/redis", Kind: KindDependency, Key: "redis", Value: "svc"},
		{ID: "dependency/kafka", Kind: KindDependency, Key: "kafka", Value: "svc"},
		{ID: "note/team", Kind: KindNote, Key: "team", Value: "payments"},
	}
	got := Relevant(facts, "connection refused to redis-master")
	if len(got) < 1 || got[0].Key != "redis" {
		t.Fatalf("%+v", got)
	}
	got2 := Relevant(facts, "upstream timeout talking to broker")
	foundKafka := false
	for _, f := range got2 {
		if f.Key == "kafka" {
			foundKafka = true
		}
	}
	if !foundKafka {
		t.Fatalf("expected kafka via infra hint: %+v", got2)
	}
}

func TestDiscoverFromFake(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "redis-master", Namespace: "ns"}},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "ns"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "api",
							Image: "ghcr.io/example/api:1",
							Env:   []corev1.EnvVar{{Name: "KAFKA_BROKERS", Value: "kafka:9092"}},
						}},
					},
				},
			},
		},
	)
	facts, err := Discover(context.Background(), client, "ns")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, f := range facts {
		keys[f.Key] = true
	}
	if !keys["redis"] || !keys["kafka"] {
		t.Fatalf("keys=%v", keys)
	}
}

func TestNeverUploadsMarker(t *testing.T) {
	b, err := Encode(Snapshot{Namespace: "ns", Facts: []Fact{{Key: "redis", Kind: KindDependency, UpdatedAt: time.Now().UTC()}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "api.kprompt.ai") {
		t.Fatal("unexpected control plane reference")
	}
}

func TestMemorySaveScopedToSnapshotNS(t *testing.T) {
	dir := t.TempDir()
	m := New(FileStore{Dir: dir})
	if _, err := m.Upsert("payments", Fact{Kind: KindDependency, Key: "redis", Value: "svc/redis"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Upsert("platform", Fact{Kind: KindDependency, Key: "redis", Value: "svc/redis-master"}); err != nil {
		t.Fatal(err)
	}
	pay, err := m.List("payments")
	if err != nil {
		t.Fatal(err)
	}
	plat, err := m.List("platform")
	if err != nil {
		t.Fatal(err)
	}
	if pay.Namespace != "payments" || plat.Namespace != "platform" {
		t.Fatalf("namespace stamps: pay=%q plat=%q", pay.Namespace, plat.Namespace)
	}
	if len(pay.Facts) != 1 || pay.Facts[0].Value != "svc/redis" {
		t.Fatalf("payments polluted: %+v", pay.Facts)
	}
	if len(plat.Facts) != 1 || plat.Facts[0].Value != "svc/redis-master" {
		t.Fatalf("platform polluted: %+v", plat.Facts)
	}
	if _, err := os.Stat(filepath.Join(dir, "payments.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "platform.json")); err != nil {
		t.Fatal(err)
	}
}
