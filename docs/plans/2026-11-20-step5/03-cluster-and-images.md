# Tasks 4 to 6: the node files, the credentials file, and `as`

Read section 9 ("The run's registry", "How the caller's image gets in", "Registries: credentials, mirrors and certificate authorities"), section 12's golden files, section 14, points 1, 2 and 30, and the Decisions of `docs/proofs/p02-mirror-credentials.md` (form D, case e) and `docs/proofs/p02b-mirror-forms.md` (`override_path` at the top, Docker Hub) before these tasks. Decisions 5 to 7 and 9 to 11 of the README fix the forms.

### Task 4: The settings file for each host

**Files:**
- Modify: `internal/cluster/hosts.go`, `internal/cluster/hosts_test.go` (`testdata/hosts-test.golden.toml` stays as it is)
- Create: `internal/cluster/testdata/hosts-as-mirror-ca-credentials.golden.toml`, `hosts-mirror-prefix.golden.toml`, `hosts-mirror-v2.golden.toml`, `hosts-mirror-v2-only.golden.toml`, `hosts-mirror-prefix-v2.golden.toml`, `hosts-mirror-root.golden.toml`, `hosts-docker-io-mirror.golden.toml`, `hosts-docker-io-ca.golden.toml`, `hosts-ca.golden.toml`, `hosts-as-ca.golden.toml`, `hosts-as-port-ca.golden.toml`, `hosts-ipv6-port-ca.golden.toml`
**Interfaces:**
- Consumes: `config.Registry` as it is.
- Produces: `func HostSettings(entry config.Registry, runRegistry string) string`
  Returns the settings file every node needs for entry.Host, by decision 5:
  the mirror as the default place with its path override and the ca, and
  the run's registry, at runRegistry, as the one place tried before it.
  runRegistry is empty when no entry of images names the host in as. An
  empty result means the node needs no file for the host.
- Produces: `func HostsTOML(runRegistry string) string`
  Unchanged: the settings of rollproof.test, now HostSettings of an entry
  that holds only that host.

- [ ] **Step 1: Write the failing tests**

Each case's golden file holds exactly the text below; `ca` is any absolute path, and `run` is `"10.89.0.5:5000"`.

```go
func TestHostSettingsGolden(t *testing.T) {
	const run, ca = "10.89.0.5:5000", "/home/u/certs/corp-ca.pem"
	for _, tc := range []struct {
		file        string
		entry       config.Registry
		runRegistry string
	}{
		{"hosts-as-mirror-ca-credentials.golden.toml", config.Registry{Host: "registry.example", Username: "ci", Password: "pw", CA: ca, Mirror: "mirror.corp.example/registry-example"}, run},
		{"hosts-mirror-prefix.golden.toml", config.Registry{Host: "registry.example", Mirror: "mirror.corp.example/registry-example"}, ""},
		{"hosts-mirror-v2.golden.toml", config.Registry{Host: "registry.example", CA: ca, Mirror: "cache.corp.example/v2/proxy-project"}, ""},
		{"hosts-mirror-v2-only.golden.toml", config.Registry{Host: "registry.example", Mirror: "mirror.example/v2"}, ""},
		{"hosts-mirror-prefix-v2.golden.toml", config.Registry{Host: "registry.example", Mirror: "mirror.corp.example/cache/v2"}, ""},
		{"hosts-mirror-root.golden.toml", config.Registry{Host: "registry.example", Mirror: "mirror.corp.example:8443"}, ""},
		{"hosts-docker-io-mirror.golden.toml", config.Registry{Host: "docker.io", Username: "ci", Password: "pw", CA: ca, Mirror: "mirror.corp.example/docker-io"}, run},
		{"hosts-docker-io-ca.golden.toml", config.Registry{Host: "docker.io", CA: ca}, ""},
		{"hosts-ca.golden.toml", config.Registry{Host: "registry.example", CA: ca}, ""},
		{"hosts-as-ca.golden.toml", config.Registry{Host: "registry.example", CA: ca}, run},
		{"hosts-as-port-ca.golden.toml", config.Registry{Host: "registry.example:5000", CA: ca}, run},
		{"hosts-ipv6-port-ca.golden.toml", config.Registry{Host: "[fd00::5]:5000", CA: ca}, ""},
	} {
		t.Run(tc.file, func(t *testing.T) { golden(t, tc.file, []byte(HostSettings(tc.entry, tc.runRegistry))) })
	}
}
```

The golden files, in the order of the table:

