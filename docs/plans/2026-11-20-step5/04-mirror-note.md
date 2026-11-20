# Tasks 7 and 8: the mirror in the texts

Read section 10 ("What a run writes", the console's stall text), section 9 ("The order for one host": what a failed pull's events name), section 14, point 31 ("A mirror that lacks an image, in the report"), and case G of `docs/proofs/p02b-mirror-forms.md` before these tasks. Decision 12 of the README fixes the sentence and when it is added.

### Task 7: `ImageHost`, `MirrorNote`, and the note in `watch`

**Files:**
- Create: `internal/config/mirrors.go`, `internal/config/mirrors_test.go`
- Modify: `internal/watch/watcher.go` (`Options`, the `TrackerOptions` it builds), `internal/watch/tracker.go` (`TrackerOptions`), `internal/watch/counts.go` (`podSentence` and its callers), `internal/watch/counts_test.go`

**Interfaces:**
- Consumes: `Config.Registries` as Task 1 leaves it.
- Produces: `func ImageHost(image string) string`
  Returns the registry host of an image's name as the nodes read it: the
  part before the first slash when that part holds a dot or a colon, is
  localhost, or holds an upper-case letter; docker.io for any other name,
  and for index.docker.io. A tag or a digest does not change it.
- Produces: `func (c *Config) MirrorNote(image string) string`
  Returns "the nodes pull HOST only from its mirror MIRROR, and a pull's
  error may name the image alone", HOST and MIRROR as the entry writes
  them, when the entry whose host is ImageHost(image) has a mirror; ""
  otherwise. Hosts are compared as written (section 8).
- Produces: `type Options struct { …; MirrorNote func(image string) string }`
  Names the mirror of an image's host, as Config.MirrorNote does, for the
  texts that list a pod: a container that waits with ErrImagePull or
  ImagePullBackOff gets "; " and the sentence after its state when the
  sentence is not empty. Nil names none. The Watcher hands it to its Tracker.
- Produces: `type TrackerOptions struct { …; MirrorNote func(image string) string }`
  The same, for the Tracker's texts: a stall's detail lines and the text of
  an install that was not ready within limits.timeout.

- [ ] **Step 1: Write the failing tests**

In `mirrors_test.go`:

```go
func TestImageHost(t *testing.T) {
	for image, want := range map[string]string{
		"orders":                          "docker.io",
		"orders:1.0":                      "docker.io",
		"library/orders":                  "docker.io",
		"myregistry/team/orders":          "docker.io",
		"docker.io/library/orders":        "docker.io",
		"index.docker.io/library/orders":  "docker.io",
		"registry.example/team/orders@sha256:" + strings.Repeat("a", 64): "registry.example",
		"registry.example:5000/orders":    "registry.example:5000",
		"localhost/orders":                "localhost",
		"[fd00::5]:5000/orders":           "[fd00::5]:5000",
		"Registry/orders":                 "Registry",
	} {
		if got := ImageHost(image); got != want {
			t.Errorf("ImageHost(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestMirrorNote(t *testing.T) {
	c := &Config{Registries: []Registry{
		{Host: "registry.example", Mirror: "mirror.corp.example/registry-example"},
		{Host: "other.example", CA: "/ca.pem"},
	}}
	// c.MirrorNote("registry.example/team/orders@sha256:…") ==
	//   "the nodes pull registry.example only from its mirror mirror.corp.example/registry-example, and a pull's error may name the image alone"
	// c.MirrorNote("other.example/x") == "", c.MirrorNote("unlisted.example/x") == "",
	// (&Config{}).MirrorNote("registry.example/x") == "".
}

func TestMirrorNoteForEachDockerHubName(t *testing.T) {
	c := &Config{Registries: []Registry{{Host: "docker.io", Mirror: "mirror.corp.example/docker-io"}}}
	const want = "the nodes pull docker.io only from its mirror mirror.corp.example/docker-io, and a pull's error may name the image alone"
	// Each of "orders", "library/orders", "docker.io/library/orders",
	// "index.docker.io/library/orders" and "orders@sha256:…" gives want.
}
```

