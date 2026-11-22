# Tasks 2 and 3: kind's Podman name, and every call into kind before the verdict

Read section 9 ("Name and lifetime", "Engines other than Docker"), section 10 ("On a signal"), section 12 (the bullets "A signal during each call into kind" and "Names"), section 14, point 32 (the bullets "A signal while kind works" and "kind's node image under Podman"), and decisions 7 and 8 of the README, before these tasks.

### Task 2: kind's node image under Podman, and the comment on `pickEngine`

**Files:**
- Create: `internal/cluster/podmanname.go`, `internal/cluster/podmanname_test.go`
- Modify: `internal/cluster/engine.go` (`nodeImageName`; `pullIfMissing`'s comment), `internal/cluster/cluster.go` (`Create`'s pull of the node image, `nodeImage`), `internal/cluster/runner.go` (the comment on `pickEngine`), `internal/cluster/engine_test.go`, `internal/cluster/create_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func podmanName(image string) string`
  The name by which kind's Podman provider asks the engine for the node image
  image: docker.io/ in front of a name with no registry host, docker.io/library/
  in front of a name of one segment, and no tag when the name has a digest.
- Produces: `func (e Engine) nodeImageName(image string) string`
  The name by which the tool asks the engine for the node image image: under
  Podman, podmanName's, so that kind finds the image the tool pulled; for every
  other engine, image as it is, the name kind's providers for those engines ask
  for (section 9).

- [ ] **Step 1: Write the failing tests**

In `podmanname_test.go` (`//go:build linux`, package `cluster`):

```go
func TestPodmanNameOnKindsCases(t *testing.T) {
	// The 13 cases of kind v0.33.0's Test_sanitizeImage
	// (pkg/cluster/internal/providers/podman/images_test.go), its image and its pullImageName.
	const d = "@sha256:69860bda5563ac81e3c0057d654b5253219618a22ec3a346306239bba8cfa1a6"
	for _, c := range []struct{ image, want string }{
		{"kindest/node:v1.21.1" + d, "docker.io/kindest/node" + d},
		{"kindest/node:v1.21.1", "docker.io/kindest/node:v1.21.1"},
		{"kindest/node" + d, "docker.io/kindest/node" + d},
		{"foo.bar/kindest/node:v1.21.1" + d, "foo.bar/kindest/node" + d},
		{"foo.bar/kindest/node:v1.21.1", "foo.bar/kindest/node:v1.21.1"},
		{"foo.bar/kindest/node" + d, "foo.bar/kindest/node" + d},
		{"foo.bar/baz:quux" + d, "foo.bar/baz" + d},
		{"foo.bar/baz:quux", "foo.bar/baz:quux"},
		{"foo.bar/baz" + d, "foo.bar/baz" + d},
		{"localhost:5000/kindest/node:v1.21.1" + d, "localhost:5000/kindest/node" + d},
		{"baz:quux" + d, "docker.io/library/baz" + d},
		{"baz:quux", "docker.io/library/baz:quux"},
		{"baz" + d, "docker.io/library/baz" + d},
	} {
		// podmanName(c.image) == c.want
	}
}

func TestPodmanNameOfTheSuitesNodeImages(t *testing.T) {
	// podmanName(DefaultNodeImage()) ==
	//   "docker.io/kindest/node@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"
	// podmanName("kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d") ==
	//   "docker.io/kindest/node@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d"
}
```

In `engine_test.go`:

```go
func TestNodeImageNameUnderEachEngine(t *testing.T) {
	// For "kindest/node:v1.37.0": Engine{Binary: "podman"} gives "docker.io/kindest/node:v1.37.0";
	// "docker", "nerdctl", "finch" and "nerdctl.lima" each give "kindest/node:v1.37.0".
}

func TestNodeImageAsksTheEngineByItsName(t *testing.T) {
	// With fakeEngineOnPath (below) as podman: nodeImage(ctx, Engine{Binary: "podman"},
	// "kindest/node:v1.37.0") returns "kindest/node:v1.37.0@sha256:abc", and the fake's log holds
	// exactly one line, "image inspect --format {{json .RepoDigests}} docker.io/kindest/node:v1.37.0".
	// With fakeEngineOnPath as nerdctl: the same call with Binary "nerdctl" returns the same, and the line
	// ends "kindest/node:v1.37.0" with nothing in front of kindest.
	// A name with a digest, under either: returned as it is, and the log is empty.
}
```

