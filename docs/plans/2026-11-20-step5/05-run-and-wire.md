# Tasks 9 and 10: `registries` and `as` from end to end

Read section 8 ("Secrets"), section 10 ("What a run writes", "On a signal"), section 12's masking test, and decisions 1, 3, 4 and 12 of the README before these tasks.

### Task 9: Masking in `run`; `registries` wired and off `NotBuilt`

**Files:**
- Modify: `internal/run/run.go` (the `writer` interface, `runState`, `setUp`, every print), `internal/run/run_test.go`, `internal/run/fakes_test.go`
- Modify: `cmd/rollproof/wire.go` (`create`, `session.Watcher`, `podOptions`), `cmd/rollproof/main_test.go`
- Modify: `internal/config/notbuilt.go`, `internal/config/notbuilt_test.go`

**Interfaces:**
- Consumes: `mask.New`, `mask.Credential`, `(*mask.Masker).String` (Task 2); `report.Mask`, `(*report.Writer).SetMasker` (Task 3); `cluster.Spec.Registries` (Task 5); `(*config.Config).MirrorNote` (Task 7); `watch.Options.MirrorNote` (Task 7); `probe.PodOptions.MirrorNote` (Task 8).
- Produces: `type writer interface { Final(report.Run) error; Evidence(name string) (io.WriteCloser, error); ReplaceEvidence(name string, b []byte) error; Dir() string; SetMasker(m *mask.Masker) }`
  What a run asks of the results folder: the final result.json, evidence
  files, a file replaced whole, the folder's path, and the masker that the
  folder applies to all of it from then on. report.Writer is the real one.
- Produces: `func clusterSpec(cfg *config.Config, n string, keep bool, evidence func(string) (io.WriteCloser, error)) cluster.Spec`
  The Spec that Create gets for cfg and the cluster's full name n: the
  fields that create writes today, and the configuration's registries.
- Produces: `func (c *Config) NotBuilt() []string`
  Lists images.NAME.as alone now; registries is built.

- [ ] **Step 1: Write the failing tests**

In `run_test.go`, with the world of `fakes_test.go`. The fake cluster gains a field `rawText string`, which its `Probe` writes into `raw`; its `deleteErr` is set to an error whose text holds `pw1`.

```go
func TestPasswordsAreMaskedEverywhere(t *testing.T) {
	const pw1, pw2 = "s3cret-pass", "pa\"ss<&"
	w := newWorld(t)
	w.cfg.Registries = []config.Registry{{Host: "registry.example", Username: "ci", Password: pw1},
		{Host: "docker.io", Username: "ci2", Password: pw2}}
	// Planted in every input: after "create", through w.evidence (the function every unit
	// writes its evidence through), each name of step 4's writers:
	//   kind/rollproof-test-control-plane/containerd.log  pw1
	//   registry.log                                      base64 of "ci:" + pw1, padded
	//   commands/setup                                    "sh -c " + strconv.Quote("echo "+pw2), then pw1 in the output
	//   pods/orders-1/orders.log                          pw1, in two writes split inside it
	//   pods/orders-1/orders.previous.log                 pw2
	//   probe-pod.json                                    json.Marshal of {"message": pw2}
	//   probe-pod/istio-proxy.log                         base64 of "ci2:" + pw2, without padding
	// the probe's raw line (w.cluster.rawText) pw1; the watcher's events (w.watcher.events) pw2;
	// the cluster's Delete fails with a text that holds pw1, which becomes a warning.
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	// For each form, of both passwords: plain; base64 of USER:PASSWORD, padded and not; the JSON
	// spellings with and without HTML escaping; the Go spelling: no file under w.out, and
	// neither w.stdout nor w.stderr, holds it. result.json parses, its verdict is "pass" and its
	// warnings hold "***"; each planted evidence file holds "***".
}

func TestTheMaskerComesRightAfterLoad(t *testing.T) {
	w := newWorld(t)
	w.cfg.Registries = []config.Registry{{Host: "registry.example", Username: "ci", Password: "s3cret-pass"}}
	w.checkErr = verdict.Condition{Code: verdict.RunnerUnsupported, Text: "the engine said s3cret-pass"}
	// Exit 2; result.json's reason text is "the engine said ***"; stdout holds no "s3cret".
}
```

`TestNotBuilt` in `run_test.go` gives the world an image with `As: "registry.example/team/orders"` in place of a registry, and wants the text "not built yet: images.orders.as"; `TestRunNotBuilt` in `main_test.go` does the same with `images: { orders: { local: orders:candidate, as: registry.example/team/orders } }` in place of its `registries` line. In `notbuilt_test.go`, the cases "registries" and "several at once" no longer list `registries`.

In `main_test.go`:

```go
func TestClusterSpec(t *testing.T) {
	// For a configuration with two registries, clusterSpec(cfg, "rollproof-x", true, ev) gives
	// Name "rollproof-x", RunDir cluster.RunDir("rollproof-x"), NodeImage, RegistryImage, Workers,
	// Namespace and Timeout from cfg, Keep true, Evidence ev, and Registries equal to cfg.Registries.
}
```