In `counts_test.go`, beside `TestStallDetails`, with the file's `pod`, `feed`, `options`, `base`, `set` and `exited` helpers:

```go
func TestStallDetailsNameTheMirror(t *testing.T) {
	const img = "registry.example/team/orders@sha256:" + strings.Repeat("a", 64)
	const note = "the nodes pull registry.example only from its mirror mirror.corp.example/registry-example, and a pull's error may name the image alone"
	o := options()
	o.MirrorNote = func(image string) string {
		if image == img {
			return note
		}
		return ""
	}
	waiting := func(reason string) *corev1.Pod {
		p := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
		p.Status.Phase = corev1.PodPending
		p.Spec.Containers = []corev1.Container{{Name: "app", Image: img}}
		p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}}}
		return p
	}
	// The events of TestStallDetails' "a pod in CrashLoopBackOff", with waiting(reason) in place of
	// the crashing pod, ended at +300 s. The pod's line in s.Incomplete is
	//   ImagePullBackOff:  "pod orders-b-1: phase Pending, container app waiting (ImagePullBackOff); " + note
	//   ErrImagePull:      "pod orders-b-1: phase Pending, container app waiting (ErrImagePull); " + note
	//   ContainerCreating: "pod orders-b-1: phase Pending, container app waiting (ContainerCreating)"
	// With options() (no MirrorNote), ImagePullBackOff gives the line with no note.
	// A second container, running, after the waiting one: its ", container side running" follows the note.
}

func TestInstallTextNamesTheMirror(t *testing.T) {
	// In the style of TestInstallState: an install whose one pod waits with ImagePullBackOff for img
	// until limits.timeout; the install-failed condition's text holds the pod's line ending in "; " + note.
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ ./internal/watch/ -run 'TestImageHost|TestMirrorNote|TestStallDetailsNameTheMirror|TestInstallTextNamesTheMirror' -v`
Expected: FAIL: `ImageHost`, `MirrorNote` and `TrackerOptions.MirrorNote` are not defined.

- [ ] **Step 3: Implement `ImageHost` and `MirrorNote` in `internal/config/mirrors.go`, and the note in `watch`, each with its interface comment from the Interfaces block**