`fakeEngineOnPath(t *testing.T, name string) (log string)`, a helper in `engine_test.go` beside its `fakeEngine`, which it does not replace: it writes into `t.TempDir()` an executable file named `name` with this text, LOG replaced by the path of a log file in the same folder, and puts that folder first on the `PATH` with `t.Setenv`:

```sh
#!/bin/sh
printf '%s\n' "$*" >> 'LOG'
case "$1 $2" in
"image inspect") printf '%s\n' '["docker.io/kindest/node@sha256:abc"]' ;;
esac
exit 0
```

So a test never reaches a real engine of that name, also on a runner that has one.

In `create_test.go`:

```go
func TestCreatePullsTheNodeImageByTheEnginesName(t *testing.T) {
	// Rows, each with fakeEngineOnPath for its engine, kindDelete replaced by one that returns nil,
	// pullImage replaced by one that records the image it is given and returns
	// errors.New("stop here"), and Spec{Name: "rollproof-test-unit", RunDir:
	// filepath.Join(t.TempDir(), "rollproof-test-unit"), Workers: 1, Namespace: "app",
	// Timeout: time.Minute, NodeImage: the row's}:
	//   podman, no node-image:    the pull is given podmanName(DefaultNodeImage())
	//   podman, node-image "kindest/node:v1.34.11@sha256:44e2…" (the whole digest as above):
	//                             the pull is given "docker.io/kindest/node@sha256:44e2…"
	//   nerdctl, no node-image:   the pull is given DefaultNodeImage()
	// Each Create returns cluster-failed with the text "pull the node image: stop here".
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cluster/ -run 'TestPodmanName|TestNodeImageNameUnderEachEngine|TestNodeImageAsksTheEngineByItsName|TestCreatePullsTheNodeImageByTheEnginesName' -v`
Expected: FAIL to compile: `podmanName` and `nodeImageName` are not defined.

- [ ] **Step 3: Implement, each named one with its interface comment from the Interfaces block**

`podmanname.go` begins with `//go:build linux`, a blank line, then kind's licence header as its file `images.go` carries it (the block comment "Copyright 2019 The Kubernetes Authors. Licensed under the Apache License, Version 2.0 …", copied whole), then this comment and the package clause:

```go
// podmanName and stripTag are copied from sigs.k8s.io/kind v0.33.0,
// pkg/cluster/internal/providers/podman/images.go (sanitizeImage and stripTag), with one change:
// podmanName keeps only the name that kind's Podman provider asks the engine for, not the name it
// prints. Read them again when the linked kind release changes; TestPodmanNameOnKindsCases holds
// kind's own cases.
```

`podmanName`'s body is `sanitizeImage`'s, returning `pullImageName`; `stripTag` is kind's, unchanged.

In `engine.go`, `nodeImageName` returns `podmanName(image)` when `e.Binary == "podman"`, and `image` otherwise. `pullIfMissing`'s comment ends: "Create hands it a node image by nodeImageName."

In `cluster.go`, `Create`'s pull becomes `pullImage(e, ctx, e.nodeImageName(ref), s.Timeout)`, and the comment above it: "kind is given the reference as written, and the engine is asked for it by nodeImageName, so that kind's own check finds the image there and does not pull it again, outside the limit." In `nodeImage`, the engine's `image inspect` is given `e.nodeImageName(named)`; its comment ends: "The engine is asked by nodeImageName; the name returned is named as written, with the digest after it, so that the report and rollproof version keep it."

In `runner.go`, the comment on `pickEngine` becomes:

