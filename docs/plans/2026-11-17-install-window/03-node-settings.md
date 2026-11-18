> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints and its rulings apply to every task here.

### Task 9: The nodes forget an attempt to connect that had no answer within 5 s

Read section 9, the item "Attempts to connect that had no answer", and section 14, point 24. This task was added after Task 8's Step 7 stopped (ruling 16). The cluster's test in Step 1 needs a container engine.

**Files:**
- Modify: `internal/cluster/cluster.go`
- Test: `internal/cluster/cluster_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `const unansweredAttempt = "net.netfilter.nf_conntrack_tcp_timeout_syn_sent=5"`
  The kernel setting, as `sysctl -w` takes it, that Create writes in each
  node: how long, in seconds, connection tracking keeps an attempt to connect
  that has had no answer (section 9). Unexported; only Create uses it.

- [ ] **Step 1: Write the failing test**

In `internal/cluster/cluster_test.go`, `TestCreateAndDelete`, after the three checks of `Exec` and before the check of the caller's kubeconfig:

```go
	for _, n := range nodes {
		out, err := c.Exec(ctx, n, "sysctl", "-n", "net.netfilter.nf_conntrack_tcp_timeout_syn_sent")
		if err != nil || strings.TrimSpace(string(out)) != "5" {
			t.Errorf("node %s keeps an attempt with no answer for %q s, %v; want 5", n, strings.TrimSpace(string(out)), err)
		}
	}
```

- [ ] **Step 2: Run it to verify it fails**

Run, on a machine whose engine is nerdctl: `env CGO_ENABLED=0 KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags cluster -p 1 -count=1 -run TestCreateAndDelete ./internal/cluster/`
Expected: FAIL, with one line "keeps an attempt with no answer for "120" s" for each of the four nodes.

- [ ] **Step 3: Write the setting in each node**

In `internal/cluster/cluster.go`, add `unansweredAttempt` with its interface comment from the Interfaces block. In `Create`, in the loop that writes `hosts.toml` into each node, after that write: run `sysctl -w` with `unansweredAttempt` in the node, through `n.CommandContext(ctx, ...)` with its standard output and standard error in one buffer, as `Exec` does. When it fails, return `fail("write the node settings", fmt.Errorf("node %s: sysctl -w %s: %w: %s", n, unansweredAttempt, err, bytes.TrimSpace(out.Bytes())))`, so that the run is `cluster-failed` and the text has what the node said. Add one sentence to `Create`'s interface comment: each node keeps an attempt to connect that had no answer for 5 s (section 9).

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go vet -tags cluster ./internal/cluster/ && go test ./... -count=1`
Expected: no output from the build and the vets; PASS for every package.

Run, on a machine whose engine is nerdctl: `env CGO_ENABLED=0 KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags cluster -p 1 -count=1 -run TestCreateAndDelete ./internal/cluster/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cluster/cluster.go internal/cluster/cluster_test.go
git commit -m "Have each node forget an attempt to connect that had no answer within 5 s"
```

- [ ] **Step 6: Run Task 8's Steps 6 and 7 again**

Build the binary as Task 8's Step 5 does, then run Task 8's Steps 6 and 7 as written, its stop included. Their results complete Task 8: the README sentence of Task 8's Step 8 is true when Step 7 passes.

In the report of this task, give the lines that `TestFirstFixture` logged about the install window under each Helm major, and every failure of a class other than `refused` with its time from the probe's first observation and its window.
