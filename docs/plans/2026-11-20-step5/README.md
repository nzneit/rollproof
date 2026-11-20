# rollproof: enterprise settings (build-order step 5) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build step 5 of the design's build order: `registries` (the forms and rejections of section 8, the nodes' mirror, certificate and credential settings), `as` in `images`, the masking of every registry password in everything the tool writes, the mirror named in the text of a stall, of an install that was not ready and of a probe that never started, and the mirror job.

**Architecture:** `config` checks the three forms and the `ca` when the file loads, and answers two questions for the other units: which hosts an `as` names, and what sentence names the mirror of an image's host. `cluster` turns each entry into the node files that proofs p02 and p02b ran (the mirror as the `server` line, the `ca` at the top, the run's registry as the only `[host]` block) and mounts a credentials file at creation; `images` stores an `as` image under the repository's path. A new package `mask` holds every form of every password; `report.Writer` applies it to everything it writes and `run` to every line it prints, so no other unit masks for itself. The mirror job is an e2e test under the tag `mirror`: stand-in registries started by `setup` commands, the nodes' route cut by p02b's script, and two runs.

**Tech Stack:** As built: Go 1.27, `k8s.io/client-go`, `k8s.io/api` and `k8s.io/apimachinery` v0.37, `sigs.k8s.io/kind` v0.33.0 as a library, `github.com/google/go-containerregistry` v0.22.1 (its `pkg/registry` serves the stand-ins), `go.yaml.in/yaml/v3`, `github.com/google/go-cmp` in tests, `crypto/x509` for the job's certificate authority; Helm 3.22.1 and 4.3.2 and kubectl v1.37.0 for the fixtures, fetched by `hack/fetchhelm`. No new Go dependency.

**Spec:** `projects/rollproof/docs/specs/2026-11-13-rollproof-design.md`, at `53fcfd5` of the branch `rollproof-step5-design`. Section numbers in this plan are that document's. Section 14, points 30 and 31, are the maintainer's rulings for this step and override anything older. An executor reads the sections a task names before it starts the task. Where the design is silent, the decisions of `docs/plans/2026-11-16-first-plan/README.md`, the rulings of `docs/plans/2026-11-17-install-window/README.md`, the decisions of `docs/plans/2026-11-19-step4/README.md` and section 14's points 24 to 29 stand, and so do this plan's decisions below. The proof records `docs/proofs/p01-registry-order.md`, `p01b-mirror.md`, `p02-mirror-credentials.md`, `p02b-mirror-forms.md` and `p19-proxy.md` hold the node files and the cut that this plan writes.

## Global Constraints

Every task's requirements include these.