```go
// pickEngine returns the engine's command name, or "" when there is none. KIND_EXPERIMENTAL_PROVIDER
// names it when it holds a value that kind's own command line takes (pkg/internal/runtime/runtime.go
// at v0.33.0): docker, podman, nerdctl, finch or nerdctl.lima. Otherwise it is the first of docker,
// nerdctl and podman whose -v exits 0 with a first line that begins with the engine's banner. That
// is the order of kind's cluster.DetectNodeProvider, and not its test: kind's IsAvailable wants
// exactly one line of output, and tries finch when nerdctl answers no such line. The tool names the
// engine to kind itself (Engine.provider), so kind's own search never runs. The action's script,
// action.sh, follows this rule line for line and starts the tool with the variable set to its answer.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cluster/ -v -run 'TestPodmanName|TestNodeImageNameUnderEachEngine|TestNodeImageAsksTheEngineByItsName|TestCreatePullsTheNodeImageByTheEnginesName|TestCreateFailureTexts'`, then `gofmt -l .`, `go test ./...` and the four vets.
Expected: PASS, no output from `gofmt` and the vets. `TestCreateFailureTexts`' row "the node image's pull fails" still wants `DefaultNodeImage()`: its engine is no Podman.

**What stays unshown on this machine, which has no Podman:** that Podman's pull of `docker.io/kindest/node@sha256:…` works on GitHub's image with no alias; that kind's Podman provider then finds the image and pulls nothing; that `podman image inspect` gives `RepoDigests` for that name. Podman stays not promised (section 9). A run of `rollproof-proofs.yml`'s Podman job without its alias would show the first two; it is not in this plan (README, "The runs on GitHub").

- [ ] **Step 5: Commit**

```
git add internal/cluster/podmanname.go internal/cluster/podmanname_test.go internal/cluster/engine.go internal/cluster/cluster.go internal/cluster/runner.go internal/cluster/engine_test.go internal/cluster/create_test.go
git commit -m "Ask Podman for a node image by the name kind's Podman provider uses, and correct the comment on how the tool picks its engine"
```

### Task 3: every call into kind before the verdict stops waiting at a signal

**Files:**
- Create: `internal/cluster/kind.go`, `internal/cluster/kind_test.go`
- Modify: `internal/cluster/cluster.go` (the seams; `Create`'s node settings into `writeNodeSettings`; `createKind`; `remove`; `writeRegistryFiles`; `byRole`; `fail`), `internal/cluster/logs.go` (`collectLogs`), `internal/cluster/create_test.go` (`TestCreateKindRetries`' row "ctx has ended"; the stubs of `TestCreateKindRetries` and `TestCreateFailureTexts`, Step 2), `internal/cluster/logs_test.go` (`writeLogFile`; the stubs of `TestCollectLogsCopiesEveryFile` and `TestCollectLogsWithNoRegistry`, Step 2), `internal/cluster/cluster_test.go` (tag `cluster`: one new test, one line of `TestCreateWritesRegistrySettings`)
- Modify: `cmd/rollproof/main_test.go` (`TestFailedClientError`, `TestStoreFailedTakesTheLogs`), `internal/run/run_test.go` (two new tests)

**Interfaces:**
- Consumes: nothing of this plan.
- Produces: `func untilDone(ctx context.Context, call func() error) error`
  untilDone runs call, a call into kind that takes no context, and returns its
  error; or, when ctx ends first, ctx's error at once. A ctx that has already
  ended returns its error, and call is not made. A call that ctx outran goes on in
  its goroutine until kind returns, and its result is dropped: what it made
  carries the cluster's name, and the next run's removal of leftovers deletes it
  (section 10, "On a signal"). A panic of call is raised again here when call
  returned first, and dropped when ctx won, so that it never ends the process
  from the goroutine.
