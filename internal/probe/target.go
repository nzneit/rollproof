package probe

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"rollproof/internal/cluster"
	"rollproof/internal/verdict"
)

// clusterDomain is the domain of the clusters the tool creates; the tool does not change it.
const clusterDomain = "cluster.local"

// waysOut ends every TargetUnsupported text.
const waysOut = "; the ways out: a Service with a cluster address for the test, or a custom probe"

// Target is what the built-in probe's URL points at. Service is the Service its host names, nil
// when the host names none of the cluster. Notes are sentences for the report about how this
// target is probed.
type Target struct {
	Service *types.NamespacedName
	Notes   []string
}

// CheckTarget reads the URL's host as a Service's name and looks the Service up. An error is a
// Condition with TargetUnsupported: a Service that does not exist, is headless, routes only
// within a node, or a host that names one pod of a headless Service. The text names a way out.
// A URL that cannot be read, or an API error other than "not found", is a plain error. No text
// of an error quotes the URL, which can hold a user, a password or a query.
func CheckTarget(ctx context.Context, client kubernetes.Interface, rawURL string) (Target, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		// The error of url.Parse quotes the URL, so it is not wrapped.
		return Target{}, errors.New("the probe's URL cannot be parsed")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return Target{}, errors.New("the probe's URL has no host")
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return Target{}, nil
	}

	name, namespace, explicit, err := readHost(host)
	if err != nil {
		return Target{}, err
	}
	if name == "" { // an outside name
		return Target{}, nil
	}
	svc, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	ref := types.NamespacedName{Namespace: namespace, Name: name}
	switch {
	case apierrors.IsNotFound(err):
		if explicit {
			return Target{}, unsupported("the host ends in .svc and no such Service exists: %s", ref)
		}
		return Target{}, nil
	case err != nil:
		return Target{}, fmt.Errorf("look up Service %s: %w", ref, err)
	}

	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		return Target{}, nil
	}
	if svc.Spec.ClusterIP == corev1.ClusterIPNone {
		return Target{}, unsupported("Service %s is headless: a headless Service has no cluster address, "+
			"and its name resolves to the addresses of pods, which change", ref)
	}
	if p := svc.Spec.InternalTrafficPolicy; p != nil && *p == corev1.ServiceInternalTrafficPolicyLocal {
		return Target{}, unsupported("Service %s routes only within a node (internalTrafficPolicy Local), "+
			"so it has no endpoint for the probe's node", ref)
	}
	t := Target{Service: &ref}
	if svc.Spec.SessionAffinity == corev1.ServiceAffinityClientIP {
		t.Notes = append(t.Notes, "the Service sends one client to one pod; every connection of the probe reaches the same pod until that pod is removed")
	}
	return t, nil
}

// readHost reads a host that is not an address. name is empty when the host names no Service.
// explicit is true when the host ends in .svc or in the cluster's domain, so that a missing
// Service is an error. A host with an empty label, or one in the cluster's domain of another
// shape than NAME.NAMESPACE.svc or POD.NAME.NAMESPACE.svc, is an error.
func readHost(host string) (name, namespace string, explicit bool, err error) {
	if slices.Contains(strings.Split(host, "."), "") {
		return "", "", true, unreadableHost()
	}
	h, inDomain := strings.CutSuffix(host, "."+clusterDomain)
	if inDomain || strings.HasSuffix(h, ".svc") {
		rest, isSvc := strings.CutSuffix(h, ".svc")
		if !isSvc {
			return "", "", true, unreadableHost()
		}
		labels := strings.Split(rest, ".")
		switch len(labels) {
		case 2:
			return labels[0], labels[1], true, nil
		case 3:
			return "", "", true, unsupported("the host names one pod of a headless Service: pod %s of Service %s/%s; "+
				"the probe would go on dialling the address of a pod that has been replaced", labels[0], labels[2], labels[1])
		default:
			return "", "", true, unreadableHost()
		}
	}
	switch labels := strings.Split(host, "."); len(labels) {
	case 1:
		return labels[0], cluster.ProbeNamespace, false, nil
	case 2:
		return labels[0], labels[1], false, nil
	}
	return "", "", false, nil
}

// unreadableHost is the error of a host that ends in .svc or in the cluster's domain, or has an
// empty label, and that is not NAME.NAMESPACE.svc or POD.NAME.NAMESPACE.svc.
func unreadableHost() error {
	return unsupported("the host cannot be read as a Service: it has an empty label, or it is in the cluster's domain " +
		"and is not NAME.NAMESPACE.svc")
}

// unsupported returns a TargetUnsupported condition that ends with the ways out.
func unsupported(format string, args ...any) verdict.Condition {
	return verdict.Condition{Code: verdict.TargetUnsupported, Text: fmt.Sprintf(format, args...) + waysOut}
}