- **Linux only.** Module `rollproof`, every package under `internal/` except `cmd/rollproof`. Apache-2.0. No file under `projects/rollproof/` cites a row of the host repository's decision log or follows its service rules. The workflow file `.github/workflows/rollproof-mirror.yml` sits outside `projects/`, so the host repository's rules apply to it, and its citation check must pass.
- **Paths and commands.** Commands run from `projects/rollproof/` unless a step says "from the repository root". Every command behaves the same in bash and fish: no unquoted glob, no `VAR=value` prefix (write `env VAR=value command`), no `$(…)`.
- **The binary is static.** `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof`.
- **No new configuration key, no new reason code, no new schema version.** Step 5 builds keys that version 1 already has. `result.json` stays at `schema` 1 and gains no field.
- **Reason codes,** in the order of section 10's list: `probe-failures`, `transition-stalled`, `pod-unschedulable`, `command-failed` (fail); `config-rejected`, `runner-unsupported`, `cluster-failed`, `chart-fetch-failed`, `setup-failed`, `install-failed`, `nothing-watched`, `workload-unsupported`, `target-unsupported`, `probe-not-started`, `probe-exited`, `probe-line-invalid`, `steady-not-clean`, `probe-silent`, `no-pod-replaced`, `restart-failed`, `interrupted`, `internal-error` (not verified).
- **Names that are contract** (sections 6 and 9): the namespace `rollproof-probe`, the container `probe`, the prefix `rollproof-`, the registry host `rollproof.test`, the file `rollproof.yaml`; in every node, the kubelet's credentials file `/var/lib/kubelet/config.json` and the settings file `/etc/containerd/certs.d/HOST/hosts.toml`.
- **The forms of section 8,** verbatim: "An entry's `host` is a registry host as an image's name writes it, in lower case, with no scheme and no path: a DNS name that holds a dot, a DNS name or an IP address with a port, or `localhost`. An IPv6 address is written in brackets." "Hosts are compared as written, with their ports, so `registry.example` and `registry.example:443` are two entries; only the check of a mirror against its own entry's host takes a port of 443 as none." "`as` is a repository as an image's name writes it, with its host written out: a host in the form above, then a path in the form of an image's name, with no tag and no digest. On `docker.io` the path has two segments or more." "An entry's `mirror` is a host in the same form, with or without a path, and no scheme: HTTPS is implied. The path's segments hold letters, digits, `-`, `.`, `_` and `~`; no segment is empty, `.` or `..`, and the path has no `/` at its end. A path whose first segment is `v2` holds the registry API's root; any other path is a prefix in front of it."
- **A listed mirror replaces the real host** (section 9): "the nodes pull that host's images from the mirror and never go on to the host itself." The order for one host: "1. the run's registry, when an entry of `images` names that host in `as`. It may serve a pull and may not resolve a tag; 2. the entry's mirror when it has one, and the real host otherwise. It may do both."
- **Secrets** (section 8), verbatim: "`registries[].password` must be a single `${env.NAME}`; a password written in the file is rejected, and the text that rejects any configuration never quotes a value. The tool masks every such password in everything it writes, in its plain form and in the encoded form in which the nodes' credentials file holds it. That includes the output of commands and the pod logs that it copies into the evidence." "The tool masks nothing else."
- **Defaults** (section 8): `cluster.workers` 3; `probe.http.rate` 200; `probe.http.timeout` 2s; `limits.steady` 10s; `limits.settle` 15s; `limits.timeout` 5m; `limits.silence` 5s.
- **Rule 10.** After the verdict is decided, nothing changes it: an error while the tool writes the report, completes the evidence or deletes the cluster is a warning, on the console and in `result.json`.
- **Tests.** Unit tests need no cluster and no network and run with `go test ./...`. A test that needs a container engine carries the build tag `cluster`; a fixture run carries `e2e`; the mirror job carries `mirror`. Run those with `-p 1`, one cluster at a time (`docs/proofs/p12-keys.md`). On this machine put `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before the command. Never set `GOOS` for `go run` or `go test`. Vet with each tag: `go vet ./...`, `go vet -tags cluster ./...`, `go vet -tags e2e ./...`, `go vet -tags mirror ./...`.
- **Commits.** One commit per task, staged by path. A message is a plain sentence that says what changed. It carries no attribution to an AI tool: no `Co-Authored-By` trailer that names one, no "Generated with" line, no session link.
- **Nothing leaves the machine.** No push, no pull request and no run on GitHub Actions without the maintainer's word. A container, image, network or volume whose name does not begin with `rollproof-` is never removed. The mirror job's stand-ins are named `rollproof-mirrorjob-mirror` and `rollproof-mirrorjob-real`.

## Review Focus

The design implies these and names no test for them. Each has its test in the task that owns the code.

1. **A Docker Hub image written in another form than the entry's host.** A pod's container waits for `orders`, `library/orders` or `index.docker.io/library/orders` while `registries` has a `docker.io` entry with a mirror: the stall's text names that mirror, as it does for `docker.io/library/orders`. Test in Task 7 (`TestMirrorNoteForEachDockerHubName`).
2. **A password that JSON or Go quoting changes, or that overlaps another.** A password with `"`, `\`, `<` or `&`, a password that holds another entry's password, and a password equal to its username are masked in every form, and the longer one is masked whole. Test in Task 2 (`TestOverlappingPasswords`, `TestFormsOfAPassword`).
3. **A host or an `as` with a port.** For `registry.example:5000` the node's settings folder is `certs.d/registry.example:5000`, which containerd reads after `registry.example_5000_` (read in its source); a node pulls an `as` image stored under such a host. Test in Task 6 (`TestStoreAsIntoEveryNode`, tag `cluster`).
4. **A `ca` that is not a certificate.** A `ca` that is missing, a folder, empty, a PEM private key alone, or a certificate in DER is rejected with the configuration, before a cluster exists, with the key's name and no value. Test in Task 1 (`TestCARejects`).
5. **A stream that ends inside a held-back tail.** An evidence file whose last bytes are a password's first bytes, or a password that ends exactly at the file's end, is written whole at `Close`, masked where a form is complete and unchanged where it is not. Test in Task 2 (`TestWriterAtClose`).

## What this plan builds, and what it leaves

