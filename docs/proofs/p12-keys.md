# Point 12: kernel keys on a rootless engine

## Question

Point 12 of section 14: how many kernel keys of the user does one cluster use through a run without the setting of section 9, which sets `NoNewKeyring = true` for the runc runtime in the nodes' containerd so that a container takes no session key of its own? And how many with it? If one cluster stays well under the user's quota, the setting leaves the tool.

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64, 16 CPUs.
- Kernel key limits for a user other than root: `kernel.keys.maxkeys` 200 and `kernel.keys.maxbytes` 20000. `fs.inotify.max_user_instances` is 1024.
- Engine: nerdctl 2.4.2, with containerd v2.4.2, runc 1.5.2 and rootlesskit 3.3.0 (network slirp4netns). Docker is absent.
- Rootless: yes. Round 1's engine check found an entry of the `SecurityOptions` of `nerdctl info --format "{{json .}}"` that contains `rootless`; the program goes on only then. The p08 record lists the entries.
- Go 1.27.3.
- kind: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go` v0.37.2.
- Node image: kind's default, `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`: Kubernetes v1.37.0 with containerd v2.3.4 in the nodes, as the p08 record read them. This program reads neither.
- The pause image that the nodes held: `registry.k8s.io/pause:3.10`.

## Method

The program is `proofs/p12-keys/main.go`. A reading is the line of `/proc/key-users` whose first field is the user's UID and a colon. The line's form is `UID: USAGE NKEYS/NIKEYS QNKEYS/MAXKEYS QNBYTES/MAXBYTES`, and the reading is QNKEYS and MAXKEYS.

It makes two rounds, one cluster at a time. Each round calls `lab.Up(ctx, "p12", cfg)` with one control-plane node and three workers, no labels, no mounts and kind's default image, and calls `Down` itself. Round 1, "without the option", carries this patch, so the lab adds none:

```toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
  NoNewKeyring = false
```

Round 2, "with the option", carries no patch, and the lab adds `NoNewKeyring = true` because the engine is rootless.

Each round reads the keys at its baseline and after Up. Round 1 then checks that the engine is rootless. A sampler then reads the keys until the round ends and keeps the highest QNKEYS; if QNKEYS passes 180, it cancels the round and calls `Down` at once. The round then:

1. takes the tag of `registry.k8s.io/pause` from `crictl images` in the first worker;
2. creates the namespace `p12` and the Deployment `load`, with 6 replicas, the labels `app: load`, and one container `pause` of `registry.k8s.io/pause:3.10` with `imagePullPolicy: IfNotPresent`. The Deployment's strategy is the default rolling update. The round waits until `readyReplicas` is 6, then reads;
3. twice, patches `spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"]` to the time in RFC 3339 (a strategic merge patch), and waits until `observedGeneration` is at least `generation` and `replicas`, `updatedReplicas` and `availableReplicas` are all 6, then reads;
4. calls `Down`, stops the sampler, and reads.

A round's peak is the highest QNKEYS of its readings and its samples. The sampler starts after the reading after Up, so the peak does not cover Create. The load is the brief's six pause replicas, not the first fixture.

**The sampler's interval departs from the brief.** The brief asked for a reading every 500 ms, and runs 1 to 6 used that. Runs 4 to 6 found that the keys rise by about 50 for a few tens of milliseconds while each node shuts down, and a sampler every 500 ms mostly misses that. The program reads every 5 ms from run 7 on. Before run 9 it had two edits that change no line a run prints: the deferred `Down` no longer prints an error that the round has already reported, and a field that nothing read was taken out. Run 9 is the program as committed.

The command, from `projects/rollproof/proofs/`:

```
go run ./p12-keys
```

It ran nine times, one cluster at a time, from 2026-11-16 23:46 UTC to 2026-11-17 00:11 UTC. `nerdctl ps -a` listed no container before and after each run. In runs 4 to 6, the program ran beside an outside sampler that wrote each change in QNKEYS with a timestamp. In runs 4 and 5 the outside sampler was a shell loop that read every 200 ms and saved `/proc/keys` at each new high; in run 6 it was a small Go program in `$TMP/…`, not committed, that read every 5 ms. In those runs a shell loop added a timestamp to each line of the program's output.

## Result

Run 9 is the run of the program as committed. Readings are QNKEYS, of a quota of 200:

| Round | Baseline | After Up | 6 ready | Restart 1 | Restart 2 | After Down | Peak | Above baseline |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| without the option | 7 | 73 | 85 | 85 | 85 | 7 | 113 | 106 |
| with the option | 7 | 43 | 43 | 43 | 43 | 7 | 86 | 79 |

The nodes held `registry.k8s.io/pause:3.10` in every run.

The readings at the stages were the same in all nine runs: 7, 73, 85, 85, 85, 7 without the option, and 7, 43, 43, 43, 43, 7 with it. The peaks differ by run and by sampler. The `Up` columns give how many Create attempts each round's `Up` took, read from the `lab: Create attempt K of 4 failed at kind's wait for a node's log line; trying again` lines (one attempt when there is no such line):