- Produces: the seams `var kindListNodes = (*kindcluster.Provider).ListNodes`, `var nodeRole = nodes.Node.Role`, `var writeNodeFile = nodeutils.WriteFile`, beside `kindCreate`, `kindDelete` and `kindCollectLogs`, with the comment: "The calls into kind that Create's node settings make. A test replaces them."
- Produces: `func writeNodeSettings(ctx context.Context, p *kindcluster.Provider, name string, regs []config.Registry, asHosts []string, runRegistry string) ([]nodes.Node, error)`
  writeNodeSettings writes the node settings into every node of the cluster name:
  each entry's ca and settings file, rollproof.test's settings file, and the
  kernel's setting for an attempt to connect that had no answer; and returns the
  nodes, the control-plane node first. A ca that cannot be read fails it before
  any node is listed, with the entry's index. Its calls into kind take no
  context, so they run through untilDone: when ctx ends, it returns ctx's error at
  once.

The calls that go through `untilDone`, read in the code at `58c1cbf`, each one before the verdict (decision 7):

| Where | Call | Reached from |
| --- | --- | --- |
| `remove` | `kindDelete` | `Create`'s removal of leftovers; `fail`'s removal of a cluster whose set-up failed; `Cluster.Delete`, which `failedClientError` calls with the run's context |
| `createKind` | `kindCreate`, and `kindDelete` of a failed attempt | `Create` |
| `collectLogs` | `kindCollectLogs` | `fail`; `Cluster.CollectLogs`, which `failedClientError` and `storeFailed` call |
| `writeNodeSettings` (new) | `kindListNodes`, `writeNodeFile` (in `writeRegistryFiles` too), `nodeRole` (in `byRole`) | `Create` |

The delete at a run's end (`runState.cleanUp`) also reaches `remove`, with a context that no signal ends (`context.WithoutCancel`), so `untilDone` waits there as before (section 10). The node's `CommandContext` (the `sysctl`) and `Cluster.Exec` take the context and are cut short by it already.

- [ ] **Step 1: Write the failing tests**

In `kind_test.go` (`//go:build linux`):

```go
func TestUntilDone(t *testing.T) {
	// "the call returns first": a call that returns errors.New("kind said no"): untilDone returns
	//   that error.
	// "a context that has ended": ctx cancelled before; untilDone returns context.Canceled, and
	//   the call was not made.
	// "the context ends during the call": a call that waits until the test closes release; ctx is
	//   cancelled 50 ms after the call began; untilDone returns context.Canceled within 1 s of the
	//   cancel. The test then closes release and sees the call return (a channel the call closes
	//   on its way out), within 1 s.
	// "a panic of a call that returns first": a call that panics with "kind broke": untilDone
	//   panics with "kind broke" (the test recovers it and compares).
	// "a panic after the context won": a call that waits for release and then panics with
	//   "kind broke late"; ctx is cancelled first; untilDone returns context.Canceled; the test
	//   closes release, waits for the call's deferred close of a channel, and then 100 ms more:
	//   the test binary is still running, so the goroutine recovered the panic.
}
```

`kind_test.go` also holds `fakeNode` and `fakeCmd`: a `nodes.Node` whose `String` gives its name, whose `Role` gives its role, whose `IP` and `SerialLogs` return nothing, and whose `Command` and `CommandContext` give a `fakeCmd`, an `exec.Cmd` of `sigs.k8s.io/kind/pkg/exec` whose `Run` returns nil and whose four setters return the command itself.