| Unit | In this plan | Left for a later plan |
| --- | --- | --- |
| `config` | The forms of `host`, `as` and `mirror`, every rejection of section 8 for them, the `ca` checked when the file loads; `AsHosts`, `ImageHost`, `MirrorNote`; `NotBuilt` removed once both keys are wired | Nothing |
| `mask` (new) | Every form of every password, masked in text and in streams | Nothing |
| `cluster` | The settings file and `ca` file for each host in every node; the credentials file, mounted at creation | Nothing |
| `images` | `as`: stored under the repository's path, pulled into every node through the host's settings | Nothing |
| `watch`, `probe` | The mirror named in the stall's, the install's and the probe's start texts | Nothing |
| `report` | `result.json` masked field by field, evidence masked as bytes; the golden file for a stall with a mirror | The job summary (step 6) |
| `run` | The masker set right after the configuration loads; every line printed masked | Nothing |
| Tests with a cluster | The registry settings in every node; `as` on a host with a port and on `docker.io`; the mirror job (two runs) | The action's test and the other workflows, runs on Docker, D11 (step 6) |
| `docs/`, README | The README's account of `registries`, `as`, masking and the mirror job | Nothing |

## Decisions made in this plan

The design left these open or silent. Each is the smaller choice. The maintainer reviews them with the plan; the ones marked **public** change something a user depends on.

