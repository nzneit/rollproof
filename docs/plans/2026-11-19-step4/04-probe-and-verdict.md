# Step 4 plan, part 4: the custom probe's pod, its lines, and missing figures

### Task 9: The custom probe's pod

Design: section 6, "The contract", the paragraphs **Form** and **Where probe pods run** (the last two paragraphs: the security settings and the token); section 8, "Types" (a custom probe's `env`) and "Secrets"; section 14, point 29, "The custom probe's pod". This plan: decision 9 and the Global Constraint "Every probe pod".

**Files:**
- Modify: `internal/probe/pod.go` (three fields of `PodOptions`; `podSpec` sets `Command`, `Env` and the token)
- Create: `internal/probe/custom.go` (`CustomEnv`)
- Create: `internal/probe/testdata/pod-custom.golden.yaml` (written by `-update`)
- Test: `internal/probe/pod_test.go`; new `internal/probe/custom_test.go`

**Interfaces:**
- Consumes: `config.EnvVar{Name, Value string; ValueFrom map[string]any}` (`internal/config/config.go`, as built)
- Produces: `PodOptions.Command []string`
  The probe container's command, as a pod's container means it: it replaces
  the image's entrypoint. Nil keeps the entrypoint, as the built-in probe does.
- Produces: `PodOptions.Env []corev1.EnvVar`
  The probe container's environment, as a pod's container means it, entries
  with `valueFrom` included. Nil gives the container no variable of the tool's.
- Produces: `PodOptions.ServiceAccountToken bool`
  Whether the probe's pod mounts the token of its namespace's default service
  account: true for a custom probe, false for the built-in probe.
- Produces: `func CustomEnv(env []config.EnvVar) ([]corev1.EnvVar, error)`
  Turns a custom probe's env entries into a container's, in order: `value`
  as it is, `valueFrom` as the container's source. It refuses a `valueFrom`
  whose keys a container's source does not hold, whose values do not have its
  types, or that has not exactly one source. The error names the entry by its
  index and name and never quotes a value. Nil or empty gives nil.

The pod is the built-in probe's pod in every field but four: `command`, `env`, the drain (the caller's `Drain`) and the token. With `ServiceAccountToken` true, `AutomountServiceAccountToken` is `true`, written out, so that a service account that turns the mount off does not take the token away; `ServiceAccountName` stays empty, which is the namespace's `default`. With it false, the pod is byte for byte today's: `pod-http.golden.yaml` does not change.

`CustomEnv`'s check is a round trip: the entry's `valueFrom` map is encoded as JSON and decoded into a `corev1.EnvVarSource`; a decoding error is "does not have the form a container takes". The source is encoded again, and every key path of the input (such as `secretKeyRef.name`) that the second encoding does not hold is a key the container does not take; the first such path, in sorted order, is named. This catches a misspelt key and a key in another case, which `encoding/json` would match without a word and the typed client would then send under the right name or drop. Then the number of non-nil fields among `ConfigMapKeyRef`, `FieldRef`, `FileKeyRef`, `ResourceFieldRef` and `SecretKeyRef` must be one. The texts, with `%d` the index and `%s` the entry's name:

```
probe.custom.env[%d] (%s): valueFrom does not have the form a container takes
probe.custom.env[%d] (%s): valueFrom holds a key that a container does not take: %s
probe.custom.env[%d] (%s): valueFrom needs exactly one of configMapKeyRef, fieldRef, fileKeyRef, resourceFieldRef and secretKeyRef
```