```toml
# hosts-as-mirror-ca-credentials.golden.toml
server = "https://mirror.corp.example/registry-example"
ca = "/etc/containerd/certs.d/registry.example/ca.pem"
[host."http://10.89.0.5:5000"]
  capabilities = ["pull"]
```

```toml
# hosts-mirror-prefix.golden.toml
server = "https://mirror.corp.example/registry-example"
```

```toml
# hosts-mirror-v2.golden.toml
server = "https://cache.corp.example/v2/proxy-project"
override_path = true
ca = "/etc/containerd/certs.d/registry.example/ca.pem"
```

```toml
# hosts-mirror-v2-only.golden.toml (decision 6: a first segment v2, alone)
server = "https://mirror.example/v2"
override_path = true
```

```toml
# hosts-mirror-prefix-v2.golden.toml (decision 6)
server = "https://mirror.corp.example/cache/v2/v2"
override_path = true
```

```toml
# hosts-mirror-root.golden.toml
server = "https://mirror.corp.example:8443"
```

```toml
# hosts-docker-io-mirror.golden.toml
server = "https://mirror.corp.example/docker-io"
ca = "/etc/containerd/certs.d/docker.io/ca.pem"
[host."http://10.89.0.5:5000"]
  capabilities = ["pull"]
```

```toml
# hosts-docker-io-ca.golden.toml (no server line: containerd keeps registry-1.docker.io)
ca = "/etc/containerd/certs.d/docker.io/ca.pem"
```

```toml
# hosts-ca.golden.toml
ca = "/etc/containerd/certs.d/registry.example/ca.pem"
```

```toml
# hosts-as-ca.golden.toml
ca = "/etc/containerd/certs.d/registry.example/ca.pem"
[host."http://10.89.0.5:5000"]
  capabilities = ["pull"]
```

```toml
# hosts-as-port-ca.golden.toml
ca = "/etc/containerd/certs.d/registry.example:5000/ca.pem"
[host."http://10.89.0.5:5000"]
  capabilities = ["pull"]
```

```toml
# hosts-ipv6-port-ca.golden.toml (decision 14)
ca = "/etc/containerd/certs.d/[fd00::5]:5000/ca.pem"
```

The comment lines above name the files; they are not part of them. Each file ends with a line end. The last file pins only what the tool writes for a bracketed IPv6 host: containerd's read of the folder `certs.d/[fd00::5]:5000` is not run, in any record or in any test of this plan.

```go
func TestHostSettingsNeedsNoFile(t *testing.T) {
	// HostSettings(config.Registry{Host: "docker.io", Username: "ci", Password: "pw"}, "") == ""
	// HostSettings(config.Registry{Host: "registry.example"}, "") == ""
}

func TestHostSettingsOfAnAsHostWithNoEntry(t *testing.T) {
	// HostSettings(config.Registry{Host: "registry.example"}, "10.89.0.5:5000") == HostsTOML("10.89.0.5:5000")
	// and HostsTOML still matches hosts-test.golden.toml (TestHostsTOMLGolden, unchanged).
}

func TestHostSettingsTakesNothingFromAnotherEntry(t *testing.T) {
	// The entry a.example with mirror b.example/x and no ca gives exactly
	// "server = \"https://b.example/x\"\n", whatever an entry for b.example holds (section 9).
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cluster/ -run 'TestHostSettings|TestHostsTOMLGolden' -v`
Expected: FAIL: `HostSettings` is not defined.

- [ ] **Step 3: Implement `HostSettings` in `internal/cluster/hosts.go`, with its interface comment from the Interfaces block, and `HostsTOML` as a call of it**

