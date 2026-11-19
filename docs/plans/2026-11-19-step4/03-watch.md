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
  Sets the pod template annotation kubectl.kubernetes.io/restartedAt, to the
  clock's time in RFC 3339, by a strategic merge patch on every workload that
  exists and matches the selector when it is called, one at a time in the
  order of kind and then name. The first refusal ends it; the workloads after
  it are not changed. No workload is Accepted. When ctx ends first, Accepted
  is false and Text is ctx's error: the caller tells that from a refusal.

- [ ] **Step 1: Write the failing tests**

  In `watcher_test.go`, `startWatcher` keeps its signature and calls `startWatcherWith(t, nil, service, objs...)`, which takes an `edit func(*Options)` applied to the Options before `Start`.

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

  const restartPatch = `{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"2026-11-13T10:00:00Z"}}}}}`

  // patches gives each patch the fake client recorded, as RESOURCE/NAME, and fails the test unless
  // every one is a strategic merge patch whose body is restartPatch.
  func patches(t *testing.T, client *fake.Clientset) []string

  func TestRestartChangesEveryWatchedWorkload(t *testing.T) {
  	w, client, _ := startWatcherWith(t, func(o *Options) { o.Selector = "app!=batch" }, nil, restartObjects()...)
  	got := w.Restart(t.Context())
  	// cmp.Diff(Restart{Began: t0, Ended: t0, Accepted: true}, got) is empty;
  	// patches(t, client) is {"daemonsets/logs", "deployments/api", "deployments/orders", "statefulsets/web"};
  	// the Deployment orders read back from the client has
  	//   Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "2026-11-13T10:00:00Z".
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
  			errors.New("admission webhook \"policy.example\" denied the request"))
  	})
  	got := w.Restart(t.Context())
  	// got.Accepted == false, got.Workload == "deployment/orders", got.Began == t0, got.Ended == at(2);
  	// got.Text holds `admission webhook "policy.example" denied the request`;
  	// patches(t, client) is {"daemonsets/logs", "deployments/api", "deployments/orders"}: web is not patched;
  	// daemonset/logs and deployment/api read back carry the annotation, so their controllers roll them.
  }

  func TestRestartWhenCtxHasEnded(t *testing.T) {
  	// ctx cancelled before the call: Accepted false, Text == context.Canceled.Error(), Workload "",
  	// and patches(t, client) is empty.
  }

  func TestRestartWithNothingToRestart(t *testing.T) {
  	// a namespace with no workload: Restart{Began: t0, Ended: t0, Accepted: true}, and no patch.
  }
  ```

  The window's verdict for a refusal, `restart-failed` with the server's text and the workload's name, is `run`'s (Task 14); this task gives `run` the facts.

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/watch/ -run 'TestRestart' -v` / Expected: FAIL to build: `Restart` and `startWatcherWith` are not defined.

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

  ```go
  // evidenceFiles records what a Watcher writes through Options.Evidence.
  type evidenceFiles struct {
  	mu    sync.Mutex
  	files map[string]*recordedFile // by name
  }
  type recordedFile struct {
  	bytes.Buffer
  	closed bool
  }
  func (e *evidenceFiles) open(name string) (io.WriteCloser, error)
  // text waits up to 5 s of real time until the file name exists and is closed, and returns its text.
  func (e *evidenceFiles) text(t *testing.T, name string) string
  // names lists the names opened so far, sorted.
  func (e *evidenceFiles) names() []string

  func TestFinishesAsTheyHappen(t *testing.T) {
  	w, client, clock := startWatcher(t, nil, base()...)
  	// Drive a deploy as TestTransition does, up to close(exited);
  	// until(...) the transition so far has a Finished; f1 is it.
  	clock.Advance(5 * time.Second)
  	// create pod orders-b-3 of ReplicaSet orders-b, ready; until(...) Finished == clock.Now(); f2 is it.
  	tr := drive(t, clock, res) // nobody has read Finishes yet: the watch was not held up
  	// tr.Finished == f2; len(tr.SettleRestarts) == 1;
  	// the next two values received from w.Finishes(), each within 1 s, are
  	//   Finish{Name: verdict.Deploy, At: f1} and Finish{Name: verdict.Deploy, At: f2};
  	// no third value arrives within 200 ms.
  }

  func TestFinishesIsClosedByStop(t *testing.T) {
  	w, _, _ := startWatcher(t, nil, base()...)
  	w.Stop()
  	// a receive from w.Finishes() gives ok == false within 1 s.
  }

  func TestPodListsBeforeAndAfter(t *testing.T) {
  	ev := &evidenceFiles{files: map[string]*recordedFile{}}
  	w, client, clock := startWatcherWith(t, func(o *Options) { o.Evidence = ev.open }, nil, base()...)
  	// Drive the deploy of TestTransition to its end.
  	// ev.names() is {"pods/deploy-after.json", "pods/deploy-before.json"};
  	// each decodes into a corev1.PodList with Kind "PodList" and APIVersion "v1";
  	// the before list's item names are {"orders-a-1", "orders-a-2"}, the after list's {"orders-b-1", "orders-b-2"}.
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
- Modify: `internal/watch/watcher.go` (`Options.ProbeNamespace`; a follower started from `set`; `Stop` waits for the followers; `PodEvents`), `internal/watch/tracker.go` (an unexported reading of the pod records for `PodEvents`), `cmd/rollproof/wire.go` (`session.Watcher` passes `ProbeNamespace: cluster.ProbeNamespace`)
- Test: new `internal/watch/podlogs_test.go`, new `internal/watch/events_test.go`

**Interfaces:**
- Consumes: `Options.Evidence`, `evidenceFiles` (Task 6); `startWatcherWith` (Task 5).
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

  `podlogs_test.go`:

  ```go
  // logPod is pod() with the given containers in its spec, each with a status: terminated with
  // exit code 0 and restartCount restarts. A terminated container's log is read once.
  func logPod(name, uid, ownerKind, ownerName string, restarts int32, containers ...string) *corev1.Pod

  // serveLogs answers every GetLogs of client with "log of CONTAINER\n", or "previous run of
  // CONTAINER\n" when the options ask for the previous run. The fake's log action holds no pod
  // name, so the text names the container only.
  func serveLogs(client *fake.Clientset)

  // logObjects: base's Deployment orders with pod orders-a-1 (containers app and sidecar), and
  // deployment/batch with ReplicaSet batch-a and pod batch-a-1 (container app), which the selector
  // app=orders leaves out.
  func logObjects(restarts int32) []runtime.Object

  func TestPodLogsOfWatchedPods(t *testing.T) {
  	ev := &evidenceFiles{files: map[string]*recordedFile{}}
  	w, client, _ := startWatcherWith(t, func(o *Options) { o.Selector = "app=orders"; o.Evidence = ev.open }, nil)
  	serveLogs(client) // before the objects exist, so that no opening comes first
  	// create logObjects(0) through the client; then delete pod orders-a-1.
  	// ev.text(t, "pods/orders-a-1/app.log") == "log of app\n";
  	// ev.text(t, "pods/orders-a-1/sidecar.log") == "log of sidecar\n";
  	// no name in ev.names() begins with "pods/batch-a-1/" and none ends with ".previous.log".
  }

  func TestPodLogsKeepThePreviousRun(t *testing.T) {
  	// as above with logObjects(1): ev.text(t, "pods/orders-a-1/app.previous.log") == "previous run of app\n",
  	// and ev.text(t, "pods/orders-a-1/app.log") == "log of app\n".
  }

  func TestPodLogsWaitForTheContainerToStart(t *testing.T) {
  	// pod orders-a-1 whose container app is waiting (ContainerCreating): after 500 ms of real time the
  	// client has recorded no action with subresource "log"; after update() with app terminated,
  	// ev.text(t, "pods/orders-a-1/app.log") == "log of app\n" once the pod is deleted.
  }

  // Review Focus 4: a log that cannot be read is told in its own file.
  func TestPodLogsOfAPodGoneBeforeItsLogWasRead(t *testing.T) {
  	// (a) pod orders-a-1 whose container app is waiting and never starts; the pod is deleted:
  	//   ev.text(t, "pods/orders-a-1/app.log") ==
  	//   "rollproof: the log of container app could not be read: the pod was gone before the container started\n";
  	// (b) app terminated, and a reactor that fails every GetLogs with errors.New("container \"app\" not found");
  	//   after 500 ms of real time the pod is deleted:
  	//   ev.text(t, "pods/orders-a-1/app.log") ==
  	//   "rollproof: the log of container app could not be read: container \"app\" not found\n";
  	// (c) as (b) with logObjects(1): app.previous.log ==
  	//   "rollproof: the previous run of container app could not be read: container \"app\" not found\n".
  }

  func TestStopClosesThePodLogs(t *testing.T) {
  	// logObjects(0), the pod not deleted; once both log files are opened, w.Stop();
  	// when Stop returns, every file in ev.files is closed.
  }

  func TestPodEvents(t *testing.T) {
  	w, client, clock := startWatcher(t, nil, base()...)
  	// Drive the deploy of TestTransition to its end, then add pod loose-1 with no owner at the end's moment.
  	wl := "deployment/orders"
  	want := []PodEvent{
  		{t0, "orders-a-1", wl, "created"}, {t0, "orders-a-1", wl, "ready"},
  		{t0, "orders-a-2", wl, "created"}, {t0, "orders-a-2", wl, "ready"},
  		{at(1), "orders-a-1", wl, "deleted"}, {at(1), "orders-a-1", wl, "gone"},
  		{at(1), "orders-a-2", wl, "deleted"}, {at(1), "orders-a-2", wl, "gone"},
  		{at(1), "orders-b-1", wl, "created"}, {at(1), "orders-b-1", wl, "ready"},
  		{at(1), "orders-b-2", wl, "created"}, {at(1), "orders-b-2", wl, "ready"},
  		// then {END, "loose-1", "", "created"}, END being the clock when it was added; a pod with no ready condition has no ready
  	}
  	// cmp.Diff(want, w.PodEvents()) is empty.
  }
  ```

  `events_test.go`:

  ```go
  // event is a core/v1 Event about KIND/NAME in ns, with the given time fields (zero leaves one unset).
  func event(ns, name, kind, object, typ, reason, message string, last, eventTime, first time.Time) *corev1.Event

  func TestWriteEvents(t *testing.T) {
  	objs := append(base(),
  		event("orders", "e1", "Pod", "orders-b-1", "Warning", "FailedScheduling",
  			"0/4 nodes are available:\n4 Insufficient cpu.", at(2), time.Time{}, time.Time{}),
  		event("orders", "e2", "Deployment", "orders", "Normal", "ScalingReplicaSet",
  			"Scaled up replica set orders-b from 0 to 1", time.Time{}, t0, time.Time{}),
  		event("rollproof-probe", "e3", "Pod", "probe", "Normal", "Pulled",
  			"Container image already present on machine", time.Time{}, time.Time{}, at(1)),
  		event("elsewhere", "e4", "Pod", "x", "Normal", "Pulled", "not ours", at(3), time.Time{}, time.Time{}))
  	w, _, _ := startWatcherWith(t, func(o *Options) { o.ProbeNamespace = "rollproof-probe" }, nil, objs...)
  	var b strings.Builder
  	// w.WriteEvents(t.Context(), &b) == nil, and b.String() ==
  	//   "2026-11-13T10:00:00.000Z\torders\tNormal\tScalingReplicaSet\tdeployment/orders\t1\tScaled up replica set orders-b from 0 to 1\n" +
  	//   "2026-11-13T10:00:01.000Z\trollproof-probe\tNormal\tPulled\tpod/probe\t1\tContainer image already present on machine\n" +
  	//   "2026-11-13T10:00:02.000Z\torders\tWarning\tFailedScheduling\tpod/orders-b-1\t1\t0/4 nodes are available: 4 Insufficient cpu.\n"
  	// (event sets Count 1).
  }

  func TestWriteEventsWhenAListFails(t *testing.T) {
  	// the same objects, and a reactor that fails "list" "events" in rollproof-probe with "connection refused":
  	// the two lines of orders are written; the error's text holds "rollproof-probe" and "connection refused".
  }
  ```

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/watch/ -run 'TestPodLogs|TestStopClosesThePodLogs|TestPodEvents|TestWriteEvents' -v` / Expected: FAIL to build: `PodEvent`, `PodEvents`, `WriteEvents` and `Options.ProbeNamespace` are not defined.