- [ ] **Step 1: Write the failing tests**

  In `custom_test.go`:

  ```go
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
  ```

  In `pod_test.go`, the golden file and the fields that must be the built-in probe's:

  ```go
  const customImage = "registry.example/tools/busybox@sha256:6d9ac9237a84afe1516540f40a0fafdc86859b2141954b4d643af7066d598b74"

  // customOptions is a custom probe's pod as section 6's example writes it.
  func customOptions() PodOptions {
  	return PodOptions{
  		Image:   customImage,
  		Command: []string{"sh", "-c", `trap 'exit 0' TERM; while true; do if nc -z -w1 "$TARGET" 5432; then echo '{"ok":true}'; else echo '{"ok":false,"class":"refused"}'; fi; sleep 0.1; done`},
  		Env: []corev1.EnvVar{
  			{Name: "TARGET", Value: "orders-db.orders"},
  			{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
  				LocalObjectReference: corev1.LocalObjectReference{Name: "probe-token"}, Key: "token"}}},
  		},
  		ServiceAccountToken: true,
  		Drain:               10 * time.Second,
  	}
  }

  func TestPodSpecCustom(t *testing.T) {
  	got, err := yaml.Marshal(podSpec(customOptions()))
  	if err != nil {
  		t.Fatal(err)
  	}
  	const path = "testdata/pod-custom.golden.yaml"
  	if *update {
  		if err := os.WriteFile(path, got, 0o644); err != nil {
  			t.Fatal(err)
  		}
  	}
  	want, err := os.ReadFile(path)
  	if err != nil {
  		t.Fatal(err)
  	}
  	if !bytes.Equal(got, want) {
  		t.Errorf("%s differs\n--- got\n%s--- want\n%s", path, got, want)
  	}
  }

  // A custom probe's pod differs from the built-in probe's only in its image, command, args, env,
  // drain and token (section 6).
  func TestCustomPodKeepsTheBuiltInSettings(t *testing.T) {
  	custom := podSpec(customOptions())
  	builtIn := podSpec(PodOptions{Image: probeImage, Args: []string{"probe", "http"}, Drain: 7 * time.Second})
  	if d := cmp.Diff(builtIn.ObjectMeta, custom.ObjectMeta); d != "" {
  		t.Errorf("metadata differs (-built-in +custom):\n%s", d)
  	}
  	bc, cc := builtIn.Spec.Containers[0], custom.Spec.Containers[0]
  	if cc.Name != "probe" || cc.ImagePullPolicy != corev1.PullIfNotPresent {
  		t.Errorf("container %q, pull policy %q; want probe, IfNotPresent", cc.Name, cc.ImagePullPolicy)
  	}
  	if d := cmp.Diff(bc.SecurityContext, cc.SecurityContext); d != "" {
  		t.Errorf("security context differs (-built-in +custom):\n%s", d)
  	}
  	if d := cmp.Diff(builtIn.Spec.NodeSelector, custom.Spec.NodeSelector); d != "" {
  		t.Errorf("node selector differs:\n%s", d)
  	}
  	if d := cmp.Diff(builtIn.Spec.Tolerations, custom.Spec.Tolerations); d != "" {
  		t.Errorf("tolerations differ:\n%s", d)
  	}
  	if custom.Spec.RestartPolicy != corev1.RestartPolicyNever {
  		t.Errorf("restart policy %q, want Never", custom.Spec.RestartPolicy)
  	}
  	if g := *custom.Spec.TerminationGracePeriodSeconds; g != 10 {
  		t.Errorf("grace %d s, want the custom probe's drain, 10 s", g)
  	}
  	if !slices.Equal(cc.Command, customOptions().Command) || cc.Args != nil {
  		t.Errorf("command %q, args %q; want the configured command and no args", cc.Command, cc.Args)
  	}
  	if d := cmp.Diff(customOptions().Env, cc.Env); d != "" {
  		t.Errorf("env differs:\n%s", d)
  	}
  	if a := custom.Spec.AutomountServiceAccountToken; a == nil || !*a || custom.Spec.ServiceAccountName != "" {
  		t.Errorf("token %v, service account %q; want the default service account's token mounted", a, custom.Spec.ServiceAccountName)
  	}
  	if a := builtIn.Spec.AutomountServiceAccountToken; a == nil || *a || bc.Command != nil || bc.Env != nil {
  		t.Errorf("the built-in probe's pod has token %v, command %q, env %v; want none of them", a, bc.Command, bc.Env)
  	}
  }
  ```

  Add `github.com/google/go-cmp/cmp` to `pod_test.go`'s imports; it is already a test dependency of the module.

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/probe/ -run 'TestCustomEnv|TestPodSpecCustom|TestCustomPodKeepsTheBuiltInSettings' -v`
  Expected: FAIL: the build fails, because `PodOptions` has no field `Command`, `Env` or `ServiceAccountToken` and `CustomEnv` is not defined.

- [ ] **Step 3: Implement the three fields of `PodOptions` and `podSpec`'s use of them in `internal/probe/pod.go`, and `func CustomEnv(env []config.EnvVar) ([]corev1.EnvVar, error)` in `internal/probe/custom.go`**, each with its interface comment from the Interfaces block. Extend `PodOptions`'s comment to say that the command, env and token are a custom probe's. Then write the golden file with `go test ./internal/probe/ -run TestPodSpecCustom -update` and read it: it holds `automountServiceAccountToken: true`, the container `probe` with `command` (three entries) and no `args`, `env` with `TARGET`'s value and `TOKEN`'s `valueFrom.secretKeyRef` (`key: token`, `name: probe-token`), the image `customImage`, the same `securityContext`, `nodeSelector`, `tolerations` and `restartPolicy: Never` as `pod-http.golden.yaml`, and `terminationGracePeriodSeconds: 10`.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/probe/ -run 'TestCustomEnv|TestPodSpecCustom|TestCustomPodKeepsTheBuiltInSettings|TestPodSpec' -v`, then `go vet ./...` and `go test ./...`
  Expected: PASS; `pod-http.golden.yaml` is unchanged (`git diff --stat internal/probe/testdata/pod-http.golden.yaml` prints nothing).

- [ ] **Step 5: Commit**
  `git add internal/probe/pod.go internal/probe/custom.go internal/probe/custom_test.go internal/probe/pod_test.go internal/probe/testdata/pod-custom.golden.yaml` and `git commit -m "Write a custom probe's pod with the built-in probe's security settings, its command and env, and the default service account's token"`

### Task 10: A refused pod, a probe that cannot start, and the probe pod's evidence

Design: section 6, "Lifecycle", the bullet **Start**; section 10, "What a run writes", the console bullet "On a stall" (its last sentence) and the evidence folder's bullet "The status of the probe's pod, and the logs of every container in it"; section 14, point 29. This plan: decision 16 and Review Focus 2.

**Files:**
- Modify: `internal/probe/pod.go` (`replace` maps a refusal; `podStateText` gives a waiting container's message; `PodOptions.Evidence`; `halt` writes the pod and its other containers' logs before it deletes the pod)
- Test: `internal/probe/pod_test.go`; `internal/probe/pod_cluster_test.go` (build tag `cluster`)

**Interfaces:**
- Consumes: `PodOptions.Command`, `PodOptions.Env`, `PodOptions.ServiceAccountToken` (Task 9)
- Produces: `PodOptions.Evidence func(name string) (io.WriteCloser, error)`
  Where the probe writes its pod's evidence: `probe-pod.json`, the pod as the
  API last gave it, and `probe-pod/<container>.log` for each container other
  than `probe`. Written once, just before the probe's pod is deleted, after a
  start without a first observation or at the stop. A part that cannot be read
  holds the reason in its place; a file that cannot be opened is skipped. Nil
  writes nothing.
- Produces: `func (p *Pod) Start(ctx context.Context) (Started, error)`
  As before, and: when the API server refuses the probe's pod as invalid,
  forbidden or a bad request, the error is a Condition `probe-not-started`
  at once, whose text is "the API server refused the probe's pod: " and the
  API server's text. When no observation came in time, the text names a
  waiting container's reason and message.

`replace` keeps returning the API's error for every other failure of the create, which `run` reports as `internal-error`, as today. The refusal is `apierrors.IsInvalid(err) || apierrors.IsForbidden(err) || apierrors.IsBadRequest(err)`: a Pod Security level, an admission webhook's denial at its default code, and the API's validation. The path after it is today's: no run starts, the Facts kept say that the probe stopped when asked, and Events is closed.

`podStateText`'s waiting part becomes `"; container probe is waiting: " + Reason`, followed by `": " + Message` when the message is not empty.