`ImageHost` follows `splitDockerDomain` of `github.com/distribution/reference` v0.6.0 (`normalize.go`, which `p02b-mirror-forms.md` read), written out in the package, with no new import. In `watch`, `podSentence` takes the note function as its second argument; for each container of the pod's status whose state waits with `ErrImagePull` or `ImagePullBackOff`, it looks up that container's image in `p.Spec.Containers` by name and, when the function is not nil and gives a sentence, writes "; " and the sentence after the container's state and its last end. `incomplete` passes `t.o.MirrorNote`; `Start` copies `Options.MirrorNote` into the `TrackerOptions` it builds.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ ./internal/watch/ -v` and `go vet ./...`
Expected: PASS, every earlier stall and install text unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/config/mirrors.go internal/config/mirrors_test.go internal/watch/watcher.go internal/watch/tracker.go internal/watch/counts.go internal/watch/counts_test.go
git commit -m "Name the mirror in a stall's and an install's text when a pod waits for an image it could not pull"
```

### Task 8: The note in `probe`, and the report's golden file

**Files:**
- Modify: `internal/probe/pod.go` (`PodOptions`, `podStateText` and its caller), `internal/probe/pod_test.go` (`TestPodStateTextNamesTheMirror`, a case of `TestStartTimesOut`)
- Modify: `internal/report/console_test.go`; Create: `internal/report/testdata/console-stall-mirror.golden.txt`

**Interfaces:**
- Consumes: the sentence of `(*config.Config).MirrorNote` (Task 7), as a function.
- Produces: `type PodOptions struct { …; MirrorNote func(image string) string }`
  Names the mirror of an image's host for the text of a probe that never
  started: when the probe's container waits with ErrImagePull or
  ImagePullBackOff, the text gets "; " and the sentence for the container's
  image, when the sentence is not empty. Nil names none.

- [ ] **Step 1: Write the failing tests**

In `pod_test.go`:

```go
func TestPodStateTextNamesTheMirror(t *testing.T) {
	const img = "registry.example/tools/busybox@sha256:" + strings.Repeat("b", 64)
	const note = "the nodes pull registry.example only from its mirror mirror.corp.example/registry-example, and a pull's error may name the image alone"
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "probe", Image: img}}},
		Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{Name: "probe",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff",
				Message: "Back-off pulling image \"" + img + "\""}}}}}}
	noteOf := func(image string) string {
		if image == img {
			return note
		}
		return ""
	}
	// podStateText(pod, noteOf) ==
	//   "the pod is Pending; container probe is waiting: ImagePullBackOff: Back-off pulling image \"" + img + "\"; " + note
	// podStateText(pod, nil) has no "; the nodes pull"; so for ErrImagePull with the note, and
	// ContainerCreating gives no note.
}
```

`TestStartTimesOut`, the Start-level table of probe-not-started texts, checks that `Start` passes `p.o.MirrorNote` on. Its table gains a field `note func(string) string`, which the test sets as `s.pod.o.MirrorNote` right after `newProbe`, and a case "image not pulled, through a mirror": the status of the case "image not pulled"; a note function that gives `note` for `probeImage`, the image of the pod that `Start` creates, and "" for any other, with `note` the sentence of `TestPodStateTextNamesTheMirror`, made a constant of the test file that both tests share; and the wanted parts `"Pending"` and `"ImagePullBackOff; " + note`. The other cases leave `note` nil, and their texts hold no "; the nodes pull".

In `console_test.go`, a record `stallMirrorRun()`: `stallRun()` whose `transition-stalled` reason's detail lines are

```
deployment/orders: Waiting for deployment "orders" rollout to finish: 1 out of 2 new replicas have been updated...
pod orders-7c9d8b-q8w2n: phase Pending, container orders waiting (ImagePullBackOff); the nodes pull registry.example only from its mirror mirror.corp.example/registry-example, and a pull's error may name the image alone
the last event: Failed to pull image "registry.example/team/orders@sha256:…": rpc error: code = NotFound desc = … not found
```

with no `pod-unschedulable` reason, added to `TestConsoleExamples` as `{"testdata/console-stall-mirror.golden.txt", stallMirrorRun()}`. The golden file is written with `-update` and read: it holds the three lines, wrapped as the console wraps detail.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/probe/ ./internal/report/ -run 'TestPodStateTextNamesTheMirror|TestStartTimesOut|TestConsoleExamples' -v`
Expected: FAIL: `podStateText` takes one argument, `PodOptions` has no `MirrorNote`, and the golden file is missing.

- [ ] **Step 3: Implement the note in `probe/pod.go`, with `PodOptions.MirrorNote`'s interface comment from the Interfaces block, and write the golden file**

`podStateText(pod *corev1.Pod, note func(string) string) string`: after the waiting container's reason and message, the same rule as `watch`'s, with the image from the pod's spec for the container `probe`. Start's text for `probe-not-started` passes `p.o.MirrorNote`. Then `go test ./internal/report/ -run TestConsoleExamples -update` and read the new file.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/probe/ ./internal/report/ -v` and `go vet ./...`
Expected: PASS, every earlier golden file unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/probe/pod.go internal/probe/pod_test.go internal/report/console_test.go internal/report/testdata/console-stall-mirror.golden.txt
git commit -m "Name the mirror in the text of a probe that never started, and pin a stall's text that names one"
```