- [ ] **Step 3: Implement the followers in `internal/watch/podlogs.go`, `WriteEvents` and the event's time in `internal/watch/events.go`, and `PodEvents`, `Options.ProbeNamespace` and `Stop` in `internal/watch/watcher.go`**, with the interface comments from the Interfaces block. The pod log's rule (the probe's follower in `internal/probe/follow.go` does not fit: it parses probe lines and judges gaps):
  - A pod is followed from the first Set that shows it as a pod of a workload the watch knows and whose labels matched at its last Set. One follower for each container of its spec, init containers first. Followers run on a context that Stop cancels; Stop waits for them after the informers have ended.
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
  // events_test.go
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
  			event("orders", "e5", "ReplicaSet", "orders-b", "Warning", "FailedCreate", "pods \"orders-b-2\" is forbidden: violates PodSecurity", at(5), time.Time{}, time.Time{}),
  		}, `replicaset/orders-b: FailedCreate: pods "orders-b-2" is forbidden: violates PodSecurity`},
  		{"no event names it", nil, ""},
  	} {
  		t.Run(tc.name, func(t *testing.T) {
  			w, client, clock := startWatcher(t, nil, append(base(), tc.events...)...)
  			// A deploy that stalls: Transition started; deployment("orders", 2, 2, 2, 0, 2, 3), ReplicaSet orders-b
  			// and pod orders-b-1, not ready, created through the client; close the exited channel; drive to the end.
  			// tr.Stalled is true and tr.LastEvent == tc.want;
  			// tr.Conditions(true)[0].Text == "did not finish within limits.timeout" in every case.
  		})
  	}
  }
  ```

  In `internal/run/record_test.go`, `TestRecordStallDetail` sets `f.transitions[0].tr.LastEvent = "pod/orders-7f8e9d-k3m2p: FailedScheduling: 0/4 nodes are available..."` beside `Incomplete`, and wants as the stall's detail, in this order:

  ```go
  want := append(slices.Clone(incomplete),
  	"the last event: pod/orders-7f8e9d-k3m2p: FailedScheduling: 0/4 nodes are available...")
  // cmp.Diff(want, got.Reasons[0].Detail) is empty; got.Reasons[0].Text is still
  // "did not finish within limits.timeout"; pod-unschedulable's detail is still empty.
  ```

  A second case in the same test: `LastEvent` empty gives `incomplete` alone, as now.

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/watch/ ./internal/run/ -run 'TestTransitionStallNamesTheLastEvent|TestRecordStallDetail' -v` / Expected: FAIL to build: `Transition.LastEvent` is not defined.

- [ ] **Step 3: Add `LastEvent`, fill it in `Watcher.Transition`, and change `reasonRecord`**, with the interface comments from the Interfaces block. In `Watcher.Transition`, when `follow` returns a window that stalled and ctx has not ended, take under `w.mu` the objects the stall names (the workloads that take part and are not complete, or are deleted with pods left; the ReplicaSets the Tracker maps to such a Deployment; their pods not gone that are not ready or are stopping), then, outside the lock and bounded by the settle time as the last reading is, list the namespace's events once and keep the latest whose involved object's kind and name are among them. Add `LastEvent` to the comment of `Transition`. In `reasonRecord`, the `TransitionStalled` case appends `"the last event: " + t.tr.LastEvent` after `t.tr.Incomplete` when it is not empty.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test -race ./internal/watch/ ./internal/run/ -v` / Expected: PASS.

- [ ] **Step 5: Commit**
  `git add internal/watch/tracker.go internal/watch/events.go internal/watch/events_test.go internal/watch/watcher.go internal/run/record.go internal/run/record_test.go` and `git commit -m "Follow a stall's details with the last event about the workloads and pods it names"`