The evidence is written by `halt`, after the watcher has ended and before `stopAsked` and the delete, so it serves both a Start that ended without a first observation (`judgeStart`'s error) and Stop. It reads the pod once (`Get`) and writes it as indented JSON to `probe-pod.json`; then, for each container of `Spec.InitContainers` and `Spec.Containers` whose name is not `probe`, it reads that container's log once, not followed, and writes it to `probe-pod/<name>.log`. Each read waits at most `stopMargin`. When the pod cannot be read, `probe-pod.json` holds `rollproof: the pod could not be read: <error>` and no log is read; when a log cannot be read, its file holds `rollproof: the log could not be read: <error>`. A file that `Evidence` cannot open is skipped, as a failed write to `Raw` is not reported today: the probe has no warnings, and the verdict does not read these files. A Start whose context ended, and a refused pod, write nothing.

- [ ] **Step 1: Write the failing tests** in `pod_test.go`

  ```go
  // evidenceFiles is an Evidence that keeps each file in memory.
  type evidenceFiles struct {
  	mu    sync.Mutex
  	files map[string]*bytes.Buffer
  }

  type nopCloser struct{ *bytes.Buffer }

  func (nopCloser) Close() error { return nil }

  func (e *evidenceFiles) open(name string) (io.WriteCloser, error) {
  	e.mu.Lock()
  	defer e.mu.Unlock()
  	if e.files == nil {
  		e.files = map[string]*bytes.Buffer{}
  	}
  	b := &bytes.Buffer{}
  	e.files[name] = b
  	return nopCloser{b}, nil
  }

  func (e *evidenceFiles) get(name string) (string, bool) {
  	e.mu.Lock()
  	defer e.mu.Unlock()
  	b, ok := e.files[name]
  	if !ok {
  		return "", false
  	}
  	return b.String(), true
  }

  // refusedBy makes the API server refuse every create of a pod with err.
  func refusedBy(s probeSetup, err error) {
  	s.client.PrependReactor("create", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
  		return true, nil, err
  	})
  }

  func TestStartRefusedPod(t *testing.T) {
  	gr := schema.GroupResource{Resource: "pods"}
  	for _, c := range []struct {
  		name string
  		err  error
  		want string
  	}{
  		{"Pod Security", apierrors.NewForbidden(gr, "probe",
  			errors.New(`violates PodSecurity "restricted:latest": runAsNonRoot != true`)),
  			`violates PodSecurity "restricted:latest": runAsNonRoot != true`},
  		{"validation", apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "probe", field.ErrorList{
  			field.Invalid(field.NewPath("spec", "containers").Index(0).Child("env").Index(0).Child("valueFrom"), "",
  				"may not have more than one field specified at a time")}),
  			"may not have more than one field specified at a time"},
  		{"a bad request", apierrors.NewBadRequest("the pod is not one the server reads"), "the pod is not one the server reads"},
  	} {
  		t.Run(c.name, func(t *testing.T) {
  			s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 10*time.Second)
  			refusedBy(s, c.err)
  			began := time.Now()
  			_, err := s.pod.Start(context.Background())
  			got := condition(err)
  			if got.Code != verdict.ProbeNotStarted || !strings.HasPrefix(got.Text, "the API server refused the probe's pod: ") ||
  				!strings.Contains(got.Text, c.want) {
  				t.Errorf("Start = %v, want probe-not-started with the API server's text", err)
  			}
  			if time.Since(began) > 2*time.Second {
  				t.Error("Start waited for StartTimeout")
  			}
  			if _, open := <-s.pod.Events(); open {
  				t.Error("Events is still open")
  			}
  			if facts, _ := s.pod.Stop(context.Background()); !facts.StoppedWhenAsked {
  				t.Error("a probe that never ran did not stop when asked")
  			}
  		})
  	}
  }

  // A create that fails for another reason than a refusal stays the API's error, which run reports
  // as a failure of the tool.
  func TestStartCreateFailsOtherwise(t *testing.T) {
  	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 10*time.Second)
  	refusedBy(s, apierrors.NewServiceUnavailable("etcd is not answering"))
  	_, err := s.pod.Start(context.Background())
  	if err == nil || condition(err).Code != "" || !strings.Contains(err.Error(), "etcd is not answering") {
  		t.Errorf("Start = %v, want the API's error and no Condition", err)
  	}
  }

  // Review Focus 2: a custom probe's env reads a Secret that does not exist.
  func TestStartNamesAMissingSecret(t *testing.T) {
  	status := corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
  		Name: "probe", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
  			Reason: "CreateContainerConfigError", Message: `secret "probe-token" not found`}},
  	}}}
  	s := newProbe(t, &fakeLog{}, status, 100*time.Millisecond, 300*time.Millisecond)
  	_, err := s.pod.Start(context.Background())
  	got := condition(err)
  	if got.Code != verdict.ProbeNotStarted ||
  		got.Text != `no observation within limits.timeout; the pod is Pending; container probe is waiting: CreateContainerConfigError: secret "probe-token" not found` {
  		t.Errorf("Start = %v, want probe-not-started that names the Secret's error", err)
  	}
  }

  // withSidecars makes every created probe pod carry a mesh's init container and proxy, as an
  // injector would add them.
  func withSidecars(s probeSetup) {
  	s.client.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
  		p := a.(k8stesting.CreateAction).GetObject().(*corev1.Pod)
  		p.Spec.InitContainers = append(p.Spec.InitContainers, corev1.Container{Name: "istio-init"})
  		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: "istio-proxy"})
  		return false, nil, nil
  	})
  }

  func TestStopWritesThePodAndItsOtherContainers(t *testing.T) {
  	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
  	withSidecars(s)
  	var ev evidenceFiles
  	s.pod.o.Evidence = ev.open
  	s.endsWhenDeleted(t, logLine(logAt(400), `{"ok":true}`))
  	s.clock.Set(logAt(700))
  	if _, err := s.pod.Start(context.Background()); err != nil {
  		t.Fatal(err)
  	}
  	if _, ok := ev.get("probe-pod.json"); ok {
  		t.Error("probe-pod.json was written before the stop")
  	}
  	if _, err := s.pod.Stop(context.Background()); err != nil {
  		t.Fatal(err)
  	}
  	// The fake API removes the pod at its delete, so a pod in the file was read before the delete.
  	pod, ok := ev.get("probe-pod.json")
  	var read corev1.Pod
  	if !ok || json.Unmarshal([]byte(pod), &read) != nil || read.Name != "probe" || read.Status.Phase != corev1.PodRunning {
  		t.Errorf("probe-pod.json = %q, want the pod as the API gave it before its delete", pod)
  	}
  	for _, name := range []string{"probe-pod/istio-init.log", "probe-pod/istio-proxy.log"} {
  		if got, ok := ev.get(name); got != "fake logs" {
  			t.Errorf("%s = %q, %v; want the container's log", name, got, ok)
  		}
  	}
  	if _, ok := ev.get("probe-pod/probe.log"); ok {
  		t.Error("the probe's own container was written to probe-pod/; it belongs in probe.log")
  	}
  }
  ```

  ```go
  func TestStartTimeoutWritesThePod(t *testing.T) {
  	status := corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
  		Name: "probe", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
  	}}}
  	s := newProbe(t, &fakeLog{}, status, 100*time.Millisecond, 300*time.Millisecond)
  	var ev evidenceFiles
  	s.pod.o.Evidence = ev.open
  	if _, err := s.pod.Start(context.Background()); condition(err).Code != verdict.ProbeNotStarted {
  		t.Fatalf("Start = %v, want probe-not-started", err)
  	}
  	if got, _ := ev.get("probe-pod.json"); !strings.Contains(got, "ImagePullBackOff") {
  		t.Errorf("probe-pod.json = %q, want the pod's status with the waiting reason", got)
  	}
  }

  func TestPodEvidenceThatCannotBeRead(t *testing.T) {
  	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
  	withSidecars(s)
  	var ev evidenceFiles
  	s.pod.o.Evidence = ev.open
  	s.client.PrependReactor("get", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
  		if a.GetSubresource() == "log" {
  			return true, nil, errors.New("the pod's log is gone")
  		}
  		return false, nil, nil
  	})
  	s.endsWhenDeleted(t, logLine(logAt(400), `{"ok":true}`))
  	s.clock.Set(logAt(700))
  	if _, err := s.pod.Start(context.Background()); err != nil {
  		t.Fatal(err)
  	}
  	if _, err := s.pod.Stop(context.Background()); err != nil {
  		t.Errorf("Stop = %v; evidence that cannot be read is no error of the stop", err)
  	}
  	if got, _ := ev.get("probe-pod/istio-proxy.log"); !strings.HasPrefix(got, "rollproof: the log could not be read: ") ||
  		!strings.Contains(got, "the pod's log is gone") {
  		t.Errorf("probe-pod/istio-proxy.log = %q, want the reason", got)
  	}
  }

  func TestRefusedPodWritesNoEvidence(t *testing.T) {
  	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
  	refusedBy(s, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "probe", errors.New("denied")))
  	var ev evidenceFiles
  	s.pod.o.Evidence = ev.open
  	s.pod.Start(context.Background())
  	s.pod.Stop(context.Background())
  	if len(ev.files) != 0 {
  		t.Errorf("evidence %v, want none for a pod that never existed", ev.files)
  	}
  }
  ```

  The fake API's `GetLogs` answers `fake logs` for any container (client-go's fake clientset). Imports to add to `pod_test.go`: `encoding/json`, `k8s.io/apimachinery/pkg/runtime/schema`, `k8s.io/apimachinery/pkg/util/validation/field`.

  In `pod_cluster_test.go`, `TestCustomPodOnACluster`, on one cluster made as `TestPodOnACluster` makes it (name `rollproof-test-custom-probe`, one worker, the test binary stored as the probe's image):
  1. Label the namespace `rollproof-probe` with `pod-security.kubernetes.io/enforce: restricted`. Start a probe with `Image: stored.Probe.Ref()`, `Command: []string{"/rollproof"}`, `Args: HTTPArgs(o)` (the same `o` as `TestPodOnACluster`), `ServiceAccountToken: true`, `Drain: 10 * time.Second`, `StartTimeout: 60 * time.Second`, and an `evidenceFiles`. Assert: Start returns no error, so the pod meets the `restricted` level; the pod read from the API has a volume mount at `/var/run/secrets/kubernetes.io/serviceaccount` in its container `probe`; after Stop, `StoppedWhenAsked` is true and `probe-pod.json` holds `"automountServiceAccountToken": true`.
  2. Start a probe like it whose `Env` is `{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: …{Name: "probe-token"}, Key: "token"}, ConfigMapKeyRef: …{Name: "probe"}, Key: "token"}}}`, built directly and not through `CustomEnv`. Assert: within 10 s, Start gives `probe-not-started` whose text begins `the API server refused the probe's pod: ` and holds `may not have more than one field specified at a time`.
  3. Start a probe like the first whose `Env` reads `secretKeyRef` `probe-token`/`token`, which does not exist, with `StartTimeout: 30 * time.Second`. Assert: Start gives `probe-not-started` whose text holds `CreateContainerConfigError` and `secret "probe-token" not found`; `t.Log` the whole text.

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/probe/ -run 'TestStartRefusedPod|TestStartCreateFailsOtherwise|TestStartNamesAMissingSecret|TestStopWritesThePodAndItsOtherContainers|TestStartTimeoutWritesThePod|TestPodEvidenceThatCannotBeRead|TestRefusedPodWritesNoEvidence' -v`
  Expected: FAIL: the build fails, because `PodOptions` has no field `Evidence`. With the field added and nothing else, `TestStartRefusedPod` gives no Condition and `TestStartNamesAMissingSecret`'s text lacks the message.

- [ ] **Step 3: Implement `PodOptions.Evidence`, the refusal in `replace`, the message in `podStateText` and the evidence in `halt`, in `internal/probe/pod.go`**, with the interface comments from the Interfaces block; rewrite `Start`'s comment to its Interfaces text and `halt`'s to say that it writes the evidence before it asks the probe to stop.

- [ ] **Step 4: Run the tests to see them pass**
  Run: the command of Step 2, then `go vet ./...`, `go vet -tags cluster ./...` and `go test -race ./internal/probe/` and `go test ./...`
  Expected: PASS.
  Then the cluster test: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 -timeout 20m ./internal/probe/ -run 'TestCustomPodOnACluster|TestPodOnACluster' -v`
  Expected: PASS. Report the logged text of case 3: it settles the belief, which only this test checks, that the kubelet names a missing Secret in the waiting container's message.

- [ ] **Step 5: Commit**
  `git add internal/probe/pod.go internal/probe/pod_test.go internal/probe/pod_cluster_test.go` and `git commit -m "End the run at once as probe-not-started when the API server refuses the probe's pod, name a waiting container's message, and keep the probe pod's status and its other containers' logs in the evidence"`

### Task 11: The floor of `t`, and a failed check's bad line

Design: section 6, "The contract": the paragraph after "How a line is read" ("A fault of a line is placed …"), and **Time**, the paragraph "An observation is assigned to the window that holds its time …"; section 12, "Probe lines"; section 14, point 29, "A `t` before the probe existed". This plan: decisions 10 and 11.

**Files:**
- Modify: `internal/probe/line.go` (`ParseLine` takes `asked`; `Line.FailedCheck`)
- Modify: `internal/probe/follow.go` (`Event.FailedCheck`; the follower passes the asked moment to `ParseLine`)
- Modify: `internal/probe/pod.go` (`replace` records the asked moment; `Start` hands it to the follower)
- Test: `internal/probe/line_test.go`, `internal/probe/follow_test.go`, `internal/probe/pod_test.go`, `internal/probe/http_test.go` (its call of `ParseLine`)

**Interfaces:**
- Consumes: `probe.ClockBound` (2 s, as built)
- Produces: `func ParseLine(text string, written, asked time.Time) Line`
  As before, and a line whose `t` is earlier than asked by more than
  ClockBound is Invalid; a `t` exactly ClockBound before asked is not. asked
  is the moment the tool asked for the probe's pod, on the tool's clock; zero
  skips this check.
- Produces: `Line.FailedCheck bool`
  The line is one JSON object, on a line of its own, with a top-level `ok`
  that is `false`, or with `ok` twice and one of them `false`. It is set
  whatever the line's Kind, so an Invalid line says whether it was a failed
  check's.
- Produces: `Event.FailedCheck bool`
  The event's Fault comes from a line whose FailedCheck is set. False for an
  observation and for every other fault.

The README's "Interfaces between tasks" names this last one `probe.Fault.FailedCheck`. The package has no type `Fault`: `Event.Fault` is a `*verdict.Condition`. The flag goes on `Event`, beside the fault it qualifies, and Task 16 reads `e.FailedCheck` on an event whose `Fault` is set.

`ParseLine`'s new check comes right after "t is later than the line", with the text built from `ClockBound`: `t is more than 2s earlier than the moment the tool asked for the probe's pod`. `FailedCheck` is found before any other rule of the line returns: once the line has decoded as one object (and the token after it is end of input), walk its top-level names as `hasDuplicateName` does and set it when any value of the name `ok` is the JSON literal `false`. A line that is not one JSON object, log text, and an object whose `ok` is the string `"false"` do not set it. The final piece of a stopped probe is not read as a line (as built), so it never sets it.

The asked moment: `replace` reads `p.o.Now()` just before its call of `Create` and returns it with its error (`replace(ctx, timeout) (time.Time, error)`). `Start` sets it on the follower as a field `podAsked time.Time`, after `newFollower` and before the follower's goroutine starts; `take` passes `f.podAsked` to `ParseLine` and sets `FailedCheck` on the `ProbeLineInvalid` event it emits. The name `asked` is taken in the follower by `stopAsked`'s flag, hence `podAsked`. The floor applies to the built-in probe too (decision 10): every probe goes through `take`.

- [ ] **Step 1: Write the failing tests**

  In `line_test.go`, change the call to `ParseLine(tc.raw, written, time.Time{})`, so that the table's cases keep their meaning, and add:

  ```go
  func TestParseLineFloorOfT(t *testing.T) {
  	asked := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
  	written := asked.Add(30 * time.Second)
  	line := func(tt time.Time) string { return `{"ok":false,"t":"` + tt.Format(time.RFC3339Nano) + `"}` }
  	for _, c := range []struct {
  		name  string
  		t     time.Time
  		asked time.Time
  		kind  Kind
  	}{
  		{"after the ask", asked.Add(time.Second), asked, Observed},
  		{"less than the bound before the ask", asked.Add(-time.Second), asked, Observed},
  		{"the bound before the ask", asked.Add(-ClockBound), asked, Observed},
  		{"more than the bound before the ask", asked.Add(-ClockBound - time.Nanosecond), asked, Invalid},
  		{"an hour before the ask", asked.Add(-time.Hour), asked, Invalid},
  		{"no ask known", asked.Add(-time.Hour), time.Time{}, Observed},
  	} {
  		t.Run(c.name, func(t *testing.T) {
  			got := ParseLine(line(c.t), written, c.asked)
  			if got.Kind != c.kind {
  				t.Fatalf("Kind = %v, want %v (fault %q)", got.Kind, c.kind, got.Fault)
  			}
  			if c.kind == Invalid && got.Fault != "t is more than 2s earlier than the moment the tool asked for the probe's pod" {
  				t.Errorf("Fault = %q", got.Fault)
  			}
  			if !got.FailedCheck {
  				t.Error("FailedCheck = false for a line whose ok is false")
  			}
  		})
  	}
  }

  func TestParseLineFailedCheck(t *testing.T) {
  	written := time.Date(2026, 11, 13, 10, 0, 7, 500_000_000, time.UTC)
  	for _, c := range []struct {
  		raw    string
  		kind   Kind
  		failed bool
  	}{
  		{`{"ok":false}`, Observed, true},
  		{`{"ok":true}`, Observed, false},
  		{`{"ok":false,"n":2}`, Invalid, true},
  		{`{"ok":false,"t":"2026-11-14T10:00:00Z"}`, Invalid, true},
  		{`{"ok":false,"ms":-1}`, Invalid, true},
  		{`{"ok":false,"v":2}`, Invalid, true},
  		{`{"ok":false,"ok":true}`, Invalid, true},
  		{`{"ok":true,"ok":false}`, Invalid, true},
  		{`{"ok":true,"ok":true}`, Invalid, false},
  		{`{"ok":true,"n":0}`, Invalid, false},
  		{`{"ok":"false"}`, Invalid, false},
  		{`{"ok":null,"ok":false}`, Invalid, true},
  		{`{"ok":false} trailing`, Invalid, false},
  		{`{"ok":fals`, Invalid, false},
  		{`2026-11-13 10:00:07 {"ok":false}`, Invalid, false},
  		{`{"detail":{"ok":false}}`, JSONText, false},
  		{`starting`, Text, false},
  	} {
  		t.Run(c.raw, func(t *testing.T) {
  			got := ParseLine(c.raw, written, time.Time{})
  			if got.Kind != c.kind || got.FailedCheck != c.failed {
  				t.Errorf("Kind %v, FailedCheck %v; want %v, %v", got.Kind, got.FailedCheck, c.kind, c.failed)
  			}
  		})
  	}
  }
  ```

  In `http_test.go`, change the call to `ParseLine(text, s.at, time.Time{})`.

  In `follow_test.go`:

  ```go
  func TestFollowFailedCheckAndFloor(t *testing.T) {
  	early := logAt(-2500).Format(time.RFC3339Nano)
  	l := newFakeLog(logLine(logAt(100), `{"ok":true}`) +
  		logLine(logAt(200), `{"ok":false,"t":"`+early+`"}`) +
  		logLine(logAt(300), `{"ok":true,"n":0}`) +
  		logAt(400).Format(time.RFC3339Nano) + ` {"ok":false,"n":2}`) // a final piece with no line end
  	quickPolling(t)
  	var events []Event
  	f := newFollower(l.open, l.running, io.Discard, func() time.Time { return logAt(60_000) }, func(e Event) { events = append(events, e) })
  	f.podAsked = logAt(0)
  	f.stopAsked()
  	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  	defer cancel()
  	f.run(ctx)
  	if len(events) != 3 {
  		t.Fatalf("events = %+v, want an observation and two faults; the final piece is not judged", events)
  	}
  	if e := events[1]; e.Fault == nil || e.Fault.Code != verdict.ProbeLineInvalid || !e.Fault.At.Equal(logAt(200)) ||
  		!strings.Contains(e.Fault.Text, "earlier than the moment the tool asked") || !e.FailedCheck {
  		t.Errorf("event 1 = %+v, want a failed check's fault for the early t, at the line's stamp", e)
  	}
  	if e := events[2]; e.Fault == nil || e.FailedCheck {
  		t.Errorf("event 2 = %+v, want a fault that is not a failed check's", e)
  	}
  	if events[0].FailedCheck {
  		t.Error("an observation carries FailedCheck")
  	}
  }
  ```

  In `pod_test.go`, the moment of the ask is the one just before the create, neither the start of `Start` nor the moment after the create:

  ```go
  func TestStartFloorsTAtTheCreate(t *testing.T) {
  	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
  	s.clock.Set(logAt(-60_000)) // Start begins
  	s.client.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
  		s.clock.Set(logAt(0)) // the moment of the ask
  		return false, nil, nil
  	})
  	s.client.PrependReactor("create", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
  		s.clock.Set(logAt(500)) // after the create
  		return false, nil, nil
  	})
  	at := func(ms int) string { return logAt(ms).Format(time.RFC3339Nano) }
  	s.endsWhenDeleted(t, logLine(logAt(0), `{"ok":true}`)+
  		logLine(logAt(100), `{"ok":false,"t":"`+at(-1800)+`"}`)+ // 1.8 s before the ask: counted
  		logLine(logAt(200), `{"ok":false,"t":"`+at(-2100)+`"}`)) // 2.1 s before the ask: a fault
  	if _, err := s.pod.Start(context.Background()); err != nil {
  		t.Fatal(err)
  	}
  	if _, err := s.pod.Stop(context.Background()); err != nil {
  		t.Fatal(err)
  	}
  	events := allEvents(s.pod.Events())
  	if len(events) != 3 || events[1].Obs == nil || events[2].Fault == nil ||
  		events[2].Fault.Code != verdict.ProbeLineInvalid || !events[2].FailedCheck {
  		t.Errorf("events = %+v, want two observations and the early t's fault", events)
  	}
  }
  ```

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/probe/ -run 'TestParseLine|TestFollowFailedCheckAndFloor|TestStartFloorsTAtTheCreate' -v`
  Expected: FAIL: the build fails, because `ParseLine` takes two arguments and `Line`, `Event` and `follower` have no field `FailedCheck` or `podAsked`.

