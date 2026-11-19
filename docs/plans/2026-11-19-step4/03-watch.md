# Step 4 plan, part 3: `watch`

> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints, its decisions and its "Interfaces between tasks" apply to every task here.

Every test in this part but Task 8's record test is a unit test of `internal/watch` against `k8s.io/client-go/kubernetes/fake`, on the fake clock of `watcher_test.go` (`t0` is `2026-11-13T10:00:00Z`; `at(s)` is `t0` plus `s` seconds). The helpers `base`, `deployment`, `statefulSet`, `daemonSet`, `replicaSet`, `pod`, `drive`, `until`, `update` and `closed` are the package's own. Run the package's tests with `go test ./internal/watch/ -v`; add `-race` at each task's Step 4, since the Watcher's goroutines grow here.

### Task 5: The restart

Design: section 7, "The restart", "When a transition has finished" (the first condition, for a restart), "What is watched", and the last paragraph of section 7 (a restart has no command; its record says whether the API server took the change); section 5 (`watch` performs the restart); section 12, "Without a cluster" (a restart that the API server refuses); README decisions 3 and 4, and Review Focus 5.

**Files:**
- Create: `internal/watch/restart.go`
- Test: new `internal/watch/restart_test.go`; `internal/watch/watcher_test.go` (`startWatcher` becomes a call of a new `startWatcherWith`)

**Interfaces:**
- Consumes: the Tracker's records of the workloads that exist and whose labels matched at their last Set (the set the install watches, `installWatched` in `settle.go`), as built.
- Produces: `type Restart struct { Began, Ended time.Time; Accepted bool; Workload, Text string }`
  How the tool's attempt to restart the watched workloads went. Began is the
  clock before the first change and Ended the moment the last change returned
  or the first refusal came: the restart's "command has exited". Accepted
  says that the API server took every change. When it refused one, Workload
  names that workload, as deployment/orders, and Text is the server's answer.
- Produces: `func (w *Watcher) Restart(ctx context.Context) Restart`
  Restarts every workload that exists and matches the selector when it is
  called, as README decision 3 says: one at a time, stopping at the first
  refusal. Accepted is true only when the API server took every change, and
  so also when there was nothing to change. When ctx ends first, Accepted is
  false and Text is ctx's error: the caller tells that from a refusal.

