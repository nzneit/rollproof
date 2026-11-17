# Proofs

A proof settles an open point of section 14 of the design before code rests on it. Its program is under `proofs/`, in the module `rollproof-proofs`, and runs from that folder; its record is in this folder. Each program makes one cluster named `rollproof-proof-…`, one at a time, and deletes it, also when the program fails.

Versions in the proofs module: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go`, `k8s.io/api` and `k8s.io/apimachinery` v0.37.2; `github.com/google/go-containerregistry` v0.22.1.

## The form of a record

Every record has these headings, in this order:

- `## Question`: the open point, in one or two sentences.
- `## Machine and versions`: the system, the engine and whether it runs without root, Go, kind, the node image, and every other tool the method used, each with its version.
- `## Method`: what the program does, and the exact command that ran it.
- `## Result`: what happened, with the lines of output that show it.
- `## Decision`: what the plan does because of the result.

It ends with a pair of lines for each thing the plan assumed, each line starting at the beginning of its line:

    Plan assumed: "…"
    Holds: yes

`Holds:` is followed by `yes`, `no` or `not measured here`.

## The list

Each row is filled from its record. A row whose record has several assumptions holds only when all of them do.

| Point | Question | Result | Decision | Plan assumed | Holds | Tasks that rest on it |
| --- | --- | --- | --- | --- | --- | --- |
| 1, first half | Does a node's settings file that lists the run's registry first serve a digest, pass a tag to the real host, and does a pod's status name the digest it runs? | Digest references were served by the run's registry, tag references went to the real host, an image the run's registry lacked came from the real host, `crictl pull` used the node's settings, a settings file written into a running node needed no restart, and `imageID` named the digest. | Task 20 writes the run's registry first with no `server` line; Tasks 21 and 22 pull with `crictl pull`; the report compares only the digest after the `@` of `imageID`. Measured only with images of one manifest each (no image index), one container per pod, and no image held under two names. | p01-registry-order, 4 assumptions: (1) "For a host, the node's settings file lists the run's registry as `[host.\"http://ADDRESS:5000\"]` with `capabilities = [\"pull\"]`, before the real host. A reference by digest is then served by the run's registry; a reference by tag is not answered by it and goes to the real host; a reference with a tag and a digest is pulled by the digest alone; an image the run's registry lacks is fetched from the real host." (2) "The tool has a node pull with `crictl pull REFERENCE`, run in the node. That pull uses the node's registry settings, the ones the kubelet's pulls use." (3) "A settings file written into a running node is used by the next pull, with no restart." (4) "A pod's status gives, in imageID, the digest of the image each container runs." | yes | Tasks 20, 21, 22, 29 |
| 1, second half | Can one settings file list the run's registry, an HTTPS mirror with a private authority, and the real host, in order, and what does a mirror under a path or a cache with a project path need? | A node tried the run's registry, then the mirror (under a path prefix, with `override_path` for a project path), in the order of the file, as measured with a `server` line in the settings file. | Step 5 writes the file with no `server` line; the record proposes that form and it was not run. | "For one host the node's settings file lists, in this order, the run's registry (pull only), the mirror over HTTPS with its `ca` (pull and resolve), and the real host as `server`. A node tries them in that order. A mirror whose registry API sits under a path prefix is written as the host with that prefix; a cache that wants its project name inside the API path needs `override_path = true`." | yes | none in this plan (step 5) |
| 5, command lines | Do the Helm command lines of section 7 exist and mean the same in Helm 3.22 and Helm 4.3? | The command lines worked in both majors and gave revisions 1, 2 and 3; none waits for the rollout (in one run a rollback needed no new pod and was complete when it returned); every failure exited 1. | The command lines stand; the chart's name and version are read from `helm show chart`, never from the pull's output. | "Under Helm 3.22.1 and under Helm 4.3.2 the same command lines work: `helm pull CHART --version V --destination DIR`; `helm upgrade --install RELEASE CHART --namespace NS --kubeconfig FILE --kube-context CTX --timeout D --values FILE`; `helm upgrade RELEASE CHART --reset-values …`; `helm rollback RELEASE REVISION …`; `helm status RELEASE -o json`, whose `version` is the revision; `helm show chart ARCHIVE`, which gives the chart's name and version. They give revisions 1, 2 and 3. Each returns before the rollout is complete. Any failure exits non-zero. A duration is accepted as Go prints it (2m0s)." | yes | Task 24, Task 36 |
| 8, nerdctl | Does kind v0.33.0 used as a library create the four-node cluster through nerdctl with the node settings of section 9? | `Create` failed in 5 of 9 runs, at kind's own 30-second wait for a node's log line; every setting held in each of the 4 runs where `Create` returned. | The settings of the record stand for Tasks 20 and 21; Task 21 retries `Create` (see `## What changes`). | "Through nerdctl, kind v0.33.0 as a library creates one control-plane node and three workers with: a zone label on each worker from the cluster description; the containerd patch that sets config_path to /etc/containerd/certs.d; a file of the runner mounted into every node; a kubeconfig written only to the path the tool gives. cluster.DetectNodeProvider picks nerdctl where Docker is absent. With a wait time of zero, Create waits for no node, so the tool waits for every node itself. A rootless engine is shown by a SecurityOptions entry of `ENGINE info` that contains rootless." | no | Tasks 20, 21 |
| 8, Docker | Does the same library call create the cluster through Docker? | not run; waits for the maintainer's word | Waits for a hosted runner (see `## Still unproved`). | none (no record) | not measured here | Tasks 21, 22, 36 on a hosted runner |
| 9 | Which release of kind does the tool link, and which node images does it list? | kind v0.33.0 is the newest release that is not an alpha on 2026-11-16 and lists four node images, the newest `kindest/node:v1.37.0`. | The tool links v0.33.0; the oldest listed image v1.34.11 is the suite's second image, and v1.36.4 and v1.35.8 are expected to work and not promised. | "The tool links sigs.k8s.io/kind v0.33.0, the newest release that is not an alpha on 2026-11-16; its default node image is kindest/node:v1.37.0." | yes | Global Constraints, Task 21 |
| 10 | Can kubectl's rollout-status rules be imported, at what cost, and what does each rule return? | Importing the package made the binary 3416064 bytes larger, over the 3 MB limit, and added 49 modules. | The tool copies the three rules with their licence header and a test that compares the copy with kubectl's package. | "The tool copies the three Status rules of k8s.io/kubectl/pkg/polymorphichelpers (rollout_status.go at the tag that matches client-go) into internal/watch/rollout.go, with their licence header and source, and a test in the tool's module compares the copy with kubectl's package on the same objects. Importing the package into the binary costs more than 3 MB." | yes | Task 25 |
| 11 | Does following a pod's log lose lines when the kubelet rotates it, and how is a stream opened again with none lost and none read twice? | A followed stream crossed rotations with no line lost or repeated and no reopening; reopens at one line per timestamp read every line once; a loss is seen as a gap. Lines that share one timestamp never occurred, so the counting for more than one was not measured. | Task 29 uses the reopening rule as run; its unit test pins the counting for lines that share a timestamp. | p11-log-rotation, 3 assumptions: (1, yes) "A log followed from its start through the API (follow and timestamps on) gives every line once, in order, across the kubelet's rotations." (2, not measured here) "A stream that ended while the container runs is opened again with sinceTime set to the second of the last line read. The lines of that second that were already read are skipped by counting them, so two lines with the same timestamp and the same text are neither lost nor read twice." (3, yes) "When the lines after the break are no longer returned (they sit in a rotated file), the follower can see it: the first line returned is later than the last line read plus what it skipped, and the count does not match. The tool then reports a gap." | not measured here | Task 29 |
| 12 | How many of the user's kernel keys does one cluster use without `NoNewKeyring = true`, and with it? | Without the option the peak was 106 above the baseline (110 and 108 in two other runs), with it 79; the peak of more than half the quota (100 of 200) occurred only while the nodes were being deleted, and through the install and the rollouts the count was 78 above the baseline. | The option stays: on a rootless engine the tool sets `NoNewKeyring = true`; the pause image's tag comes from this record. | "On a rootless engine the tool sets NoNewKeyring = true for the runc runtime in the nodes' containerd; one cluster of four nodes with the first fixture's shape, through an install and two rollouts, would otherwise use more than half of the user's key quota." | yes | Tasks 20, 27 |