- [ ] **Step 3: Implement `func ParseLine(text string, written, asked time.Time) Line`, `Line.FailedCheck` and `Event.FailedCheck` in `internal/probe/line.go` and `internal/probe/follow.go`, and the asked moment in `internal/probe/pod.go`**, with the interface comments from the Interfaces block; rewrite `Line`'s comment to say what `FailedCheck` is, and `replace`'s to say that it returns the moment it asked for the pod.

- [ ] **Step 4: Run the tests to see them pass**
  Run: the command of Step 2, then `go vet ./...`, `go test -race ./internal/probe/` and `go test ./...`
  Expected: PASS.

- [ ] **Step 5: Commit**
  `git add internal/probe/line.go internal/probe/follow.go internal/probe/pod.go internal/probe/line_test.go internal/probe/follow_test.go internal/probe/pod_test.go internal/probe/http_test.go` and `git commit -m "Make a t more than the clock check's bound before the probe's pod was asked for a fault of the line, and mark a fault whose line is a failed check's"`

### Task 12: No `ms` or no `lag` gives null and "-"

Design: section 10, "What a run writes": the console bullet on the first table ("Slowest" and "behind") and on the install's lines, and the `result.json` bullets on the windows and the install; section 12, "Golden files for the report formats" (the one for a custom probe that gives neither `ms` nor `lag`).