- [ ] **Step 1: Write the failing tests**

  In `watcher_test.go`, add `func startWatcherWith(t *testing.T, edit func(*Options), service *types.NamespacedName, objs ...runtime.Object) (*Watcher, *fake.Clientset, *fakeClock)`: the body of `startWatcher`, with the Options built into a variable `o`, `edit(&o)` called when `edit` is not nil, and `Start` given `o`. `startWatcher` keeps its signature and becomes `return startWatcherWith(t, nil, service, objs...)`.

  New `internal/watch/restart_test.go`, with the imports `context`, `errors`, `strings`, `testing`, `time`, go-cmp's `cmp`, `appsv1 "k8s.io/api/apps/v1"`, `apierrors "k8s.io/apimachinery/pkg/api/errors"`, `metav1`, `runtime`, `schema`, `types`, `k8s.io/client-go/kubernetes/fake` and `k8stesting "k8s.io/client-go/testing"`:

  ```go
  // restartObjects: four workloads that update by rolling, and deployment/batch, which the
  // selector app!=batch leaves out.
  func restartObjects() []runtime.Object {
  	const rollingSet, rollingDaemon = appsv1.RollingUpdateStatefulSetStrategyType, appsv1.RollingUpdateDaemonSetStrategyType
  	return []runtime.Object{
  		deployment("orders", 1, 1, 2, 2, 2, 2), deployment("api", 1, 1, 1, 1, 1, 1), deployment("batch", 1, 1, 1, 1, 1, 1),
  		statefulSet("web", 1, 1, 1, 1, 1, rollingSet), daemonSet("logs", 1, 1, 3, 3, 3, rollingDaemon),
  	}
  }

  const (
  	restartedAtKey = "kubectl.kubernetes.io/restartedAt"
  	restartPatch   = `{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"2026-11-13T10:00:00Z"}}}}}`
  )

  // patches gives each patch the fake client recorded, as RESOURCE/NAME, and fails the test unless
  // every one is a strategic merge patch whose body is restartPatch.
  func patches(t *testing.T, client *fake.Clientset) []string {
  	t.Helper()
  	var out []string
  	for _, a := range client.Actions() {
  		p, ok := a.(k8stesting.PatchAction)
  		if !ok || a.GetVerb() != "patch" {
  			continue
  		}
  		if p.GetPatchType() != types.StrategicMergePatchType || string(p.GetPatch()) != restartPatch {
  			t.Errorf("patch of %s/%s: type %s, body %s", a.GetResource().Resource, p.GetName(), p.GetPatchType(), p.GetPatch())
  		}
  		out = append(out, a.GetResource().Resource+"/"+p.GetName())
  	}
  	return out
  }

  // restartedAt reads the annotation from the pod template of RESOURCE/NAME in the fake client.
  func restartedAt(t *testing.T, client *fake.Clientset, workload string) string {
  	t.Helper()
  	resource, name, _ := strings.Cut(workload, "/")
  	apps := client.AppsV1()
  	var ann map[string]string
  	var err error
  	switch resource {
  	case "deployments":
  		var o *appsv1.Deployment
  		o, err = apps.Deployments("orders").Get(t.Context(), name, metav1.GetOptions{})
  		if err == nil {
  			ann = o.Spec.Template.Annotations
  		}
  	case "statefulsets":
  		var o *appsv1.StatefulSet
  		o, err = apps.StatefulSets("orders").Get(t.Context(), name, metav1.GetOptions{})
  		if err == nil {
  			ann = o.Spec.Template.Annotations
  		}
  	case "daemonsets":
  		var o *appsv1.DaemonSet
  		o, err = apps.DaemonSets("orders").Get(t.Context(), name, metav1.GetOptions{})
  		if err == nil {
  			ann = o.Spec.Template.Annotations
  		}
  	}
  	if err != nil {
  		t.Fatal(err)
  	}
  	return ann[restartedAtKey]
  }

  func TestRestartChangesEveryWatchedWorkload(t *testing.T) {
  	w, client, _ := startWatcherWith(t, func(o *Options) { o.Selector = "app!=batch" }, nil, restartObjects()...)
  	got := w.Restart(t.Context())
  	if d := cmp.Diff(Restart{Began: t0, Ended: t0, Accepted: true}, got); d != "" {
  		t.Errorf("Restart (-want +got):\n%s", d)
  	}
  	want := []string{"daemonsets/logs", "deployments/api", "deployments/orders", "statefulsets/web"}
  	if d := cmp.Diff(want, patches(t, client)); d != "" {
  		t.Errorf("patches (-want +got):\n%s", d)
  	}
  	for _, wl := range want {
  		if got := restartedAt(t, client, wl); got != "2026-11-13T10:00:00Z" {
  			t.Errorf("%s has %s %q", wl, restartedAtKey, got)
  		}
  	}
  	if got := restartedAt(t, client, "deployments/batch"); got != "" {
  		t.Errorf("deployment/batch, which the selector leaves out, has %s %q", restartedAtKey, got)
  	}
  }

  // Review Focus 5: one workload is refused after another was already changed.
  func TestRestartStopsAtTheFirstRefusal(t *testing.T) {
  	w, client, clock := startWatcherWith(t, func(o *Options) { o.Selector = "app!=batch" }, nil, restartObjects()...)
  	client.PrependReactor("patch", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
  		if a.(k8stesting.PatchAction).GetName() != "orders" {
  			return false, nil, nil
  		}
  		clock.Advance(2 * time.Second)
  		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "orders",
  			errors.New(`admission webhook "policy.example" denied the request`))
  	})
  	got := w.Restart(t.Context())
  	if got.Accepted || got.Workload != "deployment/orders" || !got.Began.Equal(t0) || !got.Ended.Equal(at(2)) {
  		t.Errorf("Restart gave %+v; want refused by deployment/orders, begun at t0, ended at +2 s", got)
  	}
  	if !strings.Contains(got.Text, `admission webhook "policy.example" denied the request`) {
  		t.Errorf("text %q does not hold the API server's answer", got.Text)
  	}
  	want := []string{"daemonsets/logs", "deployments/api", "deployments/orders"}
  	if d := cmp.Diff(want, patches(t, client)); d != "" {
  		t.Errorf("patches (-want +got):\n%s", d)
  	}
  	for _, wl := range []string{"daemonsets/logs", "deployments/api"} {
  		if got := restartedAt(t, client, wl); got != "2026-11-13T10:00:00Z" {
  			t.Errorf("%s, changed before the refusal, has %s %q", wl, restartedAtKey, got)
  		}
  	}
  	if got := restartedAt(t, client, "statefulsets/web"); got != "" {
  		t.Errorf("statefulset/web, after the refusal, has %s %q", restartedAtKey, got)
  	}
  }

  func TestRestartWhenCtxHasEnded(t *testing.T) {
  	w, client, _ := startWatcherWith(t, func(o *Options) { o.Selector = "app!=batch" }, nil, restartObjects()...)
  	ctx, cancel := context.WithCancel(t.Context())
  	cancel()
  	got := w.Restart(ctx)
  	if got.Accepted || got.Workload != "" || got.Text != context.Canceled.Error() {
  		t.Errorf("Restart with an ended ctx gave %+v; want not accepted, no workload, ctx's error", got)
  	}
  	if p := patches(t, client); len(p) != 0 {
  		t.Errorf("patches %v after ctx ended", p)
  	}
  }

  func TestRestartWithNothingToRestart(t *testing.T) {
  	w, client, _ := startWatcher(t, nil)
  	if d := cmp.Diff(Restart{Began: t0, Ended: t0, Accepted: true}, w.Restart(t.Context())); d != "" {
  		t.Errorf("Restart (-want +got):\n%s", d)
  	}
  	if p := patches(t, client); len(p) != 0 {
  		t.Errorf("patches %v with nothing to restart", p)
  	}
  }
  ```

  The window's verdict for a refusal, `restart-failed` with the server's text and the workload's name, is `run`'s (Task 14); this task gives `run` the facts.

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/watch/ -run 'TestRestart' -v` / Expected: FAIL to build: `Restart` (the type and the method) is not defined.

- [ ] **Step 3: Implement `Restart` and `(*Watcher).Restart` in `internal/watch/restart.go`**, with the interface comments from the Interfaces block. Under `w.mu`, read the clock and the workloads that exist and match (`installWatched`'s first list, already in kind-and-name order); the patches are made outside the lock, by kind through `AppsV1().Deployments`, `StatefulSets` or `DaemonSets` of the namespace, `types.StrategicMergePatchType`, `metav1.PatchOptions{FieldManager: "rollproof"}`, and the body `restartPatch` with the time read once at the start. `ctx.Err()` is checked before each patch. `Ended` is the clock read under `w.mu` after the last patch returned or the refusal came.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test -race ./internal/watch/ -v` / Expected: PASS.

- [ ] **Step 5: Commit**
  `git add internal/watch/restart.go internal/watch/restart_test.go internal/watch/watcher_test.go` and `git commit -m "Let watch restart the watched workloads one at a time by the annotation kubectl rollout restart sets, stopping at the first refusal"`

### Task 6: Finishes as they happen, and pod lists

Design: section 7, "When a transition has finished" (the settle time that starts again); section 10, "Evidence folder" (the moment each transition finished, written when it happens; the pod lists before and after each transition); section 12, the thirteenth row's paragraph (the harness reads the mark for the first finish); README decisions 13 and 15.

**Files:**
- Create: `internal/watch/marks.go` (`Finish`, the queue behind `Finishes`, the pod lists)
- Modify: `internal/watch/watcher.go` (`Options.Evidence`; `Finishes`; a finish noted after each call into the Tracker that can finish a transition: `set`, `gone` and the command's exit in `follow`; the pod lists in `Transition`; `Stop`), `internal/watch/tracker.go` (an unexported accessor of the open transition's name and `Finished`), `cmd/rollproof/wire.go` (`session.Watcher` passes `Evidence: s.evidence`)
- Test: new `internal/watch/marks_test.go`

**Interfaces:**
- Consumes: `startWatcherWith` (Task 5).
- Produces: `Options.Evidence func(name string) (io.WriteCloser, error)`
  Opens a file of the evidence folder by a name that may hold folders, such
  as pods/deploy-before.json. The Watcher writes the pod lists through it,
  and ignores its errors and its writers' errors: a caller that needs them
  wraps the writer, as run's evidence does. Nil writes nothing.
- Produces: `type Finish struct { Name verdict.WindowName; At time.Time }`
  One moment at which a transition came to be finished: the transition and
  the Watcher's clock at that moment. The last Finish of a transition whose
  window ended after the settle time is that Transition's Finished.