The top-level keys come before the run's table, in the order `server`, `override_path`, `ca`, because TOML puts every key after a table into that table. A `ca` is written as `/etc/containerd/certs.d/HOST/ca.pem`, with the host as written (decision 7); a helper `caPath(host string) string` gives it, for Task 5 too. The mirror's path decides `override_path` (decision 6): a path whose first segment is `v2`, `v2` alone included, is written as it is, with `override_path = true`; a path whose last segment is `v2` and whose first is not is written with `/v2` after it, with `override_path = true`; any other path, or none, is written as it is, with no `override_path`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cluster/ -v` and `go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cluster/hosts.go internal/cluster/hosts_test.go internal/cluster/testdata/hosts-as-mirror-ca-credentials.golden.toml internal/cluster/testdata/hosts-mirror-prefix.golden.toml internal/cluster/testdata/hosts-mirror-v2.golden.toml internal/cluster/testdata/hosts-mirror-v2-only.golden.toml internal/cluster/testdata/hosts-mirror-prefix-v2.golden.toml internal/cluster/testdata/hosts-mirror-root.golden.toml internal/cluster/testdata/hosts-docker-io-mirror.golden.toml internal/cluster/testdata/hosts-docker-io-ca.golden.toml internal/cluster/testdata/hosts-ca.golden.toml internal/cluster/testdata/hosts-as-ca.golden.toml internal/cluster/testdata/hosts-as-port-ca.golden.toml internal/cluster/testdata/hosts-ipv6-port-ca.golden.toml
git commit -m "Write a node's settings file for each shape of a registries entry"
```

### Task 5: The credentials file, its mount, and every registry file in the nodes

**Files:**
- Create: `internal/cluster/credentials.go`, `internal/cluster/credentials_test.go`, `internal/cluster/testdata/credentials.golden.json`, `internal/cluster/testdata/kind-credentials.golden.yaml`
- Modify: `internal/cluster/kindconfig.go` (`Spec`, `KindConfig`), `internal/cluster/cluster.go` (`Create`), `internal/cluster/kindconfig_test.go`, `internal/cluster/create_test.go`, `internal/cluster/cluster_test.go`

**Interfaces:**
- Consumes: `HostSettings`, `caPath` (Task 4).
- Produces: `func Credentials(regs []config.Registry) []byte`
  Returns the kubelet's credentials file for the nodes, by decision 9: for
  each entry with a username, its host as written and the base64 of
  USERNAME:PASSWORD. Entries without credentials are left out; nil when no
  entry has any.
- Produces: `type Spec struct { …; Registries []config.Registry; AsHosts []string }`
  Registries are the configuration's entries; AsHosts are the hosts that an
  entry of images names in as. Create writes, into every node, each host's
  settings and ca, with the run's registry first for each host of AsHosts,
  and mounts the credentials file in every node when one is needed.
- Produces: `func KindConfig(s Spec, rootless bool) *v1alpha4.Cluster`
  As before; with an entry that has credentials, every node also mounts
  RUNDIR/node-credentials.json read-only at /var/lib/kubelet/config.json.
- Produces: `func Create(ctx context.Context, s Spec, e Engine) (*Cluster, error)`
  As before, and: the credentials file is written, mode 0600, into the
  run's folder before kind creates the nodes; each node gets every ca and
  settings file with rollproof.test's. A file that cannot be read or
  written fails the step it belongs to, cluster-failed, as today.

- [ ] **Step 1: Write the failing tests**

```go
func TestCredentialsGolden(t *testing.T) {
	regs := []config.Registry{
		{Host: "registry.example", Username: "ci2", Password: "pw2"},
		{Host: "docker.io", Username: "ci", Password: "pw1"},
		{Host: "mirror-only.example", Mirror: "mirror.corp.example/x"},
	}
	golden(t, "credentials.golden.json", Credentials(regs))
	// The golden file:
	// {
	//   "auths": {
	//     "docker.io": {
	//       "auth": "Y2k6cHcx"
	//     },
	//     "registry.example": {
	//       "auth": "Y2kyOnB3Mg=="
	//     }
	//   }
	// }
	// and Credentials(nil) and Credentials of mirror-only.example alone are nil.
}