| Run | Program's sampler | Peak without (above baseline) | Peak with (above baseline) | `Up` round 1 | `Up` round 2 | Outside sampler: peak without, with |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 500 ms | 89 (82) | 43 (36) | 1 | 1 | none |
| 2 | 500 ms | 89 (82) | 43 (36) | 1 | 1 | none |
| 3 | 500 ms | 87 (80) | 73 (66) | 1 | 1 | none |
| 4 | 500 ms | 89 (82) | 43 (36) | 1 | 1 | 200 ms: 105 (98), 57 (50) |
| 5 | 500 ms | 89 (82) | 83 (76) | 1 | 2 | 200 ms: 104 (97), 83 (76) |
| 6 | 500 ms | 88 (81) | 43 (36) | 1 | 1 | 5 ms: 123 (116), 85 (78) |
| 7 | 5 ms | 117 (110) | 99 (92) | 1 | 1 | none |
| 8 | 5 ms | 115 (108) | 101 (94) | 1 | 3 | none |
| 9 | 5 ms | 113 (106) | 86 (79) | 2 | 1 | none |

The outside sampler's peaks cover the whole round, Up included. No round stopped early, and every run exited 0. Each run's last line has the brief's form, with the two numbers above baseline of its row and `quota 200`. Run 9's output, on standard output and standard error together:

```
info: round 1, baseline: 7/200
lab: Create attempt 1 of 4 failed at kind's wait for a node's log line; trying again
lab: Create returned after 20.36s; 0 of 4 nodes Ready
lab: every node Ready after 32.358s
info: round 1, after Up: 73/200
info: pause image registry.k8s.io/pause:3.10
info: round 1, 6 ready: 85/200
info: round 1, after restart 1: 85/200
info: round 1, after restart 2: 85/200
info: round 1, after Down: 7/200
info: round 1 peak 113, above baseline 106
info: round 1 stopped early: no
info: round 2, baseline: 7/200
lab: Create returned after 21.853s; 0 of 4 nodes Ready
lab: every node Ready after 33.85s
info: round 2, after Up: 43/200
info: pause image registry.k8s.io/pause:3.10
info: round 2, 6 ready: 43/200
info: round 2, after restart 1: 43/200
info: round 2, after restart 2: 43/200
info: round 2, after Down: 7/200
info: round 2 peak 86, above baseline 79
info: round 2 stopped early: no
p12: peak without the option 106, with the option 79, quota 200
```

Run 8's round 2 printed two retry lines before its Create returned:

```
lab: Create attempt 1 of 4 failed at kind's wait for a node's log line; trying again
lab: Create attempt 2 of 4 failed at kind's wait for a node's log line; trying again
lab: Create returned after 20.981s; 0 of 4 nodes Ready
lab: every node Ready after 32.979s
```

Where the peak comes from. In the steady state, the six pause pods add 12 keys without the option (from 73 to 85) and none with it. The option leaves 36 keys of the cluster above the baseline; this proof did not look at where they come from. The peaks come while the nodes are deleted. In run 6, `Down` began at 23:58:47.6, after the reading after restart 2. The 5 ms sampler then saw four bursts in one second, one for each node, each lasting tens of milliseconds:

```
23:58:47.789 77
23:58:47.834 91
23:58:47.839 108
23:58:47.844 121
23:58:47.865 123
23:58:47.869 117
23:58:47.874 94
23:58:47.889 76
```

The later bursts reached 105, 91 and 104 from lower levels, and the count was back at 7 before the reading after Down. With the option, the bursts of run 6 reached 83, 69, 67 and 85, from 43 for the first and from lower levels for the others. During `Up`, the 5 ms sampler saw at most 73 in round 1 and 55 in round 2.

At the high of runs 4 (105) and 5 (104), `/proc/keys` held these keys of the user, counted by type and name. A 64-digit container ID and the UID are shortened:

| Count, run 4 | Count, run 5 | Type | Name |
| --- | --- | --- | --- |
| 46 | 46 | keyring | `_ses` |
| 45 | 44 | user | `invocation_id` |
| 9 | 9 | keyring | `_ses.<container ID>` |
| 2 | 2 | keyring | `_uid.0`, `_uid.UID` |
| 2 | 2 | keyring | `_uid_ses.0`, `_uid_ses.UID` |
| 1 | 1 | keyring | `.user_reg` |

The `_ses` keyrings and `invocation_id` keys have the names of the pair that systemd makes for each unit it starts. The `_ses.<container ID>` keyrings have the name runc gives a container's own session keyring, which is what `NoNewKeyring = true` leaves out. At the high, most of the keys are of the first kind, and the option does not change those.

## Decision

The rule: when the peak above baseline without the option is at most 100 (half the default quota of 200), the option leaves the tool; otherwise it stays.

With the 5 ms sampler, the peak above baseline without the option was 106 in run 9, the program as committed, and 110 and 108 in runs 7 and 8. The 5 ms outside sampler saw 116 in run 6. The peak is more than 100, so **the option stays**: on a rootless engine the tool keeps setting `NoNewKeyring = true`, and Task 20 keeps the effect of its `rootless` argument.

The brief's 500 ms sampler gave 80 to 82, which the rule would have read as "the option leaves". Those readings missed the bursts while the nodes are deleted. They are kept in the table above.

With the option, one cluster still peaked at 79 to 94 above baseline (86 to 101 of 200) while its nodes were deleted. This proof made one cluster at a time and measured nothing about two.

Plan assumed: "On a rootless engine the tool sets NoNewKeyring = true for the runc runtime in the nodes' containerd; one cluster of four nodes with the first fixture's shape, through an install and two rollouts, would otherwise use more than half of the user's key quota."
Holds: yes
