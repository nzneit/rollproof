package probe

import (
	"encoding/json"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"

	"rollproof/internal/config"
)

// CustomEnv turns a custom probe's env entries into a container's, in order: value
// as it is, valueFrom as the container's source. It refuses a valueFrom
// whose keys a container's source does not hold, whose values do not have its
// types, or that has not exactly one source. The error names the entry by its
// index and name and never quotes a value. Nil or empty gives nil.
func CustomEnv(env []config.EnvVar) ([]corev1.EnvVar, error) {
	if len(env) == 0 {
		return nil, nil
	}
	out := make([]corev1.EnvVar, 0, len(env))
	for i, e := range env {
		v := corev1.EnvVar{Name: e.Name, Value: e.Value}
		if e.ValueFrom != nil {
			src, reason := envVarSource(e.ValueFrom)
			if src == nil {
				return nil, fmt.Errorf("probe.custom.env[%d] (%s): %s", i, e.Name, reason)
			}
			v.ValueFrom = src
		}
		out = append(out, v)
	}
	return out, nil
}

// envVarSource turns a valueFrom map into the container's source, or gives the reason it cannot:
// the map is not the container's form, holds a key the container does not take, or has not
// exactly one source.
func envVarSource(m map[string]any) (*corev1.EnvVarSource, string) {
	const form = "valueFrom does not have the form a container takes"
	b, err := json.Marshal(m)
	if err != nil {
		return nil, form
	}
	var src corev1.EnvVarSource
	if err := json.Unmarshal(b, &src); err != nil {
		return nil, form
	}
	back, err := json.Marshal(src)
	if err != nil {
		return nil, form
	}
	var held map[string]any
	if err := json.Unmarshal(back, &held); err != nil {
		return nil, form
	}
	// encoding/json matches a key written in another case and drops an unknown one without a
	// word; a key of m that the round trip did not keep is one of those.
	if p := firstMissing(m, held, ""); p != "" {
		return nil, "valueFrom holds a key that a container does not take: " + p
	}
	n := 0
	if src.ConfigMapKeyRef != nil {
		n++
	}
	if src.FieldRef != nil {
		n++
	}
	if src.FileKeyRef != nil {
		n++
	}
	if src.ResourceFieldRef != nil {
		n++
	}
	if src.SecretKeyRef != nil {
		n++
	}
	if n != 1 {
		return nil, "valueFrom needs exactly one of configMapKeyRef, fieldRef, fileKeyRef, resourceFieldRef and secretKeyRef"
	}
	return &src, ""
}

// firstMissing is the first, in sorted order, of the key paths of in that held does not hold, or
// "" when it holds every one. A path names its parents, as in secretKeyRef.name.
func firstMissing(in, held map[string]any, prefix string) string {
	var paths []string
	missing(in, held, prefix, &paths)
	if len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	return paths[0]
}

// missing adds to paths every key path of in that held does not hold.
func missing(in, held map[string]any, prefix string, paths *[]string) {
	for k, v := range in {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		sub, ok := held[k]
		if !ok {
			*paths = append(*paths, p)
			continue
		}
		inMap, inIsMap := v.(map[string]any)
		heldMap, heldIsMap := sub.(map[string]any)
		if inIsMap && heldIsMap {
			missing(inMap, heldMap, p, paths)
		}
	}
}
