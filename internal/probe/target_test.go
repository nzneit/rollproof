package probe

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"rollproof/internal/verdict"
)

// service is a Service with a cluster address; each change edits it.
func service(namespace, name string, changes ...func(*corev1.Service)) *corev1.Service {
	s := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.96.0.20",
			Ports: []corev1.ServicePort{{Port: 80}}},
	}
	for _, change := range changes {
		change(s)
	}
	return s
}

func TestCheckTarget(t *testing.T) {
	const waysOut = "the ways out: a Service with a cluster address for the test, or a custom probe"
	const oneClient = "the Service sends one client to one pod; every connection of the probe reaches the same pod until that pod is removed"
	const missing = "the host ends in .svc and no such Service exists: orders/orders; " + waysOut
	const onePod = "the host names one pod of a headless Service: pod orders-0 of Service orders/orders; " +
		"the probe would go on dialling the address of a pod that has been replaced; " + waysOut
	const headlessText = "Service orders/orders is headless: a headless Service has no cluster address, and its name resolves to the addresses of pods, which change; " + waysOut
	const unreadable = "the host cannot be read as a Service: it has an empty label, or it ends in .svc or in the cluster's domain and is not NAME.NAMESPACE.svc; " + waysOut
	orders := &types.NamespacedName{Namespace: "orders", Name: "orders"}
	withAddress := []runtime.Object{service("orders", "orders")}
	headless := []runtime.Object{service("orders", "orders", func(s *corev1.Service) {
		s.Spec.ClusterIP = corev1.ClusterIPNone
	})}
	nodeLocal := []runtime.Object{service("orders", "orders", func(s *corev1.Service) {
		local := corev1.ServiceInternalTrafficPolicyLocal
		s.Spec.InternalTrafficPolicy = &local
	})}
	affinity := []runtime.Object{service("orders", "orders", func(s *corev1.Service) {
		s.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
	})}
	externalName := []runtime.Object{service("orders", "orders", func(s *corev1.Service) {
		s.Spec.Type = corev1.ServiceTypeExternalName
		s.Spec.ExternalName = "orders.example.com"
		s.Spec.ClusterIP = ""
	})}
	for _, r := range []struct {
		name    string
		objs    []runtime.Object
		url     string
		service *types.NamespacedName
		notes   []string
		code    verdict.Code // empty: no error
		text    string
	}{
		{name: "NAME.NS.svc", objs: withAddress, url: "http://orders.orders.svc", service: orders},
		{name: "NAME.NS", objs: withAddress, url: "http://orders.orders", service: orders},
		{name: "NAME.NS.svc and the cluster's domain", objs: withAddress, url: "http://orders.orders.svc.cluster.local", service: orders},
		{name: "a port", objs: withAddress, url: "http://orders.orders.svc:8080", service: orders},
		{name: "a path, a query and a port", objs: withAddress, url: "http://orders.orders.svc:8080/work?ms=20", service: orders},
		{name: "one label names a Service of the probe's namespace",
			objs: []runtime.Object{service("rollproof-probe", "orders")}, url: "http://orders",
			service: &types.NamespacedName{Namespace: "rollproof-probe", Name: "orders"}},
		{name: "one label that names nothing is probed as it is", objs: withAddress, url: "http://orders"},
		{name: "NAME.NS.svc that does not exist", url: "http://orders.orders.svc",
			code: verdict.TargetUnsupported, text: missing},
		{name: "NAME.NS.svc.cluster.local that does not exist", url: "http://orders.orders.svc.cluster.local",
			code: verdict.TargetUnsupported, text: missing},
		{name: "NAME.NS that does not exist is probed as it is", url: "http://orders.orders"},
		{name: "a headless Service", objs: headless, url: "http://orders.orders.svc",
			code: verdict.TargetUnsupported,
			text: "Service orders/orders is headless: a headless Service has no cluster address, and its name resolves to the addresses of pods, which change; " + waysOut},
		{name: "one pod of a headless Service", objs: headless, url: "http://orders-0.orders.orders.svc",
			code: verdict.TargetUnsupported, text: onePod},
		{name: "one pod, with the cluster's domain", url: "http://orders-0.orders.orders.svc.cluster.local",
			code: verdict.TargetUnsupported, text: onePod},
		{name: "a Service that routes only within a node", objs: nodeLocal, url: "http://orders.orders.svc",
			code: verdict.TargetUnsupported,
			text: "Service orders/orders routes only within a node (internalTrafficPolicy Local), so it has no endpoint for the probe's node; " + waysOut},
		{name: "an IPv4 address", objs: withAddress, url: "http://10.96.0.10"},
		{name: "an IPv6 address with a port", objs: withAddress, url: "http://[fd00::1]:8080"},
		{name: "an outside name", objs: withAddress, url: "http://api.example.com"},
		{name: "two labels that name no Service", url: "http://example.com"},
		{name: "upper case, found", objs: withAddress, url: "http://ORDERS.Orders.SVC", service: orders},
		{name: "upper case, no such Service", url: "http://ORDERS.Orders.SVC",
			code: verdict.TargetUnsupported, text: missing},
		{name: "upper case, headless", objs: headless, url: "http://ORDERS.Orders.SVC",
			code: verdict.TargetUnsupported, text: headlessText},
		{name: "trailing dot, found", objs: withAddress, url: "http://orders.orders.svc.", service: orders},
		{name: "trailing dot, no such Service", url: "http://orders.orders.svc.",
			code: verdict.TargetUnsupported, text: missing},
		{name: "trailing dot, headless", objs: headless, url: "http://orders.orders.svc.",
			code: verdict.TargetUnsupported, text: headlessText},
		{name: "domain and trailing dot, found", objs: withAddress, url: "http://orders.orders.svc.cluster.local.", service: orders},
		{name: "domain and trailing dot, no such Service", url: "http://orders.orders.svc.cluster.local.",
			code: verdict.TargetUnsupported, text: missing},
		{name: "domain and trailing dot, headless", objs: headless, url: "http://orders.orders.svc.cluster.local.",
			code: verdict.TargetUnsupported, text: headlessText},
		{name: "domain without .svc", objs: withAddress, url: "http://orders.orders.cluster.local",
			code: verdict.TargetUnsupported, text: unreadable},
		{name: "svc.cluster.local alone", objs: withAddress, url: "http://svc.cluster.local",
			code: verdict.TargetUnsupported, text: unreadable},
		{name: "empty label before svc", objs: withAddress, url: "http://orders..svc",
			code: verdict.TargetUnsupported, text: unreadable},
		{name: "empty first label", objs: withAddress, url: "http://.orders.orders.svc",
			code: verdict.TargetUnsupported, text: unreadable},
		{name: "empty middle label", objs: withAddress, url: "http://orders..orders.svc",
			code: verdict.TargetUnsupported, text: unreadable},
		{name: "a user part that looks like a host", objs: withAddress, url: "http://orders.orders.svc@evil.example/"},
		{name: "https adds no note", objs: withAddress, url: "https://orders.orders.svc", service: orders},
		{name: "affinity by client address", objs: affinity, url: "http://orders.orders.svc", service: orders,
			notes: []string{oneClient}},
		{name: "a Service of type ExternalName names no Service of the cluster", objs: externalName,
			url: "http://orders.orders.svc"},
	} {
		t.Run(r.name, func(t *testing.T) {
			got, err := CheckTarget(t.Context(), fake.NewClientset(r.objs...), r.url)
			if r.code != "" {
				var c verdict.Condition
				if !errors.As(err, &c) || c.Code != r.code || c.Text != r.text {
					t.Fatalf("err = %v\nwant %s: %s", err, r.code, r.text)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := Target{Service: r.service, Notes: r.notes}
			if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("target (-want +got):\n%s", diff)
			}
		})
	}
}

// An error of the API server that is not "not found" is the tool's own failure.
func TestCheckTargetAPIError(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("get", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("the server is currently unable to handle the request")
	})
	_, err := CheckTarget(t.Context(), client, "http://orders.orders.svc")
	var c verdict.Condition
	if err == nil || errors.As(err, &c) {
		t.Fatalf("err = %v; want a plain error", err)
	}
}