**Files:**
- Modify: `internal/verdict/vocab.go` (`Observation.HasMs`, `HasLag`), `internal/verdict/decide.go` (`WindowResult.HasSlowest`, `HasLag`), `internal/verdict/observe.go` (`countObservations`)
- Modify: `internal/probe/line.go` (`ParseLine` sets `HasMs` and `HasLag`)
- Modify: `internal/report/record.go` and `internal/report/result.go` (`SlowestMs` and `LagMs` of `Window` and `Install` become `*float64`), `internal/report/console.go` (`slowestCell`, `behindCell`)
- Modify: `internal/run/record.go` (`windowRecord`, `installRecord`)
- Create: `internal/report/testdata/console-custom-no-ms-or-lag.golden.txt` (written by `-update`)
- Test: `internal/verdict/observe_test.go`, `internal/probe/line_test.go`, `internal/report/result_test.go`, `internal/report/console_test.go`, `internal/run/record_test.go`

**Interfaces:**
- Produces: `Observation.HasMs bool`
  The line gave `ms`. Ms is then its value; without it Ms is 0 and means
  nothing.
- Produces: `Observation.HasLag bool`
  The line gave `lag`. Lag is then its value; without it Lag is 0 and means
  nothing.
- Produces: `WindowResult.HasSlowest bool`
  A passed line counted in the window gave `ms`, so SlowestMs is a slowest
  check that passed. False when every check failed or no passed line gave it.