```go
func TestASignalDuringEachCallIntoKind(t *testing.T) {
	// Each row replaces kind's calls with stubs, each restored in t.Cleanup; the call named
	// "blocks" closes entered and then waits until the test ends (a channel closed in a t.Cleanup
	// registered after the restores, so that it runs first), as a call into kind that does not
	// return, and then returns errors.New("released") (with no nodes or role, where it gives one).
	// The test cancels ctx as soon as entered is closed, and wants the function to return within
	// 2 s of the cancel. Create's rows use Spec{Name: "rollproof-test-unit",
	// RunDir: filepath.Join(t.TempDir(), "rollproof-test-unit"), Workers: 1, Namespace: "app",
	// Timeout: time.Minute}, Engine{Binary: "rollproof-test-no-such-engine"}, and pullImage
	// replaced by one that returns nil.
	//
	//   "the removal of an earlier run's cluster": kindDelete blocks on its first call.
	//     Create returns cluster-failed whose text begins
	//     "remove leftovers: delete the kind cluster rollproof-test-unit: context canceled";
	//     kindCreate was not called.
	//   "kind's creation": kindDelete returns nil; kindCreate blocks.
	//     The text begins "create the kind cluster: context canceled"; kindDelete was called once;
	//     kindCollectLogs was not called.
	//   "the delete of a failed attempt": kindCreate returns missedLine(1); kindDelete returns nil on
	//     its first call and blocks on its second.
	//     The text begins "create the kind cluster: " + missedLine(1).Error() +
	//     "; delete the failed attempt 1: context canceled"; kindCreate was called once.
	//   "the log collection of a failed set-up": kindCreate returns errors.New("exit status 125");
	//     Spec.Evidence writes into a folder of its own (evidenceDir); kindCollectLogs is the call
	//     that blocks: it first makes its folder dir (RunDir/kind-logs) with one file in it,
	//     kind-version.txt holding "v0.33.0" (written with writeLogFile, Step 2), then closes
	//     entered and blocks; containerLogs is replaced by a stub that records its calls.
	//     The text begins "create the kind cluster: exit status 125"; RunDir/kind-logs still holds
	//     kind-version.txt; the evidence folder holds no file; the containerLogs stub was not
	//     called; kindDelete was called once (the leftovers').
	//   "the removal of a cluster whose set-up failed": as the row before, but kindCollectLogs
	//     returns nil, and kindDelete returns nil on its first call and blocks on its second.
	//     The text begins "create the kind cluster: exit status 125" and ends with LeftBehind.
	//   "the node list": writeNodeSettings(ctx, nil, "rollproof-test-unit", nil, nil,
	//     "10.89.0.5:5000") with kindListNodes blocking. It returns (nil, context.Canceled).
	//   "a node's file": kindListNodes returns []nodes.Node{fakeNode{"rollproof-test-unit-control-plane",
	//     "control-plane"}}; writeNodeFile blocks. It returns (nil, context.Canceled).
	//   "a node's role": writeNodeFile returns nil; nodeRole blocks. It returns (nil, context.Canceled).
}

func TestWriteNodeSettingsFailures(t *testing.T) {
	// "a ca that cannot be read": regs = {Host: "registry.example", CA: filepath.Join(t.TempDir(),
	//   "missing.pem")}: the error is "registries[0].ca: open PATH: no such file or directory", with
	//   PATH the ca's path, and kindListNodes was not called.
	// "a node's file that cannot be written": kindListNodes gives fakeNode{"n1", "worker"};
	//   writeNodeFile returns errors.New("disk full"): the error is "node n1: disk full".
	// "the nodes in order": kindListNodes gives fakeNode{"b", "worker"}, fakeNode{"c",
	//   "control-plane"}, fakeNode{"a", "worker"}; writeNodeFile records each (node, path): the
	//   nodes returned are c, a, b; the recorded paths are
	//   "/etc/containerd/certs.d/rollproof.test/hosts.toml" once for each node, and nothing else.
}
```

In `create_test.go`, the row "ctx has ended" of `TestCreateKindRetries` gets a field `wantAny []string` that replaces `want` for it: the error's text is `missedLine(1).Error()` or `context.Canceled.Error()`, because its stub cancels ctx and returns at once, and `untilDone` may see either first. `creates` 1 and `deletes` 0 stay.

In `cluster_test.go` (tag `cluster`):