// No text that CheckTarget returns holds a part of the URL that can carry a secret.
func TestCheckTargetKeepsSecretsOut(t *testing.T) {
	const secret = "S3CRET-VALUE"
	headless := service("orders", "orders", func(s *corev1.Service) { s.Spec.ClusterIP = corev1.ClusterIPNone })
	affinity := service("orders", "orders", func(s *corev1.Service) { s.Spec.SessionAffinity = corev1.ServiceAffinityClientIP })
	for _, r := range []struct {
		name string
		objs []runtime.Object
		url  string
	}{
		{"user, missing Service", nil, "http://" + secret + "@orders.orders.svc"},
		{"password, missing Service", nil, "http://u:" + secret + "@orders.orders.svc"},
		{"query, missing Service", nil, "http://orders.orders.svc/?k=" + secret},
		{"user, headless", []runtime.Object{headless}, "http://" + secret + "@orders.orders.svc"},
		{"password, headless", []runtime.Object{headless}, "http://u:" + secret + "@orders.orders.svc"},
		{"query, headless", []runtime.Object{headless}, "http://orders.orders.svc/?k=" + secret},
		{"user, one pod", nil, "http://" + secret + "@orders-0.orders.orders.svc"},
		{"password, one pod", nil, "http://u:" + secret + "@orders-0.orders.orders.svc"},
		{"query, one pod", nil, "http://orders-0.orders.orders.svc/?k=" + secret},
		{"user, notes", []runtime.Object{affinity}, "http://" + secret + "@orders.orders.svc"},
		{"password, notes", []runtime.Object{affinity}, "http://u:" + secret + "@orders.orders.svc"},
		{"query, notes", []runtime.Object{affinity}, "http://orders.orders.svc/?k=" + secret},
		{"fragment, notes", []runtime.Object{affinity}, "http://orders.orders.svc/#" + secret},
		{"password, URL that does not parse", nil, "http://u:" + secret + "@orders.orders.svc:bad"},
		{"query, URL that does not parse", nil, "http://orders.orders.svc:bad/?k=" + secret},
		{"user, no host", nil, "http://" + secret + "@"},
	} {
		t.Run(r.name, func(t *testing.T) {
			got, err := CheckTarget(t.Context(), fake.NewClientset(r.objs...), r.url)
			var texts []string
			if err != nil {
				texts = append(texts, err.Error())
			}
			texts = append(texts, got.Notes...)
			if got.Service != nil {
				texts = append(texts, got.Service.String())
			}
			for _, text := range texts {
				if strings.Contains(text, secret) {
					t.Errorf("text %q holds the secret", text)
				}
			}
		})
	}
}

// A host that cannot be read as a Service fails closed, and a URL that cannot be read is a plain error.
func TestCheckTargetUnreadable(t *testing.T) {
	for _, u := range []string{"http://a.b.c.d.svc", "http://ns.svc", "http://a.b.c.d.svc.cluster.local"} {
		_, err := CheckTarget(t.Context(), fake.NewClientset(), u)
		var c verdict.Condition
		if !errors.As(err, &c) || c.Code != verdict.TargetUnsupported {
			t.Errorf("%s: err = %v; want TargetUnsupported", u, err)
		}
	}
	for _, u := range []string{"http://orders.orders.svc:bad", "http:///x", ""} {
		_, err := CheckTarget(t.Context(), fake.NewClientset(), u)
		var c verdict.Condition
		if err == nil || errors.As(err, &c) {
			t.Errorf("%q: err = %v; want a plain error", u, err)
		}
	}
}