func TestKindConfigMountsTheCredentials(t *testing.T) {
	s := Spec{Name: "rollproof-x", RunDir: "/tmp/rollproof-x", Workers: 3,
		Registries: []config.Registry{{Host: "registry.example", Username: "ci", Password: "pw"}}}
	// encodeKind(t, KindConfig(s, true)) matches kind-credentials.golden.yaml and reads back as
	// TestKindConfigGolden reads its files back; every node has exactly one extra mount,
	// {ContainerPath: "/var/lib/kubelet/config.json", HostPath: "/tmp/rollproof-x/node-credentials.json", Readonly: true}.
	// With Registries holding no credentials, no node has a mount (the old golden files hold).
}
```

In `create_test.go`, in the style of `TestCreateFailureTexts`, with `pullImage`, `kindCreate`, `kindDelete` and `kindCollectLogs` replaced:

```go
func TestCreateWritesTheCredentialsFileBeforeKind(t *testing.T) {
	// Spec.Registries holds {registry.example, ci, pw}. The fake kindCreate reads
	// RUNDIR/node-credentials.json when it is called: its bytes equal Credentials(s.Registries)
	// and its mode is 0600. It then returns an error, so Create ends cluster-failed with the step
	// "create the kind cluster", and the run's folder is gone afterwards.
	// With no credentials in Spec.Registries, the fake finds no such file.
}
```

In `cluster_test.go` (tag `cluster`), with a CA written by a test helper like Task 1's `writeCA`:

```go
func TestCreateWritesRegistrySettings(t *testing.T) {
	// s := testSpec("registries", 1) with
	//   Registries: {registry.example, ci, pw, CA: <the CA file>, Mirror: mirror.corp.example/registry-example}
	//               and {docker.io, CA: <the CA file>};
	//   AsHosts:    registry.example and registry.example:5000.
	// Right after Create returns, defer c.Delete(ctx), as TestCreateAndDelete does.
	// After Create, in each node (c.Exec cat):
	//   /etc/containerd/certs.d/registry.example/hosts.toml      == HostSettings(Registries[0], c.NodeRegistry)
	//   /etc/containerd/certs.d/registry.example/ca.pem          == the CA file's bytes
	//   /etc/containerd/certs.d/docker.io/hosts.toml             == HostSettings(Registries[1], "")
	//   /etc/containerd/certs.d/registry.example:5000/hosts.toml == HostsTOML(c.NodeRegistry)
	//   /etc/containerd/certs.d/rollproof.test/hosts.toml        == HostsTOML(c.NodeRegistry)
	//   /var/lib/kubelet/config.json                             == Credentials(s.Registries)
	// and every node is Ready (Create returned).
}
```

- [ ] **Step 2: Run the unit tests to verify they fail**

Run: `go test ./internal/cluster/ -run 'TestCredentialsGolden|TestKindConfigMountsTheCredentials|TestCreateWritesTheCredentialsFileBeforeKind' -v`
Expected: FAIL: `Credentials`, `Spec.Registries` and `Spec.AsHosts` are not defined.

- [ ] **Step 3: Implement `Credentials` in `credentials.go` and the changes to `Spec`, `KindConfig` and `Create`, each with its interface comment from the Interfaces block**

`Credentials` marshals a map with `json.MarshalIndent` and two spaces, then a line end. The file's name is a constant `credentialsName = "node-credentials.json"`. In `Create`, right after the run's folder is made: when `Credentials(s.Registries)` is not nil, write it with mode 0600, failing the step "write the nodes' credentials file". In the node loop, before `rollproof.test`'s file: for each entry with a `ca`, `os.ReadFile(entry.CA)` once (before the loop) and `nodeutils.WriteFile` to `caPath(host)`; then, for each entry and then each host of `AsHosts` that no entry names, `HostSettings` with `c.NodeRegistry` when the host is in `AsHosts`, written to `/etc/containerd/certs.d/HOST/hosts.toml` when not empty. A failure fails the step "write the node settings" with the node's name, as today; a `ca` that cannot be read names its key, `registries[i].ca`.

Then write the golden file of the mount: `go test ./internal/cluster/ -run TestKindConfigMountsTheCredentials -update`, and read `testdata/kind-credentials.golden.yaml` against the test's mount: each of the four nodes has `extraMounts` with that one entry, `containerPath: /var/lib/kubelet/config.json`, `hostPath: /tmp/rollproof-x/node-credentials.json` and `readOnly: true`, and the file is otherwise `kind-rootless.golden.yaml`.

- [ ] **Step 4: Run the unit tests to verify they pass**

Run: `go test ./internal/cluster/ -v` and `go vet ./...` and `go vet -tags cluster ./...`
Expected: PASS.

- [ ] **Step 5: Run the cluster test**

Run: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 ./internal/cluster/ -run TestCreateWritesRegistrySettings -v -timeout 20m`
Expected: PASS, and `nerdctl ps -a` lists no container whose name begins with `rollproof-test-registries`.

- [ ] **Step 6: Commit**

```bash
git add internal/cluster/credentials.go internal/cluster/credentials_test.go internal/cluster/kindconfig.go internal/cluster/cluster.go internal/cluster/kindconfig_test.go internal/cluster/create_test.go internal/cluster/cluster_test.go internal/cluster/testdata/credentials.golden.json internal/cluster/testdata/kind-credentials.golden.yaml
git commit -m "Write the registries' settings, certificates and credentials file into every node"
```

### Task 6: `as` in `images`

**Files:**
- Modify: `internal/images/images.go` (`Store`), `internal/images/archive.go` (`storeArchive`), `internal/images/local.go` (`storeLocal`)
- Modify: `internal/images/own_test.go` (remove `TestStoreRefusesWhatIsNotBuilt`), `internal/images/archive_test.go` (the new tests; the calls of `storeArchive` in `TestStoreArchivePushesByDigest` and `TestStoreArchiveFailures`), `internal/images/images_test.go`

