package watch

import (
	"context"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// behind returns the watched workloads behind the Service that o.Service names: the workloads of
// the pods that its EndpointSlices name as an endpoint's target. A Service in a namespace other
// than the watched one has no watched workload behind it, and nothing is read.
func (w *Watcher) behind(ctx context.Context) (map[Workload]bool, error) {
	svc := w.o.Service
	found := map[Workload]bool{}
	if svc.Namespace != w.o.Namespace {
		return found, nil
	}
	slices, err := w.client.DiscoveryV1().EndpointSlices(svc.Namespace).List(ctx,
		metav1.ListOptions{LabelSelector: discoveryv1.LabelServiceName + "=" + svc.Name})
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range slices.Items {
		for _, e := range s.Endpoints {
			if ref := e.TargetRef; ref != nil && ref.Kind == "Pod" {
				if wl, ok := w.tr.podWorkloadByUID(ref.UID); ok {
					found[wl] = true
				}
			}
		}
	}
	return found, nil
}

// podWorkloadByUID gives the workload of the pod with that UID when the Tracker knows it.
func (t *Tracker) podWorkloadByUID(uid types.UID) (Workload, bool) {
	p, ok := t.pods[uid]
	if !ok {
		return Workload{}, false
	}
	return t.podWorkload(p)
}

// reading is one reading of the workloads behind the Service: nil when there is no Service, the
// reading failed, or ctx had ended when it returned, which counts as no reading.
func (w *Watcher) reading(ctx context.Context) map[Workload]bool {
	if w.o.Service == nil {
		return nil
	}
	found, err := w.behind(ctx)
	if err != nil || ctx.Err() != nil {
		return nil
	}
	return found
}

// withProbed returns s with each Count's Probed set from the readings: yes when its workload was
// behind the Service at any reading, no when it was at none, and not known when no reading was
// made.
func withProbed(s Transition, readings ...map[Workload]bool) Transition {
	for i := range s.Workloads {
		s.Workloads[i].Probed = probed(s.Workloads[i].Workload, readings)
	}
	return s
}

// probed is the Probed of the workload wl from the readings, where nil is no reading.
func probed(wl Workload, readings []map[Workload]bool) string {
	read := false
	for _, r := range readings {
		if r == nil {
			continue
		}
		if r[wl] {
			return "yes"
		}
		read = true
	}
	if !read {
		return "not known"
	}
	return "no"
}