- Produces: `WindowResult.HasLag bool`
  A line counted in the window gave `lag`, passed or failed, so LagMs is the
  furthest lag reported. False when no line gave it.
- Produces: `report.Window.SlowestMs *float64`
  The slowest check that passed, in milliseconds; nil when the probe gave
  none, which result.json writes as null and the console as "-".
- Produces: `report.Window.LagMs *float64`
  The furthest lag the probe reported, in milliseconds; nil when it gave none,
  which result.json writes as null and the console as "-".
- Produces: `report.Install.SlowestMs *float64`
  As Window's, for the install window; nil also in a record with no counts.
- Produces: `report.Install.LagMs *float64`
  As Window's, for the install window; nil also in a record with no counts.

The README names only the `verdict` fields; the report's fields must become pointers for `result.json` to hold null, and `run/record.go`, which copies one into the other, changes with them, as do its tests. `countObservations` keeps the values as now (an absent field is 0 and changes no maximum) and adds `r.HasLag = r.HasLag || o.HasLag` for every counted line and `r.HasSlowest = r.HasSlowest || o.HasMs` in its branch for a passed line. `windowRecord` and `installRecord` set the pointer to a copy of the value when the flag is true and leave it nil otherwise. The console's two cells read the pointer alone: `slowestCell(ms *float64) string` and `behindCell(ms *float64) string` give "-" for nil and `millis(*ms)` otherwise; their rules on the counts go, because a record whose checks all failed has no slowest, by `run`'s rule.