- Produces: `func (w *Watcher) Finishes() <-chan Finish`
  The channel on which the Watcher sends a Finish each time the open
  transition comes to be finished, again after its settle time started
  again, in order. Values wait for a reader and never hold up the watch. The
  same channel on every call. It is closed when Stop returns; a value not
  received by then is dropped.
- Produces: `func (w *Watcher) Transition(ctx context.Context, name verdict.WindowName, began time.Time, exited <-chan struct{}) Transition`
  As before, and it writes the pod list pods/NAME-before.json when it has
  begun the transition and pods/NAME-after.json when the window has ended or
  ctx ended: each a PodList, in JSON, of every pod of the namespace that the
  watch holds and that is not gone, by name.

- [ ] **Step 1: Write the failing tests**

  New `internal/watch/marks_test.go`, with the imports `bytes`, `encoding/json`, `io`, `maps`, `slices`, `sync`, `testing`, `time`, go-cmp's `cmp`, `corev1`, `metav1`, `k8s.io/client-go/kubernetes/fake` and `rollproof/internal/verdict`. Its helpers serve Task 7 too.

  ```go
  // evidenceFiles records what a Watcher writes through Options.Evidence. One lock guards every file.
  type evidenceFiles struct {
  	mu    sync.Mutex
  	files map[string]*recordedFile // by name
  }

  type recordedFile struct {
  	mu     *sync.Mutex
  	buf    bytes.Buffer
  	closed bool
  }

  func (f *recordedFile) Write(p []byte) (int, error) {
  	f.mu.Lock()
  	defer f.mu.Unlock()
  	return f.buf.Write(p)
  }

  func (f *recordedFile) Close() error {
  	f.mu.Lock()
  	defer f.mu.Unlock()
  	f.closed = true
  	return nil
  }

  func newEvidence() *evidenceFiles { return &evidenceFiles{files: map[string]*recordedFile{}} }

  func (e *evidenceFiles) open(name string) (io.WriteCloser, error) {
  	e.mu.Lock()
  	defer e.mu.Unlock()
  	f := &recordedFile{mu: &e.mu}
  	e.files[name] = f
  	return f, nil
  }

  // names lists the names opened so far, sorted.
  func (e *evidenceFiles) names() []string {
  	e.mu.Lock()
  	defer e.mu.Unlock()
  	return slices.Sorted(maps.Keys(e.files))
  }

  // state gives the text of the file name, whether it was opened, and whether it is closed.
  func (e *evidenceFiles) state(name string) (text string, opened, closed bool) {
  	e.mu.Lock()
  	defer e.mu.Unlock()
  	f, ok := e.files[name]
  	if !ok {
  		return "", false, false
  	}
  	return f.buf.String(), true, f.closed
  }

  // text waits up to 5 s of real time until the file name exists and is closed, and returns its text.
  func (e *evidenceFiles) text(t *testing.T, name string) string {
  	t.Helper()
  	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
  		if s, _, closed := e.state(name); closed {
  			return s
  		}
  		if time.Now().After(deadline) {
  			t.Fatalf("%s was not written and closed within 5 s; files: %v", name, e.names())
  		}
  	}
  }

  // holds waits up to 5 s of real time until the file name, open or closed, holds exactly want.
  func (e *evidenceFiles) holds(t *testing.T, name, want string) {
  	t.Helper()
  	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
  		if s, _, _ := e.state(name); s == want {
  			return
  		}
  		if time.Now().After(deadline) {
  			s, _, _ := e.state(name)
  			t.Fatalf("%s holds %q after 5 s, want %q", name, s, want)
  		}
  	}
  }

  // startDeploy begins a deploy on w and makes, through the client and at +1 s, the changes of
  // TestTransition: a new spec, ReplicaSet orders-b with two ready pods, the two old pods deleted,
  // the rollout complete. It returns when the Watcher has seen them, with the channel that tells
  // the command's exit, still open, and the channel of Transition's result.
  func startDeploy(t *testing.T, w *Watcher, client *fake.Clientset, clock *fakeClock) (chan struct{}, chan Transition) {
  	t.Helper()
  	ctx := t.Context()
  	exited := make(chan struct{})
  	res := make(chan Transition, 1)
  	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), exited) }()
  	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
  	clock.Advance(time.Second)
  	update(t, client, deployment("orders", 2, 1, 2, 2, 2, 2))
  	if _, err := client.AppsV1().ReplicaSets("orders").Create(ctx, replicaSet("orders-b", "orders"), metav1.CreateOptions{}); err != nil {
  		t.Fatal(err)
  	}
  	for _, p := range []*corev1.Pod{
  		pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false),
  		pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false),
  	} {
  		if _, err := client.CoreV1().Pods("orders").Create(ctx, p, metav1.CreateOptions{}); err != nil {
  			t.Fatal(err)
  		}
  	}
  	for _, old := range []*corev1.Pod{
  		pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true),
  		pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true),
  	} {
  		update(t, client, old)
  		if err := client.CoreV1().Pods("orders").Delete(ctx, old.Name, metav1.DeleteOptions{}); err != nil {
  			t.Fatal(err)
  		}
  	}
  	update(t, client, deployment("orders", 2, 2, 2, 2, 2, 2))
  	until(t, w, clock, func(s Transition) bool {
  		return len(s.Workloads) == 1 && s.Workloads[0].Replaced == 2 && s.Workloads[0].Created == 2
  	})
  	return exited, res
  }

  func TestFinishesAsTheyHappen(t *testing.T) {
  	w, client, clock := startWatcher(t, nil, base()...)
  	exited, res := startDeploy(t, w, client, clock)
  	close(exited)
  	var f1, f2 time.Time
  	until(t, w, clock, func(s Transition) bool { f1 = s.Finished; return !f1.IsZero() })
  	clock.Advance(5 * time.Second)
  	if _, err := client.CoreV1().Pods("orders").Create(t.Context(),
  		pod("orders-b-3", "b3", "ReplicaSet", "orders-b", true, false), metav1.CreateOptions{}); err != nil {
  		t.Fatal(err)
  	}
  	until(t, w, clock, func(s Transition) bool { f2 = s.Finished; return f2.Equal(clock.Now()) })

  	tr := drive(t, clock, res) // nobody has read Finishes yet: the watch was not held up
  	if !tr.Finished.Equal(f2) || len(tr.SettleRestarts) != 1 {
  		t.Errorf("finished %v with %d settle restarts; want %v and 1", tr.Finished, len(tr.SettleRestarts), f2)
  	}
  	for _, want := range []Finish{{Name: verdict.Deploy, At: f1}, {Name: verdict.Deploy, At: f2}} {
  		select {
  		case got := <-w.Finishes():
  			if got.Name != want.Name || !got.At.Equal(want.At) {
  				t.Errorf("finish %+v, want %+v", got, want)
  			}
  		case <-time.After(time.Second):
  			t.Fatalf("no finish %+v within 1 s", want)
  		}
  	}
  	select {
  	case got := <-w.Finishes():
  		t.Errorf("a third finish %+v", got)
  	case <-time.After(200 * time.Millisecond):
  	}
  }

  func TestFinishesIsClosedByStop(t *testing.T) {
  	w, _, _ := startWatcher(t, nil, base()...)
  	w.Stop()
  	select {
  	case _, ok := <-w.Finishes():
  		if ok {
  			t.Error("a value came from Finishes after Stop")
  		}
  	case <-time.After(time.Second):
  		t.Fatal("Finishes was not closed within 1 s of Stop")
  	}
  }

  func TestPodListsBeforeAndAfter(t *testing.T) {
  	ev := newEvidence()
  	w, client, clock := startWatcherWith(t, func(o *Options) { o.Evidence = ev.open }, nil, base()...)
  	exited, res := startDeploy(t, w, client, clock)
  	close(exited)
  	drive(t, clock, res)
  	if d := cmp.Diff([]string{"pods/deploy-after.json", "pods/deploy-before.json"}, ev.names()); d != "" {
  		t.Errorf("evidence files (-want +got):\n%s", d)
  	}
  	for name, want := range map[string][]string{
  		"pods/deploy-before.json": {"orders-a-1", "orders-a-2"},
  		"pods/deploy-after.json":  {"orders-b-1", "orders-b-2"},
  	} {
  		var list corev1.PodList
  		if err := json.Unmarshal([]byte(ev.text(t, name)), &list); err != nil {
  			t.Fatalf("%s: %v", name, err)
  		}
  		if list.Kind != "PodList" || list.APIVersion != "v1" {
  			t.Errorf("%s is a %s of %s, want a PodList of v1", name, list.Kind, list.APIVersion)
  		}
  		var got []string
  		for _, p := range list.Items {
  			got = append(got, p.Name)
  		}
  		if d := cmp.Diff(want, got); d != "" {
  			t.Errorf("%s (-want +got):\n%s", name, d)
  		}
  	}
  }
  ```

  The existing tests run with `Evidence` nil and must stay green: a nil `Evidence` writes nothing.

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/watch/ -run 'TestFinishes|TestPodLists' -v` / Expected: FAIL to build: `Finishes`, `Finish` and `Options.Evidence` are not defined.

- [ ] **Step 3: Implement `Finish`, `Finishes`, `Options.Evidence` and the pod lists**, in `internal/watch/marks.go` and `internal/watch/watcher.go`, with the interface comments from the Interfaces block; rewrite the comments of `Transition` and `Stop`. After each call into the Tracker named under **Files**, still under `w.mu`, the Watcher compares the open transition's `Finished` with the last finish it queued for that transition, and queues a `Finish` when it is a new moment that is not zero. The queue is a slice under its own lock, which a goroutine of the Watcher hands to the channel one value at a time until Stop; Stop ends that goroutine and closes the channel before it returns. The pod lists copy the pods under `w.mu` and encode and write them after the lock is released, with `json.MarshalIndent` at two spaces and a final newline. In `cmd/rollproof/wire.go`, `session.Watcher` sets `Evidence: s.evidence`.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test -race ./internal/watch/ -v` / Expected: PASS.
  Run: `go build ./...` and `go vet ./...` / Expected: no output.