## The rule

Part 3 starts when every row that names a task says `yes`. Two rows may say `not measured here` instead: point 12 on a machine that is not rootless, and point 8 with Docker, which waits for a hosted runner. Row 8 with nerdctl says `no`, and row 11 says `not measured here` for a reason that is not either of those two. Their consequences are in the next section.

## What changes

Point 8, nerdctl, holds `no`, and the rule's answer is written here instead of being left implicit. It does not stop Part 3, for these reasons. The design (section 9, "Engines other than Docker") makes Docker the supported engine and says that nerdctl "is expected to work and is not promised". Docker's half of point 8 waits for a hosted runner, which the rule allows. Every setting that Tasks 20 and 21 take from the record held in every run where kind's `Create` returned. What failed is kind's own 30-second wait for a node's log line inside `Create`, in 5 of 9 first attempts.

Tasks 21, 22, 27 and 29 create clusters through the tool's `cluster.Create` on a machine with nerdctl, so the text of **Task 21** changes. Its Create step 3 tries `provider.Create` again, at most 4 attempts in all. It makes a new attempt only when the error text contains `could not find a log line that matches` and the context has not ended. Before each new attempt it removes the failed attempt with `provider.Delete`, also when the cluster is to be kept, so that only the last attempt's nodes stay. After the last attempt the failure is step 3's, its text unchanged. Task 21 adds a unit test of the retry. No signature, condition or user-visible text changes.