`TestPodOptions` also checks, for both probes and a configuration whose `registries` has `{Host: "registry.example", Mirror: "mirror.corp.example/r"}`, that `o.MirrorNote("registry.example/x@sha256:…")` equals `cfg.MirrorNote` of the same image.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/run/ ./cmd/rollproof/ ./internal/config/ -run 'TestPasswordsAreMaskedEverywhere|TestTheMaskerComesRightAfterLoad|TestNotBuilt|TestRunNotBuilt|TestClusterSpec|TestPodOptions' -v`
Expected: FAIL: the world stops at "not built yet: registries", and `clusterSpec` is not defined.

- [ ] **Step 3: Implement the changes, each named one with its interface comment from the Interfaces block**

In `run.go`: `runState` gains `mask *mask.Masker`; right after `Load` returns a configuration, `s.mask = mask.New(credentialsOf(cfg))` and `s.w.SetMasker(s.mask)`, before `NotBuilt` and `Check`. `credentialsOf(cfg *config.Config) []mask.Credential` takes each entry's username and password. The console is `report.Console(report.Mask(rec, s.mask))`; every other line written to `Stdout` or `Stderr` goes through `s.mask.String` (a nil masker before `Load` leaves it as it is). `panicWriter` in `run_test.go` gains a `SetMasker` that does nothing.

In `wire.go`: `create` calls `cluster.Create(ctx, clusterSpec(cfg, n, keep, evidence), w.engine)`; `session.Watcher` sets `MirrorNote: cfg.MirrorNote`; `podOptions` sets `MirrorNote: cfg.MirrorNote` for both probes.

In `notbuilt.go`: remove the `registries` clause.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./...` and `go vet ./...` and `go vet -tags cluster ./...` and `go vet -tags e2e ./...`
Expected: PASS.

- [ ] **Step 5: Run the first fixture once**

Run: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags e2e -p 1 ./e2e/ -run 'TestFirstFixture/helm-3' -v -timeout 30m`
Expected: PASS. A run with no `registries` writes what it wrote before.

- [ ] **Step 6: Commit**

```bash
git add internal/run/run.go internal/run/run_test.go internal/run/fakes_test.go cmd/rollproof/wire.go cmd/rollproof/main_test.go internal/config/notbuilt.go internal/config/notbuilt_test.go
git commit -m "Mask registry passwords in everything a run writes, and build registries from end to end"
```

### Task 10: `as` wired; `NotBuilt` removed

**Files:**
- Modify: `cmd/rollproof/wire.go` (`clusterSpec`), `cmd/rollproof/main_test.go` (`TestClusterSpec`; remove `TestRunNotBuilt`)
- Delete: `internal/config/notbuilt.go`, `internal/config/notbuilt_test.go`
- Modify: `internal/run/run.go` (remove the check), `internal/run/run_test.go` (remove `TestNotBuilt`; add `TestAnImageWithAsIsRun`), `e2e/config_test.go` (remove the `NotBuilt` check of `TestConfigOfEachShapeLoads`), `README.md` (the line "Not built yet")

**Interfaces:**
- Consumes: `(*config.Config).AsHosts` (Task 1); `cluster.Spec.AsHosts` (Task 5); `images.Store` with `as` (Task 6).
- Produces: `func clusterSpec(cfg *config.Config, n string, keep bool, evidence func(string) (io.WriteCloser, error)) cluster.Spec`
  As in Task 9, and AsHosts from cfg.AsHosts(), so that every node's
  settings for those hosts list the run's registry first.

- [ ] **Step 1: Write the failing tests**

```go
func TestAnImageWithAsIsRun(t *testing.T) {
	w := newWorld(t)
	w.cfg.Images = map[string]config.Image{"orders": {Local: "orders:candidate", As: "registry.example/team/orders"}}
	// Exit 0, and the call log holds "create": nothing stops a configuration with as.
}
```

`TestClusterSpec` also wants `AsHosts` equal to `cfg.AsHosts()` for a configuration with an image whose `as` is on `registry.example`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/run/ ./cmd/rollproof/ -run 'TestAnImageWithAsIsRun|TestClusterSpec' -v`
Expected: FAIL: the run stops at "not built yet: images.orders.as", and `AsHosts` is empty.

- [ ] **Step 3: Set `AsHosts` in `clusterSpec`, with its interface comment from the Interfaces block, and remove `NotBuilt`**

Remove `notbuilt.go` and its test, the check in `setUp` (and the import it leaves unused), `TestNotBuilt`, `TestRunNotBuilt` and the check in `e2e/config_test.go`. In `README.md`, the line "Not built yet: …" becomes: "Every key of version 1 is built: `registries` and `as` in `images` came with step 5."

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./...` and `go vet ./...` and `go vet -tags cluster ./...` and `go vet -tags e2e ./...`
Expected: PASS, and `grep -rn NotBuilt internal cmd e2e` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add cmd/rollproof/wire.go cmd/rollproof/main_test.go internal/config/notbuilt.go internal/config/notbuilt_test.go internal/run/run.go internal/run/run_test.go e2e/config_test.go README.md
git commit -m "Build as in images from end to end, and remove the list of what the build cannot run"
```