- [ ] **Step 5: Commit**
  `git add internal/watch/marks.go internal/watch/marks_test.go internal/watch/watcher.go internal/watch/tracker.go cmd/rollproof/wire.go` and `git commit -m "Let watch tell each finish of a transition as it happens and write the namespace's pods before and after each transition"`

### Task 7: Pod logs, events, and the pods' history

Design: section 10, "Evidence folder" (the events of both namespaces; the logs of every pod of a watched workload, followed while the pod lives, with the previous run of a restarted container; the per-second timeline's pod events); section 5 (`watch` follows the pods' status and logs and gathers the events); README decisions 12, 14 and 15.

**Files:**
- Create: `internal/watch/podlogs.go` (the followers), `internal/watch/events.go` (`WriteEvents`, the event's time)
- Modify: `internal/watch/watcher.go` (`Options.ProbeNamespace`; followers started from `set`, for a pod and for the pods of a ReplicaSet; `Stop` waits for the followers; `PodEvents`), `internal/watch/tracker.go` (an unexported reading of the pod records for `PodEvents`), `cmd/rollproof/wire.go` (`session.Watcher` passes `ProbeNamespace: cluster.ProbeNamespace`)
- Test: new `internal/watch/podlogs_test.go`, new `internal/watch/events_test.go`

**Interfaces:**
- Consumes: `Options.Evidence`, and the test helpers `newEvidence`, `evidenceFiles` and `startDeploy` (Task 6); `startWatcherWith` (Task 5).
- Produces: `Options.Evidence func(name string) (io.WriteCloser, error)`
  As in Task 6; the Watcher also writes through it the log of each
  container of every watched pod, followed from first sight until the pod
  is gone, and a restarted container's previous run once (decision 15). A
  log that could not be read to its end ends with a line beginning
  "rollproof:" saying what was not read and why. An unopenable file is
  skipped.
- Produces: `Options.ProbeNamespace string`
  The probe's namespace, whose events WriteEvents writes beside those of
  Namespace; the tool passes rollproof-probe. Empty writes Namespace's alone.
- Produces: `type PodEvent struct { At time.Time; Pod, Workload string; Kind string }`
  One moment in a pod's life as the watch saw it. Kind is created (the
  watch's first sight of the pod), ready (first ready), deleted (the first
  sight of its deletion) or gone. Workload is the pod's workload, as
  deployment/orders, empty when the watch knows none.
- Produces: `func (w *Watcher) PodEvents() []PodEvent`
  Every PodEvent of every pod of the namespace so far, in order of time,
  then of pod name, then of kind in the order created, ready, deleted, gone.
  A pod known only from its disappearance has a gone alone. The slice is a
  copy; safe to call at any time, also after Stop.
- Produces: `func (w *Watcher) WriteEvents(ctx context.Context, out io.Writer) error`
  Lists the events of Namespace and of ProbeNamespace once and writes them,
  oldest first, one line each, tab-separated: the time in RFC 3339 with
  milliseconds in UTC, the namespace, the type, the reason, the object as
  kind/name in lower case, the count, and the message with line breaks as
  spaces. It writes what it could list; the error names each namespace whose
  list failed, or the failed write. It names no file; the caller bounds ctx.