The golden file's record is a custom probe's (decision 9: kind `custom`, named by its image), but the console prints no line for a custom probe until Task 15 adds it, so this golden file has no probe line now. Task 15 rewrites it with `-update` when it adds the custom probe's lines; this task fixes its "-" cells.

- [ ] **Step 1: Write the failing tests**

  In `observe_test.go`:

  ```go
  func TestHasSlowestAndHasLag(t *testing.T) {
  	noFields := func(from, to time.Duration) []Observation {
  		var obs []Observation
  		for d := from; d < to; d += sec {
  			obs = append(obs, Observation{Written: at(d), T: at(d), OK: true, N: 200})
  		}
  		return obs
  	}
  	withFields := func(o Observation) Observation { o.HasMs, o.HasLag = true, true; return o }
  	lagOnly := Observation{Written: at(20 * sec), T: at(20 * sec), OK: false, N: 1, Class: "refused", Lag: 7, HasLag: true}
  	msOnFailure := Observation{Written: at(5 * sec), T: at(5 * sec), OK: false, N: 1, Class: "refused", Ms: 900, HasMs: true}
  	passWithMs := withFields(Observation{Written: at(-10 * sec), T: at(-10 * sec), OK: true, N: 1, Ms: 41, Lag: 3})
  	d := Decide(timeline(70*sec, withInstall(allThree()), noFields(-15*sec, 70*sec),
  		[]Observation{lagOnly, msOnFailure, passWithMs}))
  	for _, c := range []struct {
  		window          int
  		slowest, hasLag bool
  		slowestMs       float64
  		lagMs           float64
  	}{
  		{0, true, true, 41, 3},   // install: a passed line gave both
  		{1, false, false, 0, 0},  // steady: only a failed line gave ms
  		{2, false, true, 0, 7},   // deploy: a failed line gave lag
  		{3, false, false, 0, 0},  // rollback: no line gave either
  	} {
  		w := d.Windows[c.window]
  		if w.HasSlowest != c.slowest || w.HasLag != c.hasLag || w.SlowestMs != c.slowestMs || w.LagMs != c.lagMs {
  			t.Errorf("%s: HasSlowest %v, HasLag %v, SlowestMs %v, LagMs %v; want %v, %v, %v, %v",
  				w.Name, w.HasSlowest, w.HasLag, w.SlowestMs, w.LagMs, c.slowest, c.hasLag, c.slowestMs, c.lagMs)
  		}
  	}
  }
  ```

  In `line_test.go`, `TestParseLineHasMsAndLag`: `{"ok":true}` gives `HasMs` and `HasLag` false; `{"ok":true,"ms":0,"lag":0}` gives both true with `Ms` and `Lag` 0; `{"ok":false,"ms":12.5}` gives `HasMs` true and `HasLag` false; `{"ok":true,"n":200,"lag":6}` gives `HasMs` false and `HasLag` true. In `TestParseLine`'s comparison of an observation, compare `HasMs` and `HasLag` too, and set both to true in the `obs` of the three cases whose line gives `ms` and `lag`: the second, the sixth, and `{"ok":true,"ms":0,"lag":0}`, which then gets an `obs` of its own in place of `pass`.

  In `result_test.go`: in `passRun` and every other record of the package's tests, `SlowestMs: 41` becomes `SlowestMs: new(41.0)`, and so for each value (`LagMs: new(6.0)`, `new(13.5)` and so on). Add `"slowest_ms"` and `"lag_ms"` to `TestNoNulls`'s `keysThatMayBeNull`. In `TestInstallRecordAtEachEnding`, the case "before the probe's first observation" now wants `"slowest_ms":null,"lag_ms":null`. Add:

  ```go
  // customNoMsOrLagRun is a pass judged by a custom probe that gives neither ms nor lag (section 12).
  func customNoMsOrLagRun() Run {
  	r := passRun()
  	for i := range r.Windows {
  		r.Windows[i].SlowestMs, r.Windows[i].LagMs = nil, nil
  	}
  	r.Install.SlowestMs, r.Install.LagMs = nil, nil
  	for i := range r.Transitions {
  		r.Transitions[i].Workloads[0].Probed = "not known"
  	}
  	r.Probes = []Probe{{
  		Kind:     "custom",
  		Target:   "registry.example/tools/busybox@sha256:6d9ac9237a84afe1516540f40a0fafdc86859b2141954b4d643af7066d598b74",
  		EnvNames: []string{"TARGET"}, CommandCount: 3, Lines: 1530, StoppedWhenAsked: new(true),
  	}}
  	return r
  }

  func TestResultNullWithoutMsOrLag(t *testing.T) {
  	b, err := encode(customNoMsOrLagRun())
  	if err != nil {
  		t.Fatal(err)
  	}
  	var file struct {
  		Windows []map[string]any `json:"windows"`
  		Install map[string]any   `json:"install"`
  	}
  	if err := json.Unmarshal(b, &file); err != nil {
  		t.Fatal(err)
  	}
  	for _, m := range append(file.Windows, file.Install) {
  		for _, k := range []string{"slowest_ms", "lag_ms"} {
  			if v, ok := m[k]; !ok || v != nil {
  				t.Errorf("%s is %v (present %v), want null", k, v, ok)
  			}
  		}
  	}
  }
  ```

  In `console_test.go`: the cases "no check in the install window" and "every check failed in the install window" set `r.Install.SlowestMs = nil` (both) and `r.Install.LagMs = nil` (the first), and keep their expected text. Add to `TestConsoleExamples`'s second list `{"testdata/console-custom-no-ms-or-lag.golden.txt", customNoMsOrLagRun()}`, and to `TestConsoleLines` the case `{"no ms or lag", passRun, func(r *Run) { r.Windows[1].SlowestMs, r.Windows[1].LagMs = nil, nil }, "deploy     33.4 s    6,680        0              0         -        -   pass\n"}`.

  In `internal/run/record_test.go`: each `verdict.WindowResult` that sets `SlowestMs` and `LagMs` also sets `HasSlowest: true, HasLag: true`, and each `report.Window` and `report.Install` it expects takes `new(…)` for them. Add:

  ```go
  func TestRecordWithoutMsOrLag(t *testing.T) {
  	w := windowRecord(verdict.WindowResult{Name: verdict.Deploy, Checks: 100, SlowestMs: 0, LagMs: 0, HasLag: true})
  	if w.SlowestMs != nil || w.LagMs == nil || *w.LagMs != 0 {
  		t.Errorf("SlowestMs %v, LagMs %v; want nil and 0", w.SlowestMs, w.LagMs)
  	}
  }
  ```

  and, in the test of `installRecord` beside `record_test.go:355`, a case whose install window has `HasSlowest` and `HasLag` false and expects both nil.

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/verdict/ ./internal/probe/ ./internal/report/ ./internal/run/ -run 'TestHasSlowestAndHasLag|TestParseLine|TestResultNullWithoutMsOrLag|TestNoNulls|TestInstallRecordAtEachEnding|TestConsoleExamples|TestConsoleLines|TestRecord' -v`
  Expected: FAIL: the build fails, because `Observation` has no field `HasMs` and the report's fields are not pointers.

- [ ] **Step 3: Implement the four `verdict` fields, `ParseLine`'s two flags, the report's four pointer fields with `slowestCell(ms *float64) string` and `behindCell(ms *float64) string`, and the copying in `run/record.go`**, each field with its interface comment from the Interfaces block. Rewrite the comments of `WindowResult`, `report.Window` and `report.Install` to say that a missing figure is nil or false. Then write the golden file with `go test ./internal/report/ -run 'TestConsoleExamples' -update` and read it: the install's lines end `slowest -, behind -`, and every row of the first table has "-" under "slowest" and "behind"; `git diff internal/report/testdata/` shows no other golden file changed.

- [ ] **Step 4: Run the tests to see them pass**
  Run: the command of Step 2, then `go vet ./...`, `go vet -tags cluster ./...`, `go vet -tags e2e ./...` and `go test ./...`
  Expected: PASS; `result-pass.golden.json` and every console golden file but the new one are unchanged.

- [ ] **Step 5: Commit**
  `git add internal/verdict/vocab.go internal/verdict/decide.go internal/verdict/observe.go internal/verdict/observe_test.go internal/probe/line.go internal/probe/line_test.go internal/report/record.go internal/report/result.go internal/report/console.go internal/report/result_test.go internal/report/console_test.go internal/report/testdata/console-custom-no-ms-or-lag.golden.txt internal/run/record.go internal/run/record_test.go` and `git commit -m "Give null in result.json and a dash on the console for a window or install whose probe gave no ms or no lag"`