Point 11 holds `not measured here` because its second assumption is not measured; the first and the third hold. No task's text changes for it. Task 29's unit test `TestFollowReopensWithoutLossOrRepeat` covers the counting of lines that share one timestamp. None occurred in the record's runs, even at about 1.3 million lines a second, because containerd stamps each line with its own clock reading in nanoseconds; the record's Decision says why.

Part 3 starts on these decisions. This section waits for the maintainer's review.

## What the proofs leave on the machine

The proofs removed every cluster and container they made. Volumes were not checked by a listing. The records give a reason for none: `p08-kind-library` says that kind gives each node an anonymous volume at `/var`, deletes the nodes with `nerdctl rm -f -v`, and that no volume was created during its runs; and the proofs' registries run with `--tmpfs /var/lib/registry` (`proofs/internal/lab/lab.go`), so the engine makes no anonymous volume for the image's `VOLUME`. After the last proof, this command printed no name that starts with `rollproof-proof-`:

    nerdctl ps -a --format "{{.Names}}"

Two things stay that the proofs did not remove: kind's network `kind`, and the node image `kindest/node:v1.37.0`.

## Still unproved

- Point 8 with Docker: `p08-kind-library` and `p01-registry-order` in `.github/workflows/rollproof-proofs.yml`, waiting for the maintainer's word.
- The engine's `save` format under Docker: Task 22's test with the `cluster` tag, on a hosted runner.
- The open points that section 13 gives to later steps: points 2 and 19 with step 5; points 3, 4 and 20 with step 6; the fixture runs of point 5 with steps 3 and 4; each fact of point 21 with the step that builds on it; points 7 and 16 with the fixture series of step 7.
- From `p05-helm-cli`, read and not run: a pull from a registry with TLS (without `--plain-http`); a pulled archive given as the chart to install and upgrade; a repository alias for the pull; and `--reset-values` together with `--values FILE` in one upgrade.
- A server-side apply conflict with another writer of a field, which section 14 point 5 gives to the fixtures.
- From `p01b-mirror`, the mirror form with no `server` line, which step 5 writes; the proof ran the form with a `server` line.
- From `p01-registry-order`, the limits of its measurements: images of one manifest each (no image index), one container per pod, no image held under two names, and no pull that fell through to a real HTTPS host.