- Produces: `func (w *Watcher) Stop()`
  Stops following, ends every pod log's follower and returns when each has
  closed its file. It is safe to call twice.

- [ ] **Step 1: Write the failing tests**

  `podlogs_test.go`, with the imports `errors`, `fmt`, `strings`, `testing`, `time`, go-cmp's `cmp`, `appsv1 "k8s.io/api/apps/v1"`, `corev1`, `metav1`, `runtime`, `k8s.io/client-go/kubernetes/fake` and `k8stesting "k8s.io/client-go/testing"`:

  ```go
  // logPod is pod() with the given containers in its spec, each with a status: terminated with
  // exit code 0 after restarts restarts. A terminated container's log is read once.
  func logPod(name, uid, ownerKind, ownerName string, restarts int32, containers ...string) *corev1.Pod {
  	p := pod(name, uid, ownerKind, ownerName, true, false)
  	for _, c := range containers {
  		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: c})
  		p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, corev1.ContainerStatus{Name: c, RestartCount: restarts,
  			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed"}}})
  	}
  	return p
  }

  // waiting is p with each container waiting to start.
  func waiting(p *corev1.Pod) *corev1.Pod {
  	for i := range p.Status.ContainerStatuses {
  		p.Status.ContainerStatuses[i].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}
  	}
  	return p
  }

  // serveLogs answers every GetLogs of client with "log of CONTAINER\n", or "previous run of
  // CONTAINER\n" when the options ask for the previous run; with fail set, it fails each with fail.
  // The fake's log action holds no pod name, so the text names the container only.
  func serveLogs(client *fake.Clientset, fail error) {
  	client.PrependReactor("get", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
  		if a.GetSubresource() != "log" {
  			return false, nil, nil
  		}
  		if fail != nil {
  			return true, nil, fail
  		}
  		o := a.(k8stesting.GenericAction).GetValue().(*corev1.PodLogOptions)
  		text := "log of " + o.Container + "\n"
  		if o.Previous {
  			text = "previous run of " + o.Container + "\n"
  		}
  		return true, &runtime.Unknown{Raw: []byte(text)}, nil
  	})
  }

  // logActions counts the GetLogs the client has been asked for.
  func logActions(client *fake.Clientset) int {
  	n := 0
  	for _, a := range client.Actions() {
  		if a.GetSubresource() == "log" {
  			n++
  		}
  	}
  	return n
  }

  // create stores Deployments, ReplicaSets and Pods in the fake client, in order.
  func create(t *testing.T, client *fake.Clientset, objs ...runtime.Object) {
  	t.Helper()
  	var err error
  	for _, obj := range objs {
  		switch o := obj.(type) {
  		case *appsv1.Deployment:
  			_, err = client.AppsV1().Deployments("orders").Create(t.Context(), o, metav1.CreateOptions{})
  		case *appsv1.ReplicaSet:
  			_, err = client.AppsV1().ReplicaSets("orders").Create(t.Context(), o, metav1.CreateOptions{})
  		case *corev1.Pod:
  			_, err = client.CoreV1().Pods("orders").Create(t.Context(), o, metav1.CreateOptions{})
  		default:
  			t.Fatalf("create: %T", obj)
  		}
  		if err != nil {
  			t.Fatal(err)
  		}
  	}
  }

  func deletePod(t *testing.T, client *fake.Clientset, name string) {
  	t.Helper()
  	if err := client.CoreV1().Pods("orders").Delete(t.Context(), name, metav1.DeleteOptions{}); err != nil {
  		t.Fatal(err)
  	}
  }

  // watchLogs starts a Watcher on an empty namespace with the selector app=orders and an evidence
  // recorder, and has the client serve logs (or fail them with fail) before any object exists.
  func watchLogs(t *testing.T, fail error) (*Watcher, *fake.Clientset, *evidenceFiles) {
  	t.Helper()
  	ev := newEvidence()
  	w, client, _ := startWatcherWith(t, func(o *Options) { o.Selector = "app=orders"; o.Evidence = ev.open }, nil)
  	serveLogs(client, fail)
  	return w, client, ev
  }

  // seen waits up to 5 s of real time until the Watcher has seen the pod name created.
  func seen(t *testing.T, w *Watcher, name string) {
  	t.Helper()
  	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
  		for _, e := range w.PodEvents() {
  			if e.Pod == name && e.Kind == "created" {
  				return
  			}
  		}
  		if time.Now().After(deadline) {
  			t.Fatalf("the Watcher did not see pod %s within 5 s", name)
  		}
  	}
  }

  // A3: the pod's workload may become known after the pod, when the ReplicaSet's Set comes second.
  func TestPodLogsOfWatchedPods(t *testing.T) {
  	orders, rs := deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders")
  	pa := logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app", "sidecar")
  	for _, tc := range []struct {
  		name  string
  		order []runtime.Object
  	}{
  		{"the ReplicaSet first", []runtime.Object{orders, rs, pa}},
  		{"the pod first", []runtime.Object{orders, pa}},
  	} {
  		t.Run(tc.name, func(t *testing.T) {
  			w, client, ev := watchLogs(t, nil)
  			create(t, client, tc.order...)
  			if len(tc.order) == 2 {
  				seen(t, w, "orders-a-1") // a pod whose ReplicaSet the Watcher does not know yet
  				create(t, client, rs)
  			}
  			create(t, client, deployment("batch", 1, 1, 1, 1, 1, 1), replicaSet("batch-a", "batch"),
  				logPod("batch-a-1", "k1", "ReplicaSet", "batch-a", 0, "app"))
  			ev.holds(t, "pods/orders-a-1/app.log", "log of app\n")
  			ev.holds(t, "pods/orders-a-1/sidecar.log", "log of sidecar\n")
  			deletePod(t, client, "orders-a-1")
  			if got := ev.text(t, "pods/orders-a-1/app.log"); got != "log of app\n" {
  				t.Errorf("app.log %q", got)
  			}
  			if got := ev.text(t, "pods/orders-a-1/sidecar.log"); got != "log of sidecar\n" {
  				t.Errorf("sidecar.log %q", got)
  			}
  			for _, n := range ev.names() {
  				if strings.HasPrefix(n, "pods/batch-a-1/") || strings.HasSuffix(n, ".previous.log") {
  					t.Errorf("evidence file %s, want none for batch-a-1 and no previous run", n)
  				}
  			}
  		})
  	}
  }

  func TestPodLogsKeepThePreviousRun(t *testing.T) {
  	_, client, ev := watchLogs(t, nil)
  	create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
  		logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 1, "app"))
  	if got := ev.text(t, "pods/orders-a-1/app.previous.log"); got != "previous run of app\n" {
  		t.Errorf("app.previous.log %q", got)
  	}
  	ev.holds(t, "pods/orders-a-1/app.log", "log of app\n")
  	deletePod(t, client, "orders-a-1")
  	if got := ev.text(t, "pods/orders-a-1/app.log"); got != "log of app\n" {
  		t.Errorf("app.log %q", got)
  	}
  }

  func TestPodLogsWaitForTheContainerToStart(t *testing.T) {
  	_, client, ev := watchLogs(t, nil)
  	create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
  		waiting(logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app")))
  	time.Sleep(500 * time.Millisecond)
  	if n := logActions(client); n != 0 {
  		t.Errorf("%d GetLogs for a container that has not started", n)
  	}
  	update(t, client, logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app"))
  	ev.holds(t, "pods/orders-a-1/app.log", "log of app\n")
  	deletePod(t, client, "orders-a-1")
  	if got := ev.text(t, "pods/orders-a-1/app.log"); got != "log of app\n" {
  		t.Errorf("app.log %q", got)
  	}
  }

  // Review Focus 4: a log that cannot be read is told in its own file.
  func TestPodLogsOfAPodGoneBeforeItsLogWasRead(t *testing.T) {
  	const prefix = "rollproof: the log of container app could not be read: "
  	notFound := errors.New(`container "app" not found`)

  	t.Run("the container never started", func(t *testing.T) {
  		_, client, ev := watchLogs(t, nil)
  		create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
  			waiting(logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app")))
  		time.Sleep(500 * time.Millisecond)
  		deletePod(t, client, "orders-a-1")
  		if got, want := ev.text(t, "pods/orders-a-1/app.log"), prefix+"the pod was gone before the container started\n"; got != want {
  			t.Errorf("app.log %q, want %q", got, want)
  		}
  	})

  	for _, restarts := range []int32{0, 1} {
  		t.Run(fmt.Sprintf("every opening fails, %d restarts", restarts), func(t *testing.T) {
  			_, client, ev := watchLogs(t, notFound)
  			create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
  				logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", restarts, "app"))
  			time.Sleep(500 * time.Millisecond)
  			deletePod(t, client, "orders-a-1")
  			files := map[string]string{"pods/orders-a-1/app.log": prefix}
  			if restarts > 0 {
  				files["pods/orders-a-1/app.previous.log"] = "rollproof: the previous run of container app could not be read: "
  			}
  			for name, start := range files {
  				got := ev.text(t, name)
  				if !strings.HasPrefix(got, start) || !strings.Contains(got, notFound.Error()) ||
  					strings.Count(got, "\n") != 1 || !strings.HasSuffix(got, "\n") {
  					t.Errorf("%s %q, want one line that begins %q and holds %q", name, got, start, notFound)
  				}
  			}
  		})
  	}
  }

  func TestStopClosesThePodLogs(t *testing.T) {
  	w, client, ev := watchLogs(t, nil)
  	create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
  		logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app", "sidecar"))
  	ev.holds(t, "pods/orders-a-1/app.log", "log of app\n")
  	ev.holds(t, "pods/orders-a-1/sidecar.log", "log of sidecar\n")
  	w.Stop()
  	for _, n := range ev.names() {
  		if text, _, closed := ev.state(n); !closed {
  			t.Errorf("%s is open when Stop has returned (it holds %q)", n, text)
  		}
  	}
  }

  func TestPodEvents(t *testing.T) {
  	w, client, clock := startWatcher(t, nil, base()...)
  	exited, res := startDeploy(t, w, client, clock)
  	close(exited)
  	drive(t, clock, res)
  	end := clock.Now()
  	loose := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "loose-1", Namespace: "orders", UID: "l1"}}
  	create(t, client, loose)
  	wl := "deployment/orders"
  	want := []PodEvent{
  		{t0, "orders-a-1", wl, "created"}, {t0, "orders-a-1", wl, "ready"},
  		{t0, "orders-a-2", wl, "created"}, {t0, "orders-a-2", wl, "ready"},
  		{at(1), "orders-a-1", wl, "deleted"}, {at(1), "orders-a-1", wl, "gone"},
  		{at(1), "orders-a-2", wl, "deleted"}, {at(1), "orders-a-2", wl, "gone"},
  		{at(1), "orders-b-1", wl, "created"}, {at(1), "orders-b-1", wl, "ready"},
  		{at(1), "orders-b-2", wl, "created"}, {at(1), "orders-b-2", wl, "ready"},
  		{end, "loose-1", "", "created"}, // no owner, so no workload; no Ready condition, so no ready
  	}
  	var got []PodEvent
  	for deadline := time.Now().Add(5 * time.Second); len(got) < len(want) && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
  		got = w.PodEvents()
  	}
  	if d := cmp.Diff(want, got); d != "" {
  		t.Errorf("PodEvents (-want +got):\n%s", d)
  	}
  }
  ```

  `events_test.go`, with the imports `errors`, `strings`, `testing`, `time`, `corev1`, `metav1`, `runtime` and `k8stesting "k8s.io/client-go/testing"` (Task 8 adds `rollproof/internal/verdict`):

  ```go
  // event is a core/v1 Event about KIND/NAME in ns, counted once, with the given time fields
  // (a zero time leaves its field unset).
  func event(ns, name, kind, object, typ, reason, message string, last, eventTime, first time.Time) *corev1.Event {
  	e := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
  		InvolvedObject: corev1.ObjectReference{Kind: kind, Name: object, Namespace: ns},
  		Type:           typ, Reason: reason, Message: message, Count: 1}
  	if !last.IsZero() {
  		e.LastTimestamp = metav1.NewTime(last)
  	}
  	if !eventTime.IsZero() {
  		e.EventTime = metav1.NewMicroTime(eventTime)
  	}
  	if !first.IsZero() {
  		e.FirstTimestamp = metav1.NewTime(first)
  	}
  	return e
  }

  // eventObjects: two events of orders, one of the probe's namespace and one of another, each with
  // its time in a different field.
  func eventObjects() []runtime.Object {
  	return append(base(),
  		event("orders", "e1", "Pod", "orders-b-1", "Warning", "FailedScheduling",
  			"0/4 nodes are available:\n4 Insufficient cpu.", at(2), time.Time{}, time.Time{}),
  		event("orders", "e2", "Deployment", "orders", "Normal", "ScalingReplicaSet",
  			"Scaled up replica set orders-b from 0 to 1", time.Time{}, t0, time.Time{}),
  		event("rollproof-probe", "e3", "Pod", "probe", "Normal", "Pulled",
  			"Container image already present on machine", time.Time{}, time.Time{}, at(1)),
  		event("elsewhere", "e4", "Pod", "x", "Normal", "Pulled", "not ours", at(3), time.Time{}, time.Time{}))
  }

  const (
  	scaledLine = "2026-11-13T10:00:00.000Z\torders\tNormal\tScalingReplicaSet\tdeployment/orders\t1\tScaled up replica set orders-b from 0 to 1\n"
  	pulledLine = "2026-11-13T10:00:01.000Z\trollproof-probe\tNormal\tPulled\tpod/probe\t1\tContainer image already present on machine\n"
  	failedLine = "2026-11-13T10:00:02.000Z\torders\tWarning\tFailedScheduling\tpod/orders-b-1\t1\t0/4 nodes are available: 4 Insufficient cpu.\n"
  )

  func TestWriteEvents(t *testing.T) {
  	w, _, _ := startWatcherWith(t, func(o *Options) { o.ProbeNamespace = "rollproof-probe" }, nil, eventObjects()...)
  	var b strings.Builder
  	if err := w.WriteEvents(t.Context(), &b); err != nil {
  		t.Fatal(err)
  	}
  	if got, want := b.String(), scaledLine+pulledLine+failedLine; got != want {
  		t.Errorf("events:\n%s\nwant:\n%s", got, want)
  	}
  }

  func TestWriteEventsWhenAListFails(t *testing.T) {
  	w, client, _ := startWatcherWith(t, func(o *Options) { o.ProbeNamespace = "rollproof-probe" }, nil, eventObjects()...)
  	client.PrependReactor("list", "events", func(a k8stesting.Action) (bool, runtime.Object, error) {
  		if a.GetNamespace() == "rollproof-probe" {
  			return true, nil, errors.New("connection refused")
  		}
  		return false, nil, nil
  	})
  	var b strings.Builder
  	err := w.WriteEvents(t.Context(), &b)
  	if err == nil || !strings.Contains(err.Error(), "rollproof-probe") || !strings.Contains(err.Error(), "connection refused") {
  		t.Errorf("error %v, want one that names rollproof-probe and holds connection refused", err)
  	}
  	if got, want := b.String(), scaledLine+failedLine; got != want {
  		t.Errorf("events:\n%s\nwant:\n%s", got, want)
  	}
  }
  ```

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/watch/ -run 'TestPodLogs|TestStopClosesThePodLogs|TestPodEvents|TestWriteEvents' -v` / Expected: FAIL to build: `PodEvent`, `PodEvents`, `WriteEvents` and `Options.ProbeNamespace` are not defined.

- [ ] **Step 3: Implement the followers in `internal/watch/podlogs.go`, `WriteEvents` and the event's time in `internal/watch/events.go`, and `PodEvents`, `Options.ProbeNamespace` and `Stop` in `internal/watch/watcher.go`**, with the interface comments from the Interfaces block. The pod log's rule (the probe's follower in `internal/probe/follow.go` does not fit: it parses probe lines and judges gaps):
  - A pod is followed from the first moment the watch knows it as a pod of a workload whose labels matched at its last Set: the pod's own Set, or, when the pod came before its ReplicaSet, the ReplicaSet's Set, which starts a follower for each pod of that ReplicaSet the watch holds that is neither followed nor gone. One follower for each container of its spec, init containers first. Followers run on a context that Stop cancels; Stop waits for them after the informers have ended.
  - A follower waits, every `pollEvery`, until the watch's last object of the pod shows the container running or terminated. It ends when the pod is gone.
  - At that first sight, a container whose restart count is above 0 has its previous run read once (`PodLogOptions{Container, Previous: true, Timestamps: true}`) into `pods/POD/CONTAINER.previous.log`. When that opening fails, the file holds the one line `rollproof: the previous run of container C could not be read: ERROR`.
  - It then follows the current run (`Follow: true, Timestamps: true`) into `pods/POD/CONTAINER.log`, opened once for the pod's life, at the first stream or at the failure line below, whichever comes first. When a stream ends while the pod is not gone, it waits `pollEvery` and reads the pod's last object: a higher restart count than at the stream's opening writes the line `rollproof: the container started again (restart N)` and opens the new run from its start; the same run, still running, writes `rollproof: the log was opened again from TIME; lines of that second may repeat` and opens it with `SinceTime` at the second of the last line's timestamp; a terminated run that has not restarted waits again. An opening that fails is tried again after `pollEvery` while the pod is not gone.
  - When the pod is gone and the follower has not read the current run to its end (no stream opened, or the last opening failed), it writes a final line and closes the file: `rollproof: the log of container C could not be read: WHY`, WHY being the last opening's error, with line breaks as spaces, or `the pod was gone before the container started`. A Stop ends the file with no such line: the evidence ends where the run did.
  - Every name is given in full (`pods/POD/CONTAINER.log`, and Task 6's `pods/NAME-before.json`); `wire` passes the session's evidence function with no prefix. A file that `Evidence` cannot open is skipped: `watch` has no channel for warnings.
  - An event's time is `LastTimestamp`, else `EventTime`, else `FirstTimestamp`, else the creation time; ties sort by namespace, then by the event's name. Task 8 reuses this.

  In `cmd/rollproof/wire.go`, `session.Watcher` sets `ProbeNamespace: cluster.ProbeNamespace`.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test -race ./internal/watch/ -v` / Expected: PASS.
  Run: `go build ./...` and `go vet ./...` / Expected: no output.