**Interfaces:**
- Consumes: `cluster.Spec.AsHosts` (Task 5).
- Produces: `func Store(ctx context.Context, c *cluster.Cluster, binary string, images map[string]config.Image, timeout time.Duration) (Stored, error)`
  As before, and: an entry with as goes into the run's registry under the
  path of its repository after the host, and Stored names it by the as
  itself (decision 11). The cluster must hold that host in Spec.AsHosts,
  else a node's pull goes past the run's registry and fails cluster-failed.

- [ ] **Step 1: Write the failing tests**

In `archive_test.go`:

```go
func TestPlaceOf(t *testing.T) {
	for _, tc := range []struct {
		entry      string
		im         config.Image
		path, repo string
	}{
		{"orders", config.Image{Local: "orders:candidate"}, "orders", "rollproof.test/orders"},
		{"orders", config.Image{Local: "orders:candidate", As: "registry.example/team/orders"}, "team/orders", "registry.example/team/orders"},
		{"x", config.Image{Archive: "/a.tar", As: "docker.io/library/orders"}, "library/orders", "docker.io/library/orders"},
		{"x", config.Image{Local: "a:b", As: "registry.example:5000/a/b"}, "a/b", "registry.example:5000/a/b"},
	} {
		// placeOf(tc.entry, tc.im) == place{path: tc.path, repository: tc.repo}
	}
}

func TestStoreArchiveUnderAs(t *testing.T) {
	c := archiveRegistry(t)
	file := archiveOf(t, 1)
	// storeArchive(ctx, c, "orders", placeOf("orders", config.Image{Archive: file, As: "registry.example/team/orders"}), file, "the archive")
	// returns {Repository: "registry.example/team/orders", Digest: D}; the run's registry answers
	// GET /v2/team/orders/manifests/D with 200 and GET /v2/orders/manifests/D with 404, and
	// lists no tag for team/orders.
}
```

In `images_test.go` (tag `cluster`), `testCluster`'s Spec gains `AsHosts: []string{"registry.example:5000", "docker.io"}`, and:

```go
func TestStoreAsIntoEveryNode(t *testing.T) {
	// One image built and saved as TestStoreArchiveIntoEveryNode does, under a tag of its own,
	// stored under two entries:
	//   a: {Archive: <the file>, As: "registry.example:5000/team/app"}
	//   b: {Archive: <the file>, As: "docker.io/rollproof/app"}
	// Store succeeds, which means every node pulled each through the settings folders
	// certs.d/registry.example:5000 and certs.d/docker.io (Review Focus 3); s.Images["a"].Repository
	// is the as; crictl inspecti of each Ref() succeeds in each node.
}
```

- [ ] **Step 2: Run the unit tests to verify they fail**

Run: `go test ./internal/images/ -run 'TestPlaceOf|TestStoreArchiveUnderAs' -v`
Expected: FAIL: `placeOf` is not defined.

- [ ] **Step 3: Implement `placeOf` and `as` in `Store`, with `Store`'s interface comment from the Interfaces block**

`type place struct{ path, repository string }` and `placeOf(entry string, im config.Image) place` in `images.go`: with no `As`, the entry's name under `cluster.TestHost`; with `As`, the part after its first `/` and the `As` itself. `storeArchive(ctx context.Context, c *cluster.Cluster, entry string, at place, file, what string)` pushes under `at.path` and returns `at.repository`. Its two callers in `archive_test.go`, `TestStoreArchivePushesByDigest` and `TestStoreArchiveFailures`, pass `placeOf("orders", config.Image{Archive: …})` with the file each uses, which is the place of today (`orders`, `rollproof.test/orders`). `storeLocal(ctx context.Context, c *cluster.Cluster, entry string, at place, local string)` passes the place on to `storeArchive`. `Store` works out each entry's place once, with `placeOf`, and hands it to `storeArchive` or `storeLocal`. `Store` no longer refuses an entry with `As`, and its node pull is unchanged: `crictl pull` of the returned reference.

- [ ] **Step 4: Run the unit tests to verify they pass**

Run: `go test ./internal/images/ -v` and `go vet -tags cluster ./...`
Expected: PASS.

- [ ] **Step 5: Run the cluster tests of the package**

Run: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 ./internal/images/ -v -timeout 20m`
Expected: PASS, `TestStoreAsIntoEveryNode` included. If the node cannot pull the image under `registry.example:5000`, stop and report to the maintainer with the node's output: decision 7 rests on a reading of containerd's source.

- [ ] **Step 6: Commit**

```bash
git add internal/images/images.go internal/images/archive.go internal/images/local.go internal/images/own_test.go internal/images/archive_test.go internal/images/images_test.go
git commit -m "Store an image with as under its repository's path in the run's registry"
```
