# Tasks 11 to 14: the mirror job

Read section 12 ("The mirror job"), section 13 (the workflows during incubation), section 14, point 30 ("The mirror job"), the Method and Decision of `docs/proofs/p02b-mirror-forms.md` (cases C and D: names through the engine's `/etc/hosts`, the cut with `iptables-nft`), and decisions 16 to 21 of the README before these tasks. The job is a test of the suite under the build tag `mirror`; it runs on this machine (rootless nerdctl) and, when the maintainer says so, on GitHub (Docker).

### Task 11: The stand-in registry

**Files:**
- Create: `fixtures/standin/main.go`, `fixtures/standin/main_test.go`, `fixtures/standin/Dockerfile`

**Interfaces:**
- Consumes: `github.com/google/go-containerregistry`'s `pkg/registry`, `pkg/v1/remote`, `pkg/v1/tarball`, `pkg/name`.
- Produces: `module fixtures/standin`
  A stand-in registry for the mirror job: an in-memory registry behind TLS,
  a Basic login and path prefixes, filled from image archives before it
  listens, writing a line for each connection and each request. It stands
  for an organization's mirror or for a real host; it is never released.
- Produces: `func newHandler(reg http.Handler, prefixes []string, user, password string, log io.Writer) http.Handler`
  Serves reg behind a Basic login; with prefixes, only paths under one of
  them, the prefix taken off, and 404 for any other. No or wrong
  credentials get 401 and WWW-Authenticate: Basic realm="rollproof-mirrorjob".
  After each answer it writes REQ METHOD PATH user=USER status=CODE, with
  the path as received and no query, and USER - when absent.
- Produces: `func logConnections(l net.Listener, log io.Writer) net.Listener`
  Returns l, writing CONN ADDRESS for each connection it accepts, before
  any byte of it is read.
- Produces: `type load struct { repository, archive string }`
  One image to put in place before listening: the archive's one image,
  under the repository.
- Produces: `func loadImages(ctx context.Context, registry string, loads []load) error`
  Pushes each archive's one image to the plain-HTTP registry at registry
  (HOST:PORT) under its repository, by digest, writing LOAD REPO@DIGEST on
  standard output for each. An archive with no image, or more than one, is
  an error that names it.
- Produces: `func check(ctx context.Context, url string, limit time.Duration) error`
  Asks GET url every 200 ms, with no certificate check, until any HTTP
  answer comes, and returns nil then; an error when limit passes first.

The program's flags: `-listen ADDR` (TLS), `-cert FILE`, `-key FILE`, `-user NAME` with the password from the variable `STANDIN_PASSWORD` (an error when it is empty), `-prefix PATH` (repeated), `-load REPOSITORY=ARCHIVE` (repeated), and `-check URL`, which runs `check` with a limit of 30 s and exits 0 or 1. Without `-check` it starts the registry on a listener of `127.0.0.1` of its own, runs `loadImages` against it, then serves `newHandler` on `logConnections` of the TLS listener, and writes `READY`.

- [ ] **Step 1: Write the failing tests**

```go
func TestHandlerStripsThePrefix(t *testing.T) {
	// reg answers 200 with the path it saw. Through newHandler(reg, []string{"/docker-io", "/real"},
	// "u", "p", log), with Basic u:p: GET /docker-io/v2/x/manifests/d reaches reg as /v2/x/manifests/d;
	// /real/v2/ reaches it as /v2/; /other/v2/ and /v2/ are 404 and never reach reg. With no
	// prefixes, /v2/ reaches reg as /v2/.
}

func TestHandlerAsksForALogin(t *testing.T) {
	// No Authorization, and Basic u:wrong, give 401 with the header
	// `WWW-Authenticate: Basic realm="rollproof-mirrorjob"`, and reg is not called.
}

func TestHandlerLogsEachRequest(t *testing.T) {
	// HEAD /docker-io/v2/x/manifests/d?ns=docker.io with u:p, then GET /real/v2/ with none, log
	//   "REQ HEAD /docker-io/v2/x/manifests/d user=u status=200\n"
	//   "REQ GET /real/v2/ user=- status=401\n"
}

func TestLogConnections(t *testing.T) {
	// Two connections to logConnections(net.Listen("tcp", "127.0.0.1:0"), log) give two lines
	// "CONN 127.0.0.1:PORT".
}

func TestLoadImages(t *testing.T) {
	// A random image written by tarball.WriteToFile, loaded under rollproof/fixture into an
	// httptest registry: GET /v2/rollproof/fixture/manifests/DIGEST answers 200. An archive of two
	// images is an error that names its path.
}

func TestCheck(t *testing.T) {
	// An httptest TLS server that answers 401: check returns nil. A closed port with a limit of
	// 500 ms: an error.
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./fixtures/standin/ -v`
Expected: FAIL: the package does not compile.

- [ ] **Step 3: Implement `fixtures/standin/main.go`, each name with its interface comment from the Interfaces block, and the Dockerfile**

The in-memory registry is `registry.New(registry.Logger(log.New(io.Discard, "", 0)))`. The Dockerfile is `FROM scratch`, `COPY standin /standin`, `ENTRYPOINT ["/standin"]`, with no `USER` line: the stand-in reads files that the runner mounts in, which a rootless engine maps to the container's root alone.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./fixtures/standin/ -v` and `go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add fixtures/standin/main.go fixtures/standin/main_test.go fixtures/standin/Dockerfile
git commit -m "Add the stand-in registry of the mirror job"
```

### Task 12: The harness's rendering of the job, the cut, the authority and the search

**Files:**
- Modify: `e2e/harness_test.go` (the build tag becomes `//go:build e2e || mirror`; `runFixture` also writes standard error as `stderr.txt` beside `console.txt`; `expect`, moved here from `fixtures_test.go`)
- Modify: `e2e/fixtures_test.go` (`expect` moved out), `e2e/config_test.go` (the fixture's new fields, `configTemplate`, `renderConfig`, `TestConfigOfEachShapeLoads`)
- Create: `e2e/mirror_helpers_test.go` (no build tag)

**Interfaces:**
- Consumes: `fixture`, `renderConfig`, `inputs` as step 4 left them.
- Produces: `type fixture struct { …; registries []map[string]string; images map[string]map[string]string; literal map[string]literalImage }`
  registries is the run's registries block, left out when empty. images,
  when not nil, is the images block as it is, an empty map included. literal
  gives, by role (previous or candidate), a release image named outright in
  place of the images entry's forms.
- Produces: `type literalImage struct { repository, digest string }`
  An image a release's values name outright: its repository and digest.
- Produces: `type mirrorInputs struct { engine, certs, archives, ca, previousDigest, candidateDigest string }`
  What the job's two fixtures need from outside: the engine's command, the
  folder of the authority's files, the folder of the fixture archives, the
  CA's path, and the digests of the previous and candidate images.
- Produces: `func throughTheMirror(in mirrorInputs) fixture`
  The first fixture through the mirror, by decisions 16 to 21: docker.io and
  the real host's stand-in with mirrors, a login and the CA; the previous
  image named after docker.io; the candidate through images with as on
  docker.io; the setup commands of mirrorSetup; Helm 3; limits.timeout 3m.
- Produces: `func mirrorReplacesItsHost(in mirrorInputs) fixture`
  As throughTheMirror, with no images and a candidate named after the real
  host's stand-in, which the mirror lacks (decision 19).
- Produces: `func mirrorSetup(in mirrorInputs) [][]string`
  The job's setup commands, in order: start both stand-ins, wait for each,
  cut the route in each node, resolve the real host's name in a node, and
  print the password plainly and in base64.
- Produces: `const cutScript = …`
  The script that cuts a node's route to everything but the engine
  network's subnets, kind's pod and service subnets, loopback, and IPv6
  link-local and multicast; it fails when it finds no IPv4 subnet or when
  its chain is missing afterwards.
- Produces: `func newAuthority(dir string) (caPath string, err error)`
  Writes ca.pem, server.pem and server-key.pem into dir, mode 0644: a CA
  and a server certificate it signed for both stand-ins' names and
  127.0.0.1, valid for one day. Returns ca.pem's path.
- Produces: `func newPassword() (string, error)`
  Returns 20 random lower-case letters and digits followed by "&<, so that
  the JSON and Go spellings differ from the plain one.
- Produces: `func passwordForms(user, password string) []string`
  Every form to search for, each once: plain, the base64 of USER:PASSWORD
  padded and not, the JSON spellings with and without HTML escaping, and
  the Go spelling.
- Produces: `func findForms(root string, forms []string) ([]string, error)`
  Walks every regular file under root and returns, sorted, "PATH holds form
  I" for each file that holds a form, PATH relative to root; none is empty.
- Produces: `type standinRequest struct { method, path, user string; status int }`
  One REQ line of a stand-in's log.
- Produces: `func parseStandinLog(text string) (conns []string, reqs []standinRequest)`
  The addresses of the CONN lines and the REQ lines of a stand-in's log, in
  order; other lines are skipped.

The names, as constants in `mirror_helpers_test.go`: `mirrorName = "rollproof-mirrorjob-mirror"`, `realName = "rollproof-mirrorjob-real"`, `mirrorHost = mirrorName + ":8443"`, `realHost = realName + ":5443"`, `jobUser = "rollproof"`, `passwordVar = "ROLLPROOF_MIRROR_PASSWORD"`, `standinImage = "rollproof-fixture-standin:dev"`.

`throughTheMirror` gives `registries`:

```go
[]map[string]string{
	{"host": "docker.io", "username": jobUser, "password": "${env." + passwordVar + "}", "ca": in.ca, "mirror": mirrorHost + "/docker-io"},
	{"host": realHost, "username": jobUser, "password": "${env." + passwordVar + "}", "ca": in.ca, "mirror": mirrorHost + "/real"},
}
```

`images` `{"candidate": {"local": "rollproof-fixture:candidate", "as": "docker.io/rollproof/fixture"}}`, `literal` `{"previous": {"docker.io/rollproof/fixture", in.previousDigest}}`. `mirrorReplacesItsHost` has `images` `{}` and also `literal["candidate"] = {realHost + "/team/candidate", in.candidateDigest}`.

`mirrorSetup`, with E the engine and each node `rollproof-e2e-control-plane`, `-worker`, `-worker2`, `-worker3`:

```go
{E, "run", "-d", "--name", mirrorName, "--network", "kind", "-v", in.certs + ":/certs:ro", "-v", in.archives + ":/images:ro",
	"-e", "STANDIN_PASSWORD=${env." + passwordVar + "}", standinImage, "-listen", ":8443", "-cert", "/certs/server.pem",
	"-key", "/certs/server-key.pem", "-user", jobUser, "-prefix", "/docker-io", "-prefix", "/real",
	"-load", "rollproof/fixture=/images/previous.tar"},
{E, "run", "-d", "--name", realName, /* the same mounts, variable, certificate and user */ "-listen", ":5443",
	"-load", "team/candidate=/images/candidate.tar"},
{E, "exec", mirrorName, "/standin", "-check", "https://127.0.0.1:8443/v2/"},
{E, "exec", realName, "/standin", "-check", "https://127.0.0.1:5443/v2/"},
{E, "exec", NODE, "sh", "-c", cutScript}, // once for each of the four nodes
{E, "exec", "rollproof-e2e-worker", "getent", "hosts", realName},
{"sh", "-c", `printf '%s\n' "$` + passwordVar + `"; printf '%s' "` + jobUser + `:$` + passwordVar + `" | base64 -w0; echo`},
```

`cutScript`, exact (p02b's rules, with the subnets read from the node's own routes, decision 18):

```sh
set -e
v4=$(ip -4 -o route show dev eth0 scope link | cut -d' ' -f1)
v6=$(ip -6 -o route show dev eth0 | cut -d' ' -f1)
[ -n "$v4" ] || { echo "rollproof: no IPv4 subnet on eth0" >&2; exit 1; }
iptables-nft -N ROLLPROOF-CUT
iptables-nft -A ROLLPROOF-CUT -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
iptables-nft -A ROLLPROOF-CUT -o lo -j RETURN
for net in $v4 10.244.0.0/16 10.96.0.0/16; do
  case "$net" in */*) iptables-nft -A ROLLPROOF-CUT -d "$net" -j RETURN;; esac
done
iptables-nft -A ROLLPROOF-CUT -p tcp -j REJECT --reject-with tcp-reset
iptables-nft -A ROLLPROOF-CUT -j REJECT --reject-with icmp-port-unreachable
iptables-nft -I OUTPUT 1 -j ROLLPROOF-CUT
iptables-nft -I FORWARD 1 -j ROLLPROOF-CUT
ip6tables-nft -N ROLLPROOF-CUT
ip6tables-nft -A ROLLPROOF-CUT -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
ip6tables-nft -A ROLLPROOF-CUT -o lo -j RETURN
for net in $v6 fe80::/10 ff00::/8; do
  case "$net" in */*) ip6tables-nft -A ROLLPROOF-CUT -d "$net" -j RETURN;; esac
done
ip6tables-nft -A ROLLPROOF-CUT -p tcp -j REJECT --reject-with tcp-reset
ip6tables-nft -A ROLLPROOF-CUT -j REJECT --reject-with icmp6-port-unreachable
ip6tables-nft -I OUTPUT 1 -j ROLLPROOF-CUT
ip6tables-nft -I FORWARD 1 -j ROLLPROOF-CUT
iptables-nft -S ROLLPROOF-CUT >/dev/null
ip6tables-nft -S ROLLPROOF-CUT >/dev/null
```

- [ ] **Step 1: Write the failing tests**

In `mirror_helpers_test.go`:

```go
func TestCutScript(t *testing.T) {
	// exec.Command("sh", "-n", "-c", cutScript) exits 0; the script holds "-I OUTPUT 1 -j ROLLPROOF-CUT"
	// and "-I FORWARD 1 -j ROLLPROOF-CUT" for both families, "10.244.0.0/16", "10.96.0.0/16",
	// "fe80::/10", "ff00::/8", "--reject-with tcp-reset", and its last line checks the IPv6 chain.
}

func TestNewAuthority(t *testing.T) {
	// The server certificate verifies against ca.pem for the DNS names mirrorName and realName and
	// for the IP 127.0.0.1; server-key.pem is its key (tls.LoadX509KeyPair); every file is 0644.
}

func TestPasswordForms(t *testing.T) {
	// newPassword gives 23 bytes ending in `"&<`, a new one each call; passwordForms("rollproof", pw)
	// holds pw, base64 of "rollproof:"+pw padded and not, json.Marshal's spelling without its quotes,
	// the spelling without HTML escaping, and strconv.Quote's, each once.
}

func TestFindForms(t *testing.T) {
	// Under a temporary folder, a/b.log holds form 1 and c.txt holds none:
	// findForms gives []string{"a/b.log holds form 1"}.
}

func TestParseStandinLog(t *testing.T) {
	// "READY\nCONN 10.4.1.2:40000\nREQ HEAD /docker-io/v2/x/manifests/d user=rollproof status=200\nLOAD x@d\n"
	// gives conns []string{"10.4.1.2:40000"} and one request {HEAD, /docker-io/v2/x/manifests/d, rollproof, 200}.
}
```

In `config_test.go`, `TestConfigOfEachShapeLoads` gains the two mirror shapes, made from `mirrorInputs` whose `ca` is `newAuthority(t.TempDir())`'s and whose digests are `sha256:` and 64 hex digits, with `t.Setenv(passwordVar, "pw")`. Each loads; `cfg.Registries` holds the two entries with their mirrors; `throughTheMirror`'s `cfg.Images["candidate"].As` is `docker.io/rollproof/fixture`; `mirrorReplacesItsHost` has no images, and its candidate's values name `realHost + "/team/candidate"` and the candidate's digest.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./e2e/ -v`
Expected: FAIL: the job's names are not defined.

- [ ] **Step 3: Implement the fields, the rendering and the helpers, each name with its interface comment from the Interfaces block**

`configTemplate` gains `registries: {{.Registries}}` after `cluster`, when set. `renderConfig` uses `f.images` in place of its computed block when it is not nil, and, for a role in `f.literal`, values `{"image": {"repository": …, "digest": …}}` in place of the `${images.ENTRY.…}` forms. `newAuthority` uses ECDSA P-256 keys and `crypto/x509`; the CA's key is never written. `passwordForms` dedupes in order.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./e2e/ -v` and `go vet -tags e2e ./...` and `go vet -tags mirror ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add e2e/harness_test.go e2e/fixtures_test.go e2e/config_test.go e2e/mirror_helpers_test.go
git commit -m "Render the mirror job's runs, its cut, its certificate authority and its password search"
```

### Task 13: The mirror job

**Files:**
- Create: `e2e/mirror_test.go` (`//go:build mirror`)

**Interfaces:**
- Consumes: every name of Task 12; `runFixture`, `expect`, `readInstall`, `outDir`, `buildImage`, `engine`, `archives`, `archiveDigests` of the harness.
- Produces: `func TestMirror(t *testing.T)`
  The mirror job of section 12: two runs of the real binary through a
  mirror that needs a login, sits under a path and serves TLS from a
  private authority, with the nodes' route to public registries cut, each
  ending with the search for the password in everything it wrote.

- [ ] **Step 1: Write the test**

```go
func TestMirror(t *testing.T) {
	buildImage(standinImage, "./fixtures/standin", "-s -w", "standin")
	certs := t.TempDir()
	ca, err := newAuthority(certs)
	if err != nil {
		t.Fatal(err)
	}
	pw, err := newPassword()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(passwordVar, pw)
	// The engine holds the node image and the registry image that `bin/rollproof version` names
	// on its lines "node image REF" and "registry image REF" before the run: each is pulled now
	// when `E image inspect REF` fails, so that the run asks the engine for no pull (decision 18).
	in := mirrorInputs{engine: engine, certs: certs, archives: filepath.Dir(archives["previous"]), ca: ca,
		previousDigest: archiveDigests["previous"], candidateDigest: archiveDigests["candidate"]}
	forms := passwordForms(jobUser, pw)

	t.Run("through the mirror", func(t *testing.T) {
		// Before: `E rm -f -v` of mirrorName and realName. Cleanup: when the test failed, log both
		// stand-ins' logs; then remove both.
		r := runFixture(t, throughTheMirror(in))
		expect(t, r, result{exit: 0, verdict: "pass",
			windows: map[string]string{"steady": "pass", "deploy": "pass", "rollback": "pass"}})
		// The mirror's log (E logs mirrorName): a request with path
		// "/docker-io/v2/rollproof/fixture/manifests/" + in.previousDigest, user "rollproof", status 200;
		// no request whose path holds in.candidateDigest, which the run's registry served.
		// The real host's log: no CONN from an address other than 127.0.0.1.
		// findForms(outDir(t), forms) is empty; a file under outDir(t)/evidence/commands holds "***".
	})

	t.Run("a mirror replaces its host", func(t *testing.T) {
		// The same before and cleanup.
		r := runFixture(t, mirrorReplacesItsHost(in))
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"transition-stalled"},
			decided: "transition-stalled",
			windows: map[string]string{"steady": "pass", "deploy": "fail", "rollback": "not-run"}})
		// A detail line of the transition-stalled reason (readInstall) holds
		// "only from its mirror " + mirrorHost + "/real".
		// The mirror's log: a request with path "/real/v2/team/candidate/manifests/" +
		// in.candidateDigest, user "rollproof", status 404.
		// The real host's log: no CONN from an address other than 127.0.0.1 (R25).
		// findForms(outDir(t), forms) is empty; a file under outDir(t)/evidence/commands holds "***".
	})
}
```

- [ ] **Step 2: Vet it**

Run: `go vet -tags mirror ./...`
Expected: no output.

- [ ] **Step 3: Run the job on this machine**

Run: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m`
Expected: PASS for both subtests; afterwards `nerdctl ps -a` lists no container whose name begins with `rollproof-e2e-` or `rollproof-mirrorjob-`.

**Stop rule.** When a subtest fails because the job's environment does not behave as the proofs recorded (a node that cannot resolve a stand-in's name, a cut that the script cannot apply, a settings folder with a port that containerd does not read, credentials under a key with a port that the kubelet does not match), stop and report to the maintainer with the results folder and the stand-ins' logs. Do not change the expected results to make the job pass.

- [ ] **Step 4: Commit**

```bash
git add e2e/mirror_test.go
git commit -m "Add the mirror job: a run through a mirror that needs a login, and a mirror that replaces its host"
```

### Task 14: The workflow and the documentation

**Files:**
- Create: `.github/workflows/rollproof-mirror.yml` (from the repository root)
- Modify: `README.md`, `e2e/doc.go`, `docs/proofs/README.md`

**Interfaces:**
- Consumes: `TestMirror` (Task 13).
- Produces: nothing that code calls. The workflow runs the mirror job on GitHub, started only by hand, on a hosted Ubuntu runner with Docker, as section 13 says for incubation.

- [ ] **Step 1: Write the workflow**

`.github/workflows/rollproof-mirror.yml`, with the actions pinned as `rollproof-unit.yml` pins them:

```yaml
name: rollproof mirror job

on:
  workflow_dispatch:

permissions:
  contents: read

defaults:
  run:
    shell: bash
    working-directory: projects/rollproof

jobs:
  mirror:
    runs-on: ubuntu-24.04
    timeout-minutes: 60
    env:
      CGO_ENABLED: "0"
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: "1.27"
          cache-dependency-path: projects/rollproof/go.sum
      - name: Helm and kubectl
        run: go run ./hack/fetchhelm
      - name: The mirror job
        run: go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m
```

- [ ] **Step 2: Write the documentation**

In `README.md`, after "What works now", one paragraph:

> `registries` tells the cluster's nodes how to pull from a host. A `mirror` replaces the host: the nodes pull its images from the mirror alone, and a mirror that lacks an image fails the pull. A `ca` adds a certificate authority for the place the nodes pull from. `username` and `password` go into a credentials file that every node mounts and that any pod in the cluster can read, so give credentials that can only read and expire soon. Write Docker Hub as `docker.io`. `as` in `images` stores an image under the repository that the real manifests name, so the manifest under test is the one that deploys. A registry password is masked as `***` in the console, in `result.json` and in the evidence folder, in its plain form and in each form the tool writes it in; nothing else is masked. A mirror that the nodes must reach without a proxy goes into the caller's own `NO_PROXY`, which kind passes into the nodes (read in kind's source, not run).

and, after the paragraph on the suite:

> The mirror job, `go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m` (with `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before it on nerdctl), runs the first fixture through a mirror that needs a login, sits under a path and serves TLS from a private authority, with the nodes' route to public registries cut, and then a run whose candidate the mirror lacks. Its workflow in the host repository, `rollproof-mirror.yml`, starts only by hand.

In `e2e/doc.go`, a paragraph: the mirror job carries the tag `mirror`, shares `harness_test.go` with the suite, and runs with the command above.

In `docs/proofs/README.md`, below the "Still unproved" line for `p02-mirror-credentials` and `p02b-mirror-forms`: "Run since by tests of the plan `docs/plans/2026-11-20-step5/`, not as proofs: a settings folder for a host with a port and `as` on Docker Hub (Task 6's cluster test), and a Docker Hub path other than `library/` (the mirror job, Task 13)."

- [ ] **Step 3: Run the checks**

Run: `go test ./...`, `go vet ./...`, `go vet -tags cluster ./...`, `go vet -tags e2e ./...`, `go vet -tags mirror ./...`; then, from the repository root, `bun tools/citations/cli.ts repo`.
Expected: PASS, no vet output, and the citation check exits 0. An exit code of 2 means the check did not run: report it as unchecked, never as a pass.

- [ ] **Step 4: Commit**

```bash
git add README.md e2e/doc.go docs/proofs/README.md
git add ../../.github/workflows/rollproof-mirror.yml
git commit -m "Add the mirror job's workflow, started by hand, and document registries, as and masking"
```