- [ ] **Step 5: Commit**
  `git add internal/watch/podlogs.go internal/watch/podlogs_test.go internal/watch/events.go internal/watch/events_test.go internal/watch/watcher.go internal/watch/tracker.go cmd/rollproof/wire.go` and `git commit -m "Let watch follow the logs of watched pods into the evidence, list the events of both namespaces and give the pods' history"`

### Task 8: The last event after a stall's details

Design: section 10, "What a run writes", the console item on a stall (each workload that is not complete and each pod that is not ready; "The last event message follows them; Kubernetes keeps events as best effort, and for one hour by default"); section 7, "When a transition has finished" (the stall); README decision 12.

**Files:**
- Modify: `internal/watch/tracker.go` (`Transition.LastEvent`; an unexported reading of what a stall names), `internal/watch/events.go` (the last event of those objects), `internal/watch/watcher.go` (`Transition` reads it at a stall), `internal/run/record.go` (`reasonRecord`)
- Test: `internal/watch/events_test.go` (new test), `internal/run/record_test.go` (`TestRecordStallDetail`)

**Interfaces:**
- Consumes: the event's time and `event` (Task 7).
- Produces: `Transition.LastEvent string`
  The last event of the namespace about what a stall names, when the window
  stalled: a watched workload that is not complete, a ReplicaSet of such a
  Deployment, or a pod of one that is not ready or is stopping, written
  KIND/NAME: REASON: MESSAGE with kind in lower case and line breaks as
  spaces. Empty when it did not stall, no such event exists, or the events
  could not be listed in the settle time.