1. **Each key leaves the list of what the build cannot run in the task that wires it from end to end:** `registries` in Task 9, once the node settings, the credentials file, masking and the mirror's name are joined; `images.*.as` in Task 10, once the `as` hosts reach the cluster. Task 10 then removes `NotBuilt` and its check in `run`, because no key of version 1 is left unbuilt (step 4, decision 1). A key accepted earlier would run without masking, or store an `as` image that no node's settings route.
2. **public: what is masked, and with what.** Each form of a password is replaced by `***`. The forms are the plain text; its JSON spelling with HTML escaping (as `json.Marshal` writes it, in `probe-pod.json` for one) and without (as an encoder with HTML escaping turned off writes it); its Go spelling (as `strconv.Quote` writes it, which is how the commands' argument lines quote an argument); and the base64 of `USERNAME:PASSWORD` in the standard alphabet without padding, which also covers the padded form the credentials file holds (its padding is left after `***`). A spelling equal to another is held once. Nothing else is masked: no username, no URL encoding, no base64 of the password alone.
3. **public: where masking applies.** The console, standard error, `result.json` and every file of the evidence folder. Not the run's folder, whose credentials file must hold the password and which only the user can read and the cluster's deletion removes (section 9). A stream holds back its last bytes, fewer than the longest form, until more come or it is closed; a stream that is never closed, as after a second signal, loses that tail.
4. **How the masker reaches each writer.** `run` builds one `mask.Masker` from the loaded configuration and hands it to `report.Writer` before anything that could hold a password is written; the writer masks `Final`, `Evidence` and `ReplaceEvidence`, and `run` masks each line it prints. No unit masks for itself: every unit already writes its evidence through the function that `run` gives it (step 4's interfaces). `result.json` is masked field by field before it is encoded, every string but the fields of a fixed vocabulary (decision 22), so its keys, numbers and literals stay valid whatever a password is; evidence is masked as bytes.
5. **public: the settings file for each shape of entry.** For host H, with ADDRESS the run's registry and `as` meaning that an entry of `images` names H in `as`:

   | The entry has | `/etc/containerd/certs.d/H/hosts.toml` |
   | --- | --- |
   | a `mirror` | `server = "https://MIRROR"`, then `override_path = true` for a `v2` path (decision 6), then `ca = "…/H/ca.pem"` with a `ca`, then the run's block with `as` |
   | no `mirror`, a `ca` | `ca = "…/H/ca.pem"`, then the run's block with `as`; no `server` line |
   | no `mirror`, no `ca`, `as` | the run's block alone |
   | credentials alone, or nothing | no file |

   The run's block is `[host."http://ADDRESS"]` with `capabilities = ["pull"]`. The tool never writes a `server` line that names H itself, so an entry for `docker.io` with only a `ca` or only credentials keeps containerd's default place, `registry-1.docker.io` (`p01-registry-order.md`, its warning about `server`). Credentials never enter this file (point 30: no header). An `as` host with no entry gets the run's block alone, as `rollproof.test` does today.
6. **public: a mirror's path that ends in the segment `v2`.** containerd takes any `server` path that ends in `/v2` as the API's root and adds nothing to it (`hosts.go` at v2.3.4, `parseHostConfig`). For a prefix whose last segment, and not its first, is `v2`, the tool writes the path with `/v2` after it and `override_path = true`, so that the path stays a prefix as section 9 says. Not run.
7. **public: the settings folder of a host with a port** is the host as written, `certs.d/registry.example:5000`; containerd tries `registry.example_5000_` first and then this name (`config_unix.go` at v2.3.4, `hostPaths`). Task 6's cluster test pulls through such a folder.
8. **public: a `ca` is checked when the file loads.** It must be a regular file that holds at least one PEM block of type `CERTIFICATE` that parses as X.509; other text around the blocks is allowed. A `ca` that does not exist, is not a regular file, or holds no such block is rejected with the configuration, before a cluster exists, with the key's name and no value (as step 4, decision 2, did for an archive).
9. **public: the credentials file.** `{"auths":{HOST:{"auth":BASE64}}}`, keyed by each entry's host as written and holding only the entries with credentials, with `BASE64` the standard base64 of `USERNAME:PASSWORD`, indented by two spaces. It is `node-credentials.json` in the run's folder, mode 0600, written before kind creates the nodes and mounted read-only at `/var/lib/kubelet/config.json` in every node. When no entry has credentials there is no file and no mount, and the kind description is the one of today.
10. **The nodes' registry files are written** after the run's registry starts and before the ready wait, in the loop that writes `rollproof.test`'s file today: for each node, each entry's `ca` at `/etc/containerd/certs.d/H/ca.pem` and then each host's `hosts.toml`. No restart is needed (p01).
11. **public: `as` storage.** The image goes into the run's registry under the path after the host (`registry.example/team/orders` under `team/orders`, `docker.io/library/orders` under `library/orders`), by digest; `${images.NAME.repository}` is the `as` itself. Two entries may share a path: a node asks by digest.
12. **public: the mirror in the text, and where its name comes from.** `config.(*Config).MirrorNote(image)` returns "the nodes pull HOST only from its mirror MIRROR, and a pull's error may name the image alone" when the image's host, read as the nodes read it (no host, or `index.docker.io`, is `docker.io`), has an entry with a mirror, and "" otherwise. `wire` passes it to `watch` and `probe`. They add "; " and the sentence after a listed container that waits with the reason `ErrImagePull` or `ImagePullBackOff`, taking the image from the pod's spec for the container of that name: in the stall's detail lines, in the text of an install that was not ready, and in the text of a probe that never started.
13. **public: Docker Hub's other names and `rollproof.test` are rejected as written.** `index.docker.io:443` and `rollproof.test:5000` are other hosts by section 8's comparison and are accepted; the first never applies in a node (finding 3).
14. **public: a bracketed IPv6 address is a host with or without a port,** because an image's name writes `[fd00::5]/team/orders` as a host (finding 2).
15. **public: the details of the forms.** A DNS label is lower-case letters and digits with hyphens inside; no label is empty and the name has no dot at its end. A port is digits, 1 to 65535. An `as` path is components of lower-case letters and digits joined by `.`, `_`, `__` or dashes, separated by `/`. Upper case is reported before any other fault of the form, so the text says why.
16. **The mirror job's stand-ins.** One program, `fixtures/standin`, built into `rollproof-fixture-standin:dev` from scratch and run as root, so that it reads files the runner mounts in under a rootless engine. It serves `go-containerregistry`'s in-memory registry over TLS from a certificate authority that the test makes, asks for a Basic login, serves only paths under its prefixes and strips them, loads images from the harness's archives before it listens, and logs each request and each connection. The mirror is `rollproof-mirrorjob-mirror:8443` with the prefixes `/docker-io` and `/real`; the stand-in for a real host is `rollproof-mirrorjob-real:5443`. The names carry a port because a host needs a dot or a port. No token service: proof p02b ran the token flow, and the tool writes the same files whatever the challenge; a token service would have to sit on the engine's network too (p02b, Decision).
17. **public: the job's public host is `docker.io`.** The previous release is `docker.io/rollproof/fixture@DIGEST`, which only the mirror holds; the candidate enters through `images` with `as: docker.io/rollproof/fixture`. The job so also runs `as` on Docker Hub and a Docker Hub path other than `library/`, which p02b did not.
18. **The job cuts the nodes' route, and not the engine's.** `setup` commands run p02b's `iptables-nft` script in each node through the engine's `exec`; the script reads the engine network's subnets from the node's own routes, adds kind's default pod and service subnets (`10.244.0.0/16`, `10.96.0.0/16`, which the tool does not change), and fails when it found no subnet or its chain is missing. The engine's route is not cut: the engine pulls on the runner's own network, the one the GitHub runner reports through and the one this machine's engine serves everything else from, so a cut would end the job or change the machine (finding 1). In its place the job makes sure the engine holds the node image and the registry image before the run, so the run asks the engine for no pull (point 28).
19. **The job shows that a mirror replaces its host** (re-read finding R25) with a second run: its candidate is named after `rollproof-mirrorjob-real:5443`, which holds it, and the mirror lacks it. The run fails, `transition-stalled`; the stall's detail names the mirror; the real host's stand-in logs no connection from outside its own container. A `setup` command shows that a node resolves the real host's name; it does not connect.
20. **The job's password and its search.** A password of 20 random lower-case letters and digits followed by `"&<`, new for each test run, in `ROLLPROOF_MIRROR_PASSWORD`, for the user `rollproof`. A `setup` command's argument line carries it substituted (the stand-in's start) and another command prints it plainly and in base64. After each run the job searches every file of the results folder, the console and standard error for the plain form, both base64 forms and the JSON and Go spellings, expects none, and expects `***` in the commands' evidence.
21. **The job runs under Helm 3 only,** with `limits.timeout: 3m`; Helm plays no part in a node's pull.
22. **public: the fields of `result.json` that are never masked** are those of a fixed vocabulary: `verdict`, `driver`, each reason's `code`, `verdict` and `window`, each window's `name` and `result`, each transition's `name`, `command` and `restart`, each workload's `probed`, each probe's `kind`, each release's `role`, each `run_by`'s `transition`, and the cluster's `provider`, and every map key. They carry the struct tag `mask:"keep"`.
23. **public: documentation.** The README gains a paragraph on `registries`, `as`, masking and a mirror behind a proxy (re-read finding R29: kind passes the caller's own `NO_PROXY` into the nodes, read in its source and not run), and how to run the mirror job.

## What writing this plan found in the spec

None is blocking; each has the smaller choice above.

1. **Section 12, "The mirror job":** the job "cuts the route from the nodes and the engine to public registries." The engine's pulls leave on the runner's own network, which the job cannot cut without ending the GitHub job or changing this machine (decision 18). The text could say that the job cuts the nodes' route and makes sure the engine needs no pull.
2. **Section 8, Types, and point 31:** "a host holds a dot or a port or is `localhost`" leaves out a bracketed IPv6 address with no port, which an image's name writes as a host by its colons. The plan accepts it (decision 14).
3. **Section 8:** hosts "are compared as written, with their ports", so `index.docker.io:443` passes the rejection of Docker Hub's other names and then never applies in a node, which is what point 31's ruling meant to prevent. The plan follows the text (decision 13).
4. **Section 9:** "any other path is a prefix in front of it" meets containerd's rule that a path ending in `/v2` is the API's root. The plan writes such a prefix explicitly (decision 6); no record ran it.
5. **Not run in any record:** the form "`ca` at the top, no `server` line, and the run's block" (an `as` host with a `ca` and no mirror). p02 case e1 ran the `ca` line alone and p01 the run's block alone; `hosts.go` puts the top-level fields on the default place either way. A golden file pins the form; Task 6's cluster test runs the run's block and not the `ca`.
6. **Section 8, "Secrets":** "everything it writes" would include the run's folder, whose credentials file must hold the password. The plan masks the console, standard error and the results folder (decision 3).
7. **A short password.** Masking replaces every occurrence, so a password such as `a` or `pass` garbles the console and the evidence. `result.json` keeps its keys and its vocabulary (decision 22). The spec sets no shortest password.
8. **`docs/proofs/README.md`, "Still unproved":** a settings folder for a host with a port, `as` on Docker Hub and a Docker Hub path other than `library/` are run by this plan's tests (Tasks 6 and 13). Task 14 records that beside the list.

## File structure

All paths are under `projects/rollproof/` unless they begin with `.github/`.

| Path | Change | Task |
| --- | --- | --- |
| `internal/config/registries.go` (new), `validate.go`, `testdata/full.yaml`, `testdata/corp-ca.pem` (new) | The forms, the `ca`, `AsHosts` | 1 |
| `internal/mask/mask.go` (new) | `Masker` | 2 |
| `internal/report/mask.go` (new), `record.go`, `result.go`, `evidence.go` | `Mask`, `SetMasker` | 3 |
| `internal/cluster/hosts.go`, `testdata/hosts-*.golden.toml` (new) | `HostSettings` | 4 |
| `internal/cluster/credentials.go` (new), `kindconfig.go`, `cluster.go`, `testdata/credentials.golden.json`, `testdata/kind-credentials.golden.yaml` (new) | `Credentials`, the mount, `Spec.Registries`, `Spec.AsHosts`, the files in `Create` | 5 |
| `internal/images/images.go`, `archive.go`, `local.go` | `as` | 6 |
| `internal/config/mirrors.go` (new), `internal/watch/watcher.go`, `tracker.go`, `counts.go` | `ImageHost`, `MirrorNote`; the note in `watch` | 7 |
| `internal/probe/pod.go`, `internal/report/testdata/console-stall-mirror.golden.txt` (new) | The note in `probe`; the golden file | 8 |
| `internal/run/run.go`, `cmd/rollproof/wire.go`, `internal/config/notbuilt.go` | Masking in `run`; `registries` wired | 9 |
| `cmd/rollproof/wire.go`, `internal/config/notbuilt.go` and its test (removed), `internal/run/run.go`, `e2e/config_test.go`, `README.md` | `as` wired; `NotBuilt` removed | 10 |
| `fixtures/standin/` (new) | The stand-in registry | 11 |
| `e2e/harness_test.go`, `e2e/config_test.go`, `e2e/mirror_helpers_test.go` (new, no build tag) | The job's rendering, cut, authority and search | 12 |
| `e2e/mirror_test.go` (new, tag `mirror`) | The two runs | 13 |
| `.github/workflows/rollproof-mirror.yml` (new), `README.md`, `e2e/doc.go`, `docs/proofs/README.md` | The workflow and the documentation | 14 |

## The tasks

One plan in seven files. Read this file first, then the file of the task at hand. Tasks are numbered through all files and are done in order.

| File | Tasks | What |
| --- | --- | --- |
| `01-config.md` | 1 | The forms of `host`, `as` and `mirror`, and the `ca` |
| `02-mask-and-report.md` | 2 to 3 | `mask`, and the report's writer |
| `03-cluster-and-images.md` | 4 to 6 | The node files, the credentials file, `as` |
| `04-mirror-note.md` | 7 to 8 | The mirror in the texts |
| `05-run-and-wire.md` | 9 to 10 | `registries` and `as` from end to end |
| `06-mirror-job.md` | 11 to 14 | The stand-in, the harness, the two runs, the workflow and the documentation |

| Task | What it delivers |
| --- | --- |
| 1 | Every rejection of section 8 for `host`, `as`, `mirror` and `ca`; `(*config.Config).AsHosts` |
| 2 | `mask.New`, `(*Masker).String`, `(*Masker).Writer` |
| 3 | `report.Mask`, `(*report.Writer).SetMasker` |
| 4 | `cluster.HostSettings` and the golden files of section 12 |
| 5 | `cluster.Credentials`, the mount, and `Create` writing every registry file |
| 6 | `images.Store` with `as` |
| 7 | `config.ImageHost`, `(*config.Config).MirrorNote`, the note in `watch` |
| 8 | The note in `probe`; the report's golden file for a stall with a mirror |
| 9 | Masking in `run`; `registries` wired and off `NotBuilt` |
| 10 | `as` wired; `NotBuilt` removed |
| 11 | The stand-in registry program |
| 12 | The harness's rendering of the job, the cut, the authority and the search |
| 13 | The mirror job, run on this machine |
| 14 | `rollproof-mirror.yml`, the README, `e2e/doc.go`, the proofs index |

## Interfaces between tasks

The names that one task produces and a later one uses. Each task's Interfaces block repeats the ones it touches with their interface comments.

- `(*config.Config).AsHosts() []string` (Task 1): the hosts that the entries of `images` name in `as`, sorted, each once.
- `mask.Credential{Username, Password string}`, `mask.New(creds []mask.Credential) *mask.Masker`, `(*mask.Masker).String(s string) string`, `(*mask.Masker).Writer(w io.WriteCloser) io.WriteCloser`, `mask.Replacement` (`"***"`) (Task 2). A nil `*Masker` masks nothing.
- `report.Mask(r report.Run, m *mask.Masker) report.Run` and `(*report.Writer).SetMasker(m *mask.Masker)` (Task 3).
- `cluster.HostSettings(entry config.Registry, runRegistry string) string` (Task 4): "" means no file.
- `cluster.Credentials(regs []config.Registry) []byte`, `cluster.Spec.Registries []config.Registry`, `cluster.Spec.AsHosts []string` (Task 5).
- `images.Store` stores an entry with `As` (Task 6); its signature does not change.
- `config.ImageHost(image string) string`, `(*config.Config).MirrorNote(image string) string`, `watch.Options.MirrorNote func(image string) string`, `watch.TrackerOptions.MirrorNote func(image string) string` (Task 7).
- `probe.PodOptions.MirrorNote func(image string) string` (Task 8).
- `run`'s `writer` interface gains `SetMasker(*mask.Masker)`; `cmd/rollproof`'s `clusterSpec(cfg *config.Config, n string, keep bool, evidence func(string) (io.WriteCloser, error)) cluster.Spec`, with `n` the cluster's full name (Task 9; `AsHosts` in Task 10).
- The stand-in's log lines (Task 11): `CONN ADDRESS`, `REQ METHOD PATH user=USER status=CODE`, `LOAD REPOSITORY@DIGEST`, `READY`.
- The job's names in `e2e`: `fixture.registries`, `fixture.images`, `fixture.literal`, `literalImage`, `mirrorInputs`, `throughTheMirror`, `mirrorReplacesItsHost`, `mirrorSetup`, `cutScript`, `newAuthority`, `newPassword`, `passwordForms`, `findForms`, `standinRequest`, `parseStandinLog`, and the constants `mirrorName`, `realName`, `mirrorHost`, `realHost`, `jobUser`, `passwordVar`, `standinImage` (Task 12).

## Alternatives considered

### `internal/mask`

Common case: a writer of the results folder, or a line of the console, holds a registry password, and what reaches the disk or the terminal holds `***` in its place.

**A. A masker that knows the forms, for text and for streams**

- `mask.New(creds []mask.Credential) *mask.Masker`
- `(*Masker).String(s string) string`
- `(*Masker).Writer(w io.WriteCloser) io.WriteCloser`

```go
m := mask.New([]mask.Credential{{Username: "ci", Password: pw}})
fmt.Fprint(stdout, m.String(text))

f = m.Writer(f) // a pod's log, followed while the pod lives
```

**B. The list of forms, applied by each writer** (expected to lose)

- `mask.Forms(creds []mask.Credential) []string`

```go
r := strings.NewReplacer(pairs(mask.Forms(creds), "***")...)
fmt.Fprint(stdout, r.Replace(text))

// a stream: each writer keeps its own tail of up to the longest form
```

| | A. Masker | B. Forms |
| --- | --- | --- |
| Ease of use for the common caller | One call per text or per stream | Each writer builds a replacer, and each stream its own tail |
| Interface size | A constructor and two operations | One function |
| Generality | Text and streams, one rule for overlapping forms | Text only; a form split across two writes leaks |
| Information hidden | Which forms exist, the order of overlapping ones, the tail | Every caller sees the forms |
| Implementation simplicity | One scanner used by both operations | Simple, then repeated in each writer |
| Efficiency | One pass | One pass, but a stream must copy its tail itself |

Third design: not needed; A leaves the caller nothing to do.

Pick: A.

Losers: B, because a password split across two writes of a stream would pass each writer that forgot the tail, and section 12's test plants exactly that.

### How the masker reaches each writer

Common case: a unit writes a pod's log, an event list or a command's output into the evidence, and `run` writes `result.json` and the console, each after the configuration loaded.

**A. `report.Writer` masks what it writes; `run` masks what it prints**

- `(*report.Writer).SetMasker(m *mask.Masker)`; `report.Mask(r report.Run, m *mask.Masker) report.Run`

```go
s.mask = mask.New(credentialsOf(cfg))
s.w.SetMasker(s.mask)
fmt.Fprint(s.o.Stdout, report.Console(report.Mask(rec, s.mask)))
```

**B. Each unit masks its own writers** (expected to lose)

- `watch.Options.Mask`, `probe.PodOptions.Mask`, `cluster.Spec.Mask`, `deploy.RunnerOptions.Mask`, and `run` for the rest

```go
watch.Start(ctx, client, watch.Options{Mask: m, Evidence: ev})
```

| | A. One seam | B. Every unit |
| --- | --- | --- |
| Ease of use for the common caller | Units unchanged | Each unit wraps each writer |
| Interface size | Two names in `report` | One field per unit, five units |
| Generality | Every file of the folder, a new writer included | Only the writers that remember |
| Information hidden | Units never see a password's forms | Each unit holds the masker |
| Implementation simplicity | One place, one test of every file | Five places, five tests |
| Efficiency | Equal | Equal |

Third design: not needed.

Pick: A.

Losers: B, because step 4 added six writers in four units, and a seventh added later would leak with no test failing.

### `internal/cluster`'s registry files

Common case: `Create` writes, into every node, what each listed host and each `as` host needs, and golden files pin each shape.

**A. One pure function per file**

- `HostSettings(entry config.Registry, runRegistry string) string`
- `Credentials(regs []config.Registry) []byte`

```go
for _, e := range entries(s.Registries, s.AsHosts) {
	if text := HostSettings(e, runRegistryFor(e.Host)); text != "" { write(e.Host, text) }
}
```

**B. One function that gives every node file** (expected to lose)

- `NodeFiles(regs []config.Registry, asHosts []string, runRegistry string) (map[string][]byte, error)`

```go
files, err := NodeFiles(s.Registries, s.AsHosts, c.NodeRegistry)
for path, b := range files { write(path, b) }
```

| | A. Per file | B. All files |
| --- | --- | --- |
| Ease of use for the common caller | A loop over hosts | One call |
| Interface size | Two functions | One function |
| Generality | Each shape is a golden file with no disk | Reads every `ca` from disk, so a test needs files |
| Information hidden | Paths of a node stay in `Create` | Paths of a node leave in the map's keys |
| Implementation simplicity | Pure | Pure only if the `ca` is read elsewhere |
| Efficiency | Equal | Equal |

Third design: not needed.

Pick: A.

Losers: B, because it either reads the `ca` files, which makes every golden test touch the disk, or hands the caller paths it must not change.

Canary kept: 2: `HostSettings` and `Credentials` return the content of a file that the node's runtime and the kubelet read; that form is the contract the proofs ran, not a hidden layout.

### How the mirror's name reaches the texts

Common case: a pod's container waits for an image the nodes could not pull, and the text that lists it names the mirror of the image's host.

**A. A function from the configuration, given to the units that write the texts**

- `(*config.Config).MirrorNote(image string) string`
- `watch.Options.MirrorNote`, `probe.PodOptions.MirrorNote`, each `func(image string) string`

```go
watch.Start(ctx, client, watch.Options{MirrorNote: cfg.MirrorNote})
probe.PodOptions{MirrorNote: cfg.MirrorNote}
```

**B. `run` rewrites the texts afterwards** (expected to lose)

- `run.nameMirrors(text string, cfg *config.Config) string`, which finds image names in a reason's text

```go
reason.Text = nameMirrors(reason.Text, s.cfg)
```

**C. The units return the waiting containers, and `report` writes the sentence**

- `watch.Transition.Waiting []watch.WaitingImage`, `probe.Started.Waiting`, `report.Reason.Mirrors []string`

```go
for _, w := range tr.Waiting { out.Detail = append(out.Detail, note(w)) }
```

| | A. Function | B. Rewrite | C. Structured |
| --- | --- | --- | --- |
| Ease of use for the common caller | One field set in `wire` | One call, after the fact | Three new fields, and code in `run` to join them |
| Interface size | One method, two fields | One function | A type and three fields |
| Generality | Every text that lists a container | Only texts whose image names it can parse | Every text, through `run` |
| Information hidden | `watch` and `probe` never learn `registries` | `run` parses the texts it was handed | `report` learns pod states |
| Implementation simplicity | One sentence in one place | Fragile matching | Plumbing through three units |
| Efficiency | Equal | Equal | Equal |

Third design: C is the third; it removes nothing A leaves.

Pick: A.

Losers: B, because a container's line names no image; it cannot know which line to change. C, because it moves the texts' facts through `run` and `report` for one clause.

### `images.Store` with `as`

Common case: `run` asks `Store` to put every entry in place; an entry with `as` must land under the repository's path and come back as the `as` itself.

**A. `Store` reads `As` and works out the path**

- `Store(ctx, c, binary, images, timeout)` as today

**B. The caller hands `Store` each entry's target** (expected to lose)

- `Store(ctx, c, binary, images, targets map[string]string, timeout)`

| | A. Inside | B. Targets |
| --- | --- | --- |
| Ease of use for the common caller | Unchanged | `wire` builds a map |
| Interface size | Unchanged | One more argument |
| Generality | Every entry | Every entry |
| Information hidden | The run's registry's paths stay in `images` | The caller learns them |
| Implementation simplicity | A helper of four lines | The same helper, in `wire` |
| Efficiency | Equal | Equal |

Third design: not needed.

Pick: A.

Losers: B, because the path in the run's registry is `images`' own business (section 5).

Design delta: better, because the masking that section 8 asks of "everything it writes" lives at the one seam every writer already passes, the node files are pure functions with a golden file per shape, and `NotBuilt` leaves the code once nothing is unbuilt.