```go
// A signal during kind's creation: Create returns at once, kind's abandoned call runs to its end,
// and the next run's first step removes what it left (section 10, "On a signal"). The test checks
// that no attempt of kind's Create has ended at the cancel, so that the result it then waits for
// is the one of the call that the signal abandoned, not an earlier attempt's.
func TestAnAbandonedCreateIsRemovedByTheNextRun(t *testing.T) {
	// testSpec("abandoned", 1) on testEngine(t). kindCreate, as TestMain wrapped it, is wrapped
	// once more: the wrapper closes began (once) and sends the wrapped call's error on ended (a
	// channel with room for createAttempts, one for every attempt that createKind makes, so that
	// no send of the wrapper waits). Create runs in a goroutine with a context that the test
	// cancels 10 s after began was closed (began is awaited for at most 5 minutes: the node image
	// may be pulled first). Just before the cancel the test fails unless ended is empty: "kind's
	// Create ended before the signal (ERR), so no call was abandoned".
	// Create returns within 3 s of the cancel, cluster-failed, with "context canceled" in its text.
	// The test then waits at most 5 minutes for ended: kind's call ran on to its end.
	// The engine then lists at least one container whose name begins "rollproof-test-abandoned-"
	// (ENGINE ps -a --format {{.Names}}): kind left nodes.
	// remove(context.Background(), e, s.Name, s.RunDir) returns nil; afterwards the engine lists no
	// container whose name begins "rollproof-test-abandoned-", and s.RunDir does not exist.
	// Cleanup: remove(context.Background(), e, s.Name, s.RunDir) once more, its error ignored.
}
```

`TestCreateWritesRegistrySettings`' map `want` gains the line `"/etc/containerd/certs.d/docker.io/ca.pem": string(caBytes),` (step 5's leftover: its entry for `docker.io` has a `ca`).

In `cmd/rollproof/main_test.go`, `TestFailedClientError`'s table gains a field `cancelDuring string`: the call it names ("collect" or "delete") cancels the test's context and returns `ctx.Err()`, as the real call does now when a signal comes during it. Two rows:

```go
// {"a signal during the collection", keep false, cancelDuring "collect", collectErr nil,
//   deleteErr nil, wantCollect 1, wantCalls 0, wantOrder ["collect"],
//   wantText reached + "; the cluster's logs were not all collected: context canceled",
//   wantJoined ""},
// {"a signal during the delete", keep false, cancelDuring "delete", collectErr nil, deleteErr nil,
//   wantCollect 1, wantCalls 1, wantOrder ["collect", "delete"], wantText left,
//   wantJoined "the clean-up after it failed: context canceled"},
```

`TestStoreFailedTakesTheLogs` gains the row `{"a signal during the collection", failed, cancelDuring: true, …}`: its collect cancels the context and returns `ctx.Err()`; one call; the condition's text is `failed.Text + "; the cluster's logs were not all collected: context canceled"`.

In `internal/run/run_test.go`:

```go
// A signal while the cluster is made: the cluster unit returns as soon as the signal came, and the
// run ends with a final result.json whose one reason is interrupted, before any window.
func TestASignalWhileTheClusterIsMade(t *testing.T) {
	// newWorld; ctx with cancel; w.hooks["create"] = cancel; w.createErr = verdict.Condition{
	// Code: verdict.ClusterFailed, Text: "create the kind cluster: context canceled"}.
	// Exit 2; result.json final; its codes are exactly ["interrupted"], with no window; the call log
	// holds no "store".
}

// The same while the images are stored: storeFailed takes no logs once the signal came, and the
// cluster stays for the next run.
func TestASignalWhileTheImagesAreStored(t *testing.T) {
	// newWorld; ctx with cancel; w.hooks["store"] = cancel; w.cluster.storeErr = verdict.Condition{
	// Code: verdict.ClusterFailed, Text: "the probe's image: context canceled"}.
	// Exit 2; result.json final; codes exactly ["interrupted"]; the call log holds no "deployer"
	// and no "delete".
}
```

- [ ] **Step 2: Keep `t.Fatal` out of the stubs of kind's calls**

From this task on, kind's calls run on `untilDone`'s goroutine, not on the test's, and `testing` does not allow `t.Fatal`, `t.Fatalf` or `t.FailNow` from another goroutine than the test's. So in every test file of `internal/cluster` that replaces `kindCreate`, `kindDelete`, `kindCollectLogs` or one of the three new seams, a stub calls none of them, nor a helper that does: such a call becomes `t.Errorf` and a return. The new stubs of Task 2 and of this task follow the rule. The places in the existing tests, read at `58c1cbf`:

- `internal/cluster/logs_test.go:81-82`, in the `kindCollectLogs` stub of `TestCollectLogsCopiesEveryFile` (lines 77-84), and `:164`, in that of `TestCollectLogsWithNoRegistry` (lines 163-166): each calls `writeTestFile` (lines 30-39), which calls `t.Fatal`. Beside `writeTestFile` comes a variant that returns its error:

  ```go
  // writeLogFile writes text to path, making its folders, and returns the error. A stub of kind's
  // calls runs on another goroutine than the test's, where t.Fatal is not allowed: it reports the
  // error with t.Errorf and returns it.
  func writeLogFile(path, text string) error
  ```

  The two stubs call it, and on an error call `t.Errorf` with the error and return it. `writeTestFile` stays for the test's own goroutine (`TestCopyLogsClosesEachFile`, line 140).
- `internal/cluster/create_test.go:65`, in the `kindCreate` stub of `TestCreateKindRetries`: `t.Fatalf("attempt %d of kind's Create, want at most %d", …)` becomes `t.Errorf` with the same text and `return errors.New("an attempt too many")`.
- `internal/cluster/create_test.go:261` and `:266`, in the `kindCreate` stub of `TestCreateFailureTexts`: `t.Fatalf("attempt %d of kind's Create, want %d", …)`, and `t.Fatal(err)` after the kubeconfig's write; and `:273`, in its `kindDelete` stub: `t.Fatalf("delete %d of kind's Delete, want %d", …)`. Each becomes `t.Errorf` (the first and the last with the same text, the second as `t.Errorf("write the kubeconfig: %v", err)`) and a return: of `errors.New("an attempt too many")`, of the write's error, and of `errors.New("a delete too many")`. The returns for lines 261 and 273 come before the index into `tc.creates` or `tc.deletes`, which would otherwise run past its end.
- Read, and left as they are, since no stub there calls `t.Fatal` or a helper that does: `internal/cluster/remove_test.go:38-41` (the `kindDelete` stub of `TestRemoveRefuses` counts and returns nil); `internal/cluster/cluster_test.go:50-61` (`TestMain`'s wrapper of `kindCreate`, which has no `t`); in `create_test.go`, the `kindDelete` stub of `TestCreateKindRetries` (lines 77-83), the `kindCollectLogs` stub of `TestCreateFailureTexts` (line 282) and the stubs of `TestCreateWritesTheCredentialsFileBeforeKind` (lines 346-370), which report with `t.Errorf`; in `logs_test.go`, the `kindCollectLogs` stubs at lines 182-184 and 204-207, which call no `t` method, or `t.Error` only.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/cluster/ ./cmd/rollproof/ ./internal/run/ -run 'TestUntilDone|TestASignalDuringEachCallIntoKind|TestWriteNodeSettingsFailures|TestCreateKindRetries|TestFailedClientError|TestStoreFailedTakesTheLogs|TestASignalWhileTheClusterIsMade|TestASignalWhileTheImagesAreStored' -v`
Expected: FAIL to compile in `internal/cluster`: `untilDone`, `writeNodeSettings` and the three seams are not defined. The `cmd` and `run` rows pass already, since their fakes return as the real calls will: they pin the paths that the real calls now reach.

- [ ] **Step 4: Implement, each named one with its interface comment from the Interfaces block**

`kind.go` (`//go:build linux`) holds `untilDone`:

```go
func untilDone(ctx context.Context, call func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	type result struct {
		err      error
		panicked any
	}
	done := make(chan result, 1) // room for one, so that the goroutine never waits for a reader
	go func() {
		var r result
		defer func() {
			r.panicked = recover()
			done <- r
		}()
		r.err = call()
	}()
	select {
	case r := <-done:
		if r.panicked != nil {
			panic(r.panicked)
		}
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

In `cluster.go`:

- The seams, beside `kindCreate` and `kindDelete`.
- `createKind`: `err := untilDone(ctx, func() error { return kindCreate(p, name, options...) })`, and the delete of a failed attempt `untilDone(ctx, func() error { return kindDelete(p, name, kubeconfig) })`. The loop and its texts stay.
- `remove`: `untilDone(ctx, func() error { return kindDelete(e.provider(), name, kubeconfig) })`. The text "delete the kind cluster NAME: " stays.
- `Create`: the lines from `c.provider.ListNodes(s.Name)` to `byRole(listed)` become one call, `if c.nodes, err = writeNodeSettings(ctx, c.provider, s.Name, s.Registries, s.AsHosts, c.NodeRegistry); err != nil { return fail("write the node settings", err) }`.
- `writeNodeSettings` reads each `ca` first (the loop of today, with its text `registries[%d].ca: %w`), then runs the rest inside one `untilDone`: `kindListNodes(p, name)`; for each node, `writeRegistryFiles`, `writeNodeFile(n, settingsPath(TestHost), HostsTOML(runRegistry))` and the `sysctl` through `n.CommandContext(ctx, …)`, with the errors and texts of today; then `byRole(listed)`. The goroutine assigns the sorted nodes to a variable of the function; it is read only when `untilDone` returned nil.
- `writeRegistryFiles` calls `writeNodeFile` in place of `nodeutils.WriteFile`, and `byRole` calls `nodeRole(n)` in place of `n.Role()`.
- `fail`: after `collectLogs`, `if ctx.Err() != nil { return nil, failure }`, so that a signal during the collection ends the clean-up there. Its comment says so.

In `logs.go`, `collectLogs`: `collectErr := untilDone(ctx, func() error { return kindCollectLogs(p, name, dir) })`; when `ctx.Err() != nil` after it, return `ctx.Err()` at once: nothing is copied, the folder is not removed (kind may still be writing it), and the registry's log is not taken. Otherwise `collectErr` joins the copy's and the removal's errors as today. Its comment says what a signal leaves: "the half-written folder stays in the run's folder, which the next run removes."

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cluster/ ./cmd/rollproof/ ./internal/run/ -v -run 'TestUntilDone|TestASignalDuringEachCallIntoKind|TestWriteNodeSettingsFailures|TestCreateKindRetries|TestCreateFailureTexts|TestCollectLogs|TestFailedClientError|TestStoreFailedTakesTheLogs|TestASignalWhileTheClusterIsMade|TestASignalWhileTheImagesAreStored'`, then `go test -race ./internal/cluster/ ./cmd/rollproof/ ./internal/run/`, `gofmt -l .`, `go test ./...` and the four vets.
Expected: PASS, no race, no output from `gofmt` and the vets.

- [ ] **Step 6: Run the cluster tests on this machine**

Run: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster/ -run 'TestAnAbandonedCreateIsRemovedByTheNextRun|TestCreateWritesRegistrySettings|TestCreateAndDelete' -v -timeout 30m`
Expected: PASS for the three. Then `nerdctl ps -a --format "{{.Names}}"` prints no name that begins `rollproof-test-`.

**Stop rule.** When `TestAnAbandonedCreateIsRemovedByTheNextRun` fails because kind's abandoned call did not end within 5 minutes, or because a container is left after `remove`, stop and report to the maintainer with the test's output and `nerdctl ps -a`; remove only the containers whose names begin `rollproof-test-abandoned-`. A failure of kind's creation at its log line (`could not find a log line that matches`) inside the abandoned call is not a failure of the test: the call ended, and its nodes are kept and removed all the same. When kind's abandoned call ends with another error and leaves no container whose name begins `rollproof-test-abandoned-`, the test fails at its assertion that kind left nodes: stop and report, with kind's error (under `-v`, `TestMain`'s line "kind Create of rollproof-test-abandoned, attempt N: failed: …" in the test's output); it is not a failure of the change.

- [ ] **Step 7: Commit**

```
git add internal/cluster/kind.go internal/cluster/kind_test.go internal/cluster/cluster.go internal/cluster/logs.go internal/cluster/create_test.go internal/cluster/logs_test.go internal/cluster/cluster_test.go cmd/rollproof/main_test.go internal/run/run_test.go
git commit -m "Stop waiting for kind at a signal in every call into kind before the verdict, and leave what kind made to the next run"
```