- Produces: `func reasonRecord(reason verdict.Reason, trs []transitionFacts) report.Reason`
  As before; the detail of a transition-stalled reason is the transition's
  Incomplete lines and then, when its LastEvent is not empty, the final line
  "the last event: " and LastEvent. `Transition.Conditions` and the stall's
  text are unchanged.

- [ ] **Step 1: Write the failing tests**

  ```go
  // events_test.go, which now also imports rollproof/internal/verdict
  func TestTransitionStallNamesTheLastEvent(t *testing.T) {
  	for _, tc := range []struct {
  		name   string
  		events []runtime.Object
  		want   string
  	}{
  		{"the pod's event is the last", []runtime.Object{
  			event("orders", "e1", "Deployment", "orders", "Normal", "ScalingReplicaSet", "Scaled up replica set orders-b from 0 to 1", at(1), time.Time{}, time.Time{}),
  			event("orders", "e2", "Pod", "orders-b-1", "Warning", "FailedScheduling", "0/4 nodes are available:\n4 Insufficient cpu.", at(2), time.Time{}, time.Time{}),
  			event("orders", "e3", "Pod", "unrelated-1", "Warning", "BackOff", "not named by the stall", at(3), time.Time{}, time.Time{}),
  			event("elsewhere", "e4", "Pod", "orders-b-1", "Warning", "BackOff", "another namespace", at(4), time.Time{}, time.Time{}),
  		}, "pod/orders-b-1: FailedScheduling: 0/4 nodes are available: 4 Insufficient cpu."},
  		{"the ReplicaSet's event is the last", []runtime.Object{
  			event("orders", "e2", "Pod", "orders-b-1", "Warning", "FailedScheduling", "0/4 nodes are available", at(2), time.Time{}, time.Time{}),
  			event("orders", "e5", "ReplicaSet", "orders-b", "Warning", "FailedCreate", `pods "orders-b-2" is forbidden: violates PodSecurity`, at(5), time.Time{}, time.Time{}),
  		}, `replicaset/orders-b: FailedCreate: pods "orders-b-2" is forbidden: violates PodSecurity`},
  		{"no event names it", nil, ""},
  	} {
  		t.Run(tc.name, func(t *testing.T) {
  			w, client, clock := startWatcher(t, nil, append(base(), tc.events...)...)
  			ctx := t.Context()
  			exited := make(chan struct{})
  			res := make(chan Transition, 1)
  			go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), exited) }()
  			until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
  			update(t, client, deployment("orders", 2, 2, 2, 0, 2, 3)) // one new pod wanted, none updated
  			if _, err := client.AppsV1().ReplicaSets("orders").Create(ctx, replicaSet("orders-b", "orders"), metav1.CreateOptions{}); err != nil {
  				t.Fatal(err)
  			}
  			if _, err := client.CoreV1().Pods("orders").Create(ctx, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false), metav1.CreateOptions{}); err != nil {
  				t.Fatal(err)
  			}
  			close(exited)
  			tr := drive(t, clock, res)
  			if !tr.Stalled || tr.LastEvent != tc.want {
  				t.Errorf("stalled %v, last event %q; want a stall and %q", tr.Stalled, tr.LastEvent, tc.want)
  			}
  			if got := tr.Conditions(true)[0].Text; got != "did not finish within limits.timeout" {
  				t.Errorf("the stall's text %q changed", got)
  			}
  		})
  	}
  }
  ```

  In `internal/run/record_test.go`, `TestRecordStallDetail`, after the check of pod-unschedulable's detail and before the nothing-watched case, add (the file gains the import `slices`):

  ```go
  	const last = "pod/orders-7f8e9d-k3m2p: FailedScheduling: 0/4 nodes are available..."
  	f.transitions[0].tr.LastEvent = last
  	got = record(f)
  	if diff := cmp.Diff(append(slices.Clone(incomplete), "the last event: "+last), got.Reasons[0].Detail); diff != "" {
  		t.Errorf("detail of transition-stalled with a last event (-want +got):\n%s", diff)
  	}
  	if got.Reasons[0].Text != "did not finish within limits.timeout" {
  		t.Errorf("the stall's text %q changed", got.Reasons[0].Text)
  	}
  	if d := got.Reasons[1].Detail; len(d) != 0 {
  		t.Errorf("detail of pod-unschedulable is %q, want none", d)
  	}
  	f.transitions[0].tr.LastEvent = ""
  ```

  The check before it, with `LastEvent` empty, already wants `incomplete` alone.

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/watch/ ./internal/run/ -run 'TestTransitionStallNamesTheLastEvent|TestRecordStallDetail' -v` / Expected: FAIL to build: `Transition.LastEvent` is not defined.

- [ ] **Step 3: Add `LastEvent`, fill it in `Watcher.Transition`, and change `reasonRecord`**, with the interface comments from the Interfaces block. In `Watcher.Transition`, when `follow` returns a window that stalled and ctx has not ended, take under `w.mu` the objects the stall names (the workloads that take part and are not complete, or are deleted with pods left; the ReplicaSets the Tracker maps to such a Deployment; their pods not gone that are not ready or are stopping), then, outside the lock and bounded by the settle time as the last reading is, list the namespace's events once and keep the latest whose involved object's kind and name are among them. Add `LastEvent` to the comment of `Transition`. In `reasonRecord`, the `TransitionStalled` case appends `"the last event: " + t.tr.LastEvent` after `t.tr.Incomplete` when it is not empty.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test -race ./internal/watch/ ./internal/run/ -v` / Expected: PASS.

- [ ] **Step 5: Commit**
  `git add internal/watch/tracker.go internal/watch/events.go internal/watch/events_test.go internal/watch/watcher.go internal/run/record.go internal/run/record_test.go` and `git commit -m "Follow a stall's details with the last event about the workloads and pods it names"`
