package probe

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"

	"rollproof/internal/config"
)

func TestCustomEnv(t *testing.T) {
	got, err := CustomEnv([]config.EnvVar{
		{Name: "TARGET", Value: "orders-db.orders:5432"},
		{Name: "TOKEN", ValueFrom: map[string]any{"secretKeyRef": map[string]any{"name": "probe-token", "key": "token"}}},
		{Name: "MODE", ValueFrom: map[string]any{"configMapKeyRef": map[string]any{"name": "probe", "key": "mode", "optional": true}}},
		{Name: "POD_IP", ValueFrom: map[string]any{"fieldRef": map[string]any{"fieldPath": "status.podIP"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []corev1.EnvVar{
		{Name: "TARGET", Value: "orders-db.orders:5432"},
		{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "probe-token"}, Key: "token"}}},
		{Name: "MODE", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "probe"}, Key: "mode", Optional: new(true)}}},
		{Name: "POD_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"}}},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("CustomEnv (-want +got):\n%s", d)
	}
	if got, err := CustomEnv(nil); got != nil || err != nil {
		t.Errorf("CustomEnv(nil) = %v, %v; want nil, nil", got, err)
	}
}

func TestCustomEnvRefuses(t *testing.T) {
	secret := func(sel map[string]any) map[string]any { return map[string]any{"secretKeyRef": sel} }
	for _, c := range []struct {
		name      string
		valueFrom map[string]any
		want      string
	}{
		{"a misspelt source", map[string]any{"secretKeyRefs": map[string]any{"name": "S3CRET-NAME", "key": "token"}},
			"probe.custom.env[1] (TOKEN): valueFrom holds a key that a container does not take: secretKeyRefs"},
		{"a source in another case", map[string]any{"SecretKeyRef": map[string]any{"name": "S3CRET-NAME", "key": "token"}},
			"probe.custom.env[1] (TOKEN): valueFrom holds a key that a container does not take: SecretKeyRef"},
		{"a misspelt key inside", secret(map[string]any{"name": "S3CRET-NAME", "keys": "token"}),
			"probe.custom.env[1] (TOKEN): valueFrom holds a key that a container does not take: secretKeyRef.keys"},
		{"a value of another type", secret(map[string]any{"name": "S3CRET-NAME", "key": 12345}),
			"probe.custom.env[1] (TOKEN): valueFrom does not have the form a container takes"},
		{"two sources", map[string]any{
			"secretKeyRef":    map[string]any{"name": "S3CRET-NAME", "key": "token"},
			"configMapKeyRef": map[string]any{"name": "probe", "key": "token"}},
			"probe.custom.env[1] (TOKEN): valueFrom needs exactly one of configMapKeyRef, fieldRef, fileKeyRef, resourceFieldRef and secretKeyRef"},
		{"no source", map[string]any{},
			"probe.custom.env[1] (TOKEN): valueFrom needs exactly one of configMapKeyRef, fieldRef, fileKeyRef, resourceFieldRef and secretKeyRef"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := CustomEnv([]config.EnvVar{{Name: "TARGET", Value: "x"}, {Name: "TOKEN", ValueFrom: c.valueFrom}})
			if err == nil || err.Error() != c.want {
				t.Fatalf("CustomEnv = %v, want %q", err, c.want)
			}
			if strings.Contains(err.Error(), "S3CRET") || strings.Contains(err.Error(), "12345") {
				t.Errorf("the error %q quotes a value", err)
			}
		})
	}
}
