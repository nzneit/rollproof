# Point 11: a pod's log across rotation, and opening a stream again

## Question

Point 11 of section 14: the kubelet rotates a container's log at 10 MiB by default. Does following a pod's log through the API, with follow and timestamps on, lose lines when the kubelet rotates the log? How is a stream that ended opened again with no line lost and none read twice, when the API takes a start time in whole seconds? And what does the last piece of an exited container's output look like when it has no line end?

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64, 16 CPUs.
- Engine: nerdctl 2.4.2, rootless. Docker is absent. The engine is rootless, so the lab adds the `NoNewKeyring = true` patch.
- Go 1.27.3, for the program and for `liner`.
- kind: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go`, `k8s.io/api` and `k8s.io/apimachinery` v0.37.2; `github.com/google/go-containerregistry` v0.22.1, which pushes the image.
- Node image: kind's default, `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`. The program read the server's GitVersion as `v1.37.0`. The p01 record read `containerd v2.3.4` and `crictl version 1.36.0` in a worker of the same image; this program did not read them again.
- The worker's kubelet, read by the program through the node's `configz` (`/api/v1/nodes/WORKER/proxy/configz`): `containerLogMaxSize 10Mi, containerLogMaxFiles 5, containerLogMaxWorkers 1, containerLogMonitorInterval 10s`. These are the kubelet's defaults; the program changes no kubelet setting.
- Registry: `docker.io/library/registry:3`, as in the p01 record.
- Sources read, all at the tags named, with unauthenticated `curl -sS https://raw.githubusercontent.com/…`:
  - Kubernetes v1.37.0: `pkg/kubelet/apis/config/v1beta1/defaults.go` (lines 256 to 267, the four log defaults); `pkg/kubelet/logs/container_log_manager.go` (rotation); `staging/src/k8s.io/cri-client/pkg/logs/logs.go` (what the kubelet serves for a pod's log).
  - containerd v2.3.4: `internal/cri/io/logger.go` (how a line gets its timestamp and its tag); `internal/cri/server/container_log_reopen.go` (what the runtime does when the kubelet rotates).
  - The design: section 6, "Time" and "The log stream", and section 14 point 11.

## Method

The programs are `proofs/p11-log-rotation/main.go` and `proofs/p11-log-rotation/liner/main.go`, in the module `rollproof-proofs`.

**liner** stands in for a probe. It is a static program with the standard library only. It writes `-n` lines (default 10), `-rate` lines a second (0: as fast as it can), each `{"ok":true,"detail":{"i":N,"pad":"xxx…"}}` with N from 1 and `-pad` bytes of `x` (default 0). Each line and its `\n` go out with one `os.Stdout.Write`, with no buffering, and line N is written no earlier than (N-1)/rate seconds after the start. `-same` writes the identical line `{"ok":false,"class":"refused"}` instead. `-tail` adds, after the lines, the piece `{"ok":true,"detail":{"i":N}}` with N one past the last line and no `\n`. SIGTERM ends it at once with exit code 0, and so does the end of its lines.

**The program:**

1. It builds `liner` in a new temporary folder, `$TMP/…`, with `CGO_ENABLED=0` and `GOOS=linux` added to the environment:

   ```
   go build -trimpath -ldflags "-s -w" -o $TMP/…/liner ./p11-log-rotation/liner
   ```

2. `lab.Up(ctx, "p11", cfg)`: one control-plane node and one worker, with the `config_path` patch of the p01 record:

   ```toml
   [plugins."io.containerd.grpc.v1.cri".registry]
     config_path = "/etc/containerd/certs.d"
   ```

3. `l.Registry(ctx, "run")`; in every node `/etc/containerd/certs.d/rollproof.test/hosts.toml`, the form that the p01 record decided (its case e), written with `mkdir -p` and `cp /dev/stdin` through `l.Exec`; `lab.PushBinary` of `liner` to the registry as `probe/liner`; then `crictl pull rollproof.test/probe/liner@DIGEST` in the worker. In run 1 the file was:

   ```toml
   [host."http://10.4.1.223:5000"]
     capabilities = ["pull"]
   ```

4. The namespace `p11`, and a wait of at most 30 s for its default service account. For each experiment, the program deletes a pod `liner` that is left (grace period 0) and waits until it is gone. It then creates the pod `liner`: `restartPolicy: Never`, `nodeName` the worker, one container `probe` of `rollproof.test/probe/liner@DIGEST` with `imagePullPolicy: IfNotPresent` and the experiment's arguments as `args`. It waits, at most 2 minutes, until the container runs or has ended.

5. **The reader.** `CoreV1().Pods("p11").GetLogs("liner", &corev1.PodLogOptions{Container: "probe", Follow: true, Timestamps: true, SinceTime: SINCE}).Stream(ctx)`, with SINCE nil when the stream first opens. The program reads with `bufio.Reader.ReadString('\n')`. It splits each line at its first space into the timestamp, parsed with `time.RFC3339Nano`, and the text, and takes N from `detail.i`. A stream is cut by cancelling its own context; a line that arrives after the cut is not taken.

6. **The reopening rule.** LAST is the timestamp of the last line taken, and AT is how many lines taken carry exactly LAST. A stream is opened again with SINCE `metav1.NewTime(LAST.Truncate(time.Second))`. A line whose timestamp is before LAST is skipped. Of the lines at LAST, the first AT are skipped. Every line after them is taken, and LAST and AT are updated from it. For each reopen the program notes the first line returned and how many lines at LAST came back. When that first line is later than LAST, or fewer than AT lines at LAST came back, the rule has seen a gap. When a stream ends without a cut, the program polls the pod for 5 s: if the container has ended, the follow is over. If it still runs, B, C, C2 and D open the stream again by the rule, and A and E count it against their result.

7. **The experiments:**
   - A, rotation: `-n 150000 -rate 15000 -pad 180`. One stream from the start to its end. Check: N runs from 1 to n with no gap and no repeat (the program checks that each line's N is one more than the last), no timestamp is before the one before it, and the one stream ended with `io.EOF` after the container had ended. Then `ls -la /var/log/pods/p11_liner_UID/probe/` in the worker. If it lists no rotated file (a name that starts with `0.log.`), `-n` is doubled and A runs again, at most twice. A counts as failed if no run rotated.
   - B, reopen: `-n 60000 -rate 2000` (30 s). At 5, 12 and 19 s after the stream first opened, the stream is cut, and after 2 s it is opened again by the rule. Check: three cuts, and every N from 1 to 60,000 read exactly once.
   - C, reopen after a rotation: the arguments with which A rotated. The `ls` of A runs every 500 ms while the first stream is open. When a rotated file appears, the stream is cut, and 12 s later it is opened again by the rule and followed to the end. C is recorded, not judged. It counts as failed only if it cannot run as described, for example when no rotated file appears while the first stream is open.
   - C2, reopen after a second rotation: C's procedure, with A's arguments and `-n 300000` (20 s). The liner then still writes when the kubelet looks again after the cut, so the lines after the break land in a rotated file during the 12-second wait. If C2 loses no line, it runs once more with `-n 600000`, and no further. C2 was added after C, with A's arguments, lost no line in runs 1 and 3. It is recorded, not judged: the program prints the lines lost (a count, and the first and last missing N), the first line returned after the reopen, the lines at LAST that came back against AT, whether the rule saw a gap, and the listings at the cut and at the end. Like C, it counts as failed if it cannot run to its end.
   - D, the same text and the same stamp: `-same -n 50000 -rate 0`. The stream is cut after 10,000 and after 30,000 lines read, and opened again by the rule each time. Check: two cuts, and exactly 50,000 lines read.
   - E, the tail: `-n 10 -tail`. One stream to its end. The program prints the last piece as received, whether it ends with `\n`, its timestamp, the error that ended the stream, and the container's terminated state from the pod's status.

8. `Down`, also when a step fails.

The last line is `p11: A ok, B ok, C TEXT, D ok, E recorded`, with `FAILED` in place of a word for a failed check or an experiment that did not run to its end. TEXT is `no line lost`, `N lines lost, gap seen` or `N lines lost, gap not seen`. The last line does not name C2; its results are `info:` lines only. A C2 that did not run to its end, an interrupt and a failed `Down` are failures too: each prints a `FAILED:` line, and when every word of the last line reads as held, the line gets `; N other checks failed` after it. The exit code is 0 only when nothing failed.

The command, from `projects/rollproof/proofs/`:

```
go run ./p11-log-rotation
```

The program ran four times on 2026-11-17, between 01:33 and 01:52 UTC, one cluster at a time. Runs 1 to 3 used the program before C2 was added; it printed every other line as the program does now. Runs 1 and 3 are the plain command. Run 2 tested an interrupt: that program was built as a binary in `$TMP/…` and run from a script, which sent SIGINT to its process alone 3 s after B's pod started. Run 4 is the plain command on the program as committed, with C2. `nerdctl ps -a` listed no container whose name begins with `rollproof-` before or after any run.

## Result

| Run | Program | Create attempts of `Up` | Last line | Exit | `nerdctl volume ls -q` before, after |
| --- | --- | --- | --- | --- | --- |
| 1 | before C2; the plain command | 1 | `p11: A ok, B ok, C no line lost, D ok, E recorded` | 0 | 57, 57 |
| 2 | before C2; SIGINT to the process during B | 1 | `p11: A ok, B FAILED, C FAILED, D FAILED, E FAILED` | 1 | 57, 57 |
| 3 | before C2; the plain command | 1 | `p11: A ok, B ok, C no line lost, D ok, E recorded` | 0 | 57, 57 |
| 4 | as committed, with C2; the plain command | 1 | `p11: A ok, B ok, C no line lost, D ok, E recorded` | 0 | 57, 57 |

No run printed the lab's line `lab: Create attempt K of 4 failed …`, so each `Up` took one attempt. Runs 1, 3 and 4 print the same lines for A to E, apart from times, addresses, uids, sizes and the counts of skipped lines. The quotes below are from run 1 unless they say otherwise; those of C2 are from run 4.

**The timestamp before a line.** As received, a line is the timestamp, one space, and the line the container wrote:

```
info: E: the first line as received: "2026-11-17T01:35:25.862032785Z {\"ok\":true,\"detail\":{\"i\":1,\"pad\":\"\"}}\n"
```

The timestamp is RFC 3339 in UTC with `Z` and always nine digits after the second, leading zeros kept, as in `2026-11-17T01:34:41.000764736Z`. That is the kubelet's `RFC3339NanoFixed` (logs.go, lines 52, 57 and 244 to 246). `time.RFC3339Nano` parses it. The runtime stamps each line with its own `time.Now()` when it writes the line to the node's file (containerd's logger.go, line 134). The values of LAST in the `reopen` lines below are formatted by the program with Go's `RFC3339Nano`, which drops trailing zeros: `01:34:41.51859991Z` was received as `01:34:41.518599910Z`. Every other timestamp quoted here is as received.

**A. Following across a rotation.** The first run of A, at `-n 150000`, rotated in all four runs, so `-n` was never doubled. Every line came once and in order, and the one stream ended with `io.EOF` only after the container had ended:

```
info: A, -n 150000: first timestamp 2026-11-17T01:34:24.938986933Z, last timestamp 2026-11-17T01:34:34.939144088Z, as received
info: A, -n 150000: 150000 lines taken; missing N: 0 (none); repeats: 0; N outside 1 to 150000: 0; lines whose N is not one more than the N before: 0; timestamps before the one before: 0
info: A, -n 150000: ls -la /var/log/pods/p11_liner_55409c4b-a768-4b4a-9602-65759ee42720/probe/ in the worker:
info: A, -n 150000:   total 38404
info: A, -n 150000:   drwxr-xr-x 1 root root       52 Nov 17 01:34 .
info: A, -n 150000:   drwxr-xr-x 1 root root       10 Nov 17 01:34 ..
info: A, -n 150000:   -rw-r----- 1 root root 14060493 Nov 17 01:34 0.log
info: A, -n 150000:   -rw-r----- 1 root root 25261801 Nov 17 01:34 0.log.20261117-013431
ok: A, -n 150000: the one stream ended after the container had ended; the stream ended with io.EOF (it ended with EOF); N runs from 1 to 150000 with no gap and no repeat; no timestamp is before the one before it
info: A: the -n that rotated: 150000
```

Each run of A rotated once: the rotated file is named for the second of the rotation, about 6 s after the first line in each run (01:34:31 in run 1, 01:37:22 in run 2, 01:38:36 in run 3, 01:50:14 in run 4). The kubelet looks every 10 s, so a 10-second container meets at most one look while its file is over 10 MiB. At that look the file held about 25 MB. The two files hold 39,322,294 bytes for 150,000 lines, 262 bytes a line in the node's format. The rotated file held 24,911,078 bytes in run 2, 25,269,557 in run 3 and 25,475,412 in run 4, and each of those runs passed A in the same way. With the first streams of C (runs 1, 3 and 4) and of C2 (run 4), below, eight streams in all followed the log across a rotation with no line lost.

The kubelet's source agrees. When a following reader is at the end of its file, it watches the file's folder. On a `Create` event for the file's own name, it opens the new file and reads on from there (logs.go, lines 352 to 383 and 461 to 491). Rotation renames `0.log` to `0.log.TIMESTAMP` and then asks the runtime to reopen the log, which creates a new `0.log` (container_log_manager.go, lines 416 to 435; container_log_reopen.go, lines 39 to 50). The comment at the top of logs.go (lines 43 to 48) says that a follower stays on the old file after such a rotation. The code no longer does that, and this proof saw no such behaviour.

**B. Opening again while the container runs.** Three cuts, three reopens, and every line once:

```
info: B: reopen 1: LAST 2026-11-17T01:34:41.51859991Z (N 10178), AT 1; first line returned: N 9141 at 2026-11-17T01:34:41.000764736Z; lines at LAST that came back: 1 of AT 1; lines skipped: 1038; the rule saw a gap: false
info: B: reopen 2: LAST 2026-11-17T01:34:48.519824099Z (N 24179), AT 1; first line returned: N 23140 at 2026-11-17T01:34:48.000622813Z; lines at LAST that came back: 1 of AT 1; lines skipped: 1040; the rule saw a gap: false
info: B: reopen 3: LAST 2026-11-17T01:34:55.5196675Z (N 38180), AT 1; first line returned: N 37140 at 2026-11-17T01:34:55.000387726Z; lines at LAST that came back: 1 of AT 1; lines skipped: 1041; the rule saw a gap: false
info: B: 60000 lines taken; missing N: 0 (none); repeats: 0; N outside 1 to 60000: 0; lines whose N is not one more than the N before: 0; timestamps before the one before: 0
ok: B: the stream was cut and opened again three times (cuts: 3); every N from 1 to 60,000 read exactly once
```

A reopened stream starts at the first line of the second that SINCE names, so it repeats every line of that second that was already read. The rule skipped them: 1,037 lines before LAST and the one at LAST, in reopen 1. In run 3 the cuts fell early in their seconds, and the rule skipped 24, 21 and 21 lines; in run 4, 89, 87 and 90. The kubelet returns a line unless its timestamp is before SINCE (logs.go, lines 239 to 242).

**C. Opening again after a rotation.** The rotated file appeared at the seventh poll, and the stream was cut then. No line was lost, and the rule saw no gap:

```
info: C: a rotated file appeared at poll 7, 2026-11-17T01:35:11.478857409Z
info: C, at the cut: ls -la /var/log/pods/p11_liner_809dd799-59c1-49ae-9a1a-23007c30dbcd/probe/ in the worker:
info: C, at the cut:   total 13920
info: C, at the cut:   drwxr-xr-x 1 root root       52 Nov 17 01:35 .
info: C, at the cut:   drwxr-xr-x 1 root root       10 Nov 17 01:35 ..
info: C, at the cut:   -rw-r----- 1 root root   405664 Nov 17 01:35 0.log
info: C, at the cut:   -rw-r----- 1 root root 13843196 Nov 17 01:35 0.log.20261117-013511
info: C, at the end: ls -la /var/log/pods/p11_liner_809dd799-59c1-49ae-9a1a-23007c30dbcd/probe/ in the worker:
info: C, at the end:   total 38404
info: C, at the end:   drwxr-xr-x 1 root root       52 Nov 17 01:35 .
info: C, at the end:   drwxr-xr-x 1 root root       10 Nov 17 01:35 ..
info: C, at the end:   -rw-r----- 1 root root 25478846 Nov 17 01:35 0.log
info: C, at the end:   -rw-r----- 1 root root 13843196 Nov 17 01:35 0.log.20261117-013511
info: C: reopen 1: LAST 2026-11-17T01:35:11.478037461Z (N 54466), AT 1; first line returned: N 52903 at 2026-11-17T01:35:11.374853292Z; lines at LAST that came back: 1 of AT 1; lines skipped: 1564; the rule saw a gap: false
info: C: 150000 lines taken; missing N: 0 (none); repeats: 0; N outside 1 to 150000: 0; lines whose N is not one more than the N before: 0; timestamps before the one before: 0
info: C: the lines lost: 0; the missing N: none
info: C: the last line read before the cut: N 54466 at 2026-11-17T01:35:11.478037461Z; AT 1
info: C: the first line returned after the reopen: N 52903 at 2026-11-17T01:35:11.374853292Z
info: C: the lines at LAST that came back: 1, against AT 1
info: C: the rule saw a gap: false
```

When the stream was cut, the follower had already read past the rotation. LAST, N 54,466, sat in the new `0.log`, which held 405,664 bytes, about 1,550 lines, at the cut. So the lines after the break were not in a rotated file. The reopened stream started at N 52,903, the first line of the new `0.log`, at 0.37 s into the second that SINCE named. The lines of that second before N 52,903, about 5,600 of them at 15,000 a second, sit in the rotated file, and none came back. **A stream opened again reads only the current file**, as logs.go says (line 45, and line 297 opens one path). Run 3 shows the same: LAST was N 54,310, and the reopen started at N 52,972, 0.89 s into its second.

C could not lose a line with A's arguments. The liner runs for 10 s, and the kubelet looks again only 10 s after the rotation, so no second rotation happened during the 12-second wait: `0.log` at the end is the file that was new at the cut. So in C the lines after the break never sat in a rotated file, and C did not test the third assumption. Run 4's C was the same: LAST was N 54,233, in the new file, and no line was lost. C2 tests it.

**C2. Opening again after a second rotation.** In run 4, C2 at `-n 300000` lost lines, and the rule saw the gap, so the run at `-n 600000` was not needed:

```
info: C2, -n 300000: a rotated file appeared at poll 14, 2026-11-17T01:51:15.086698333Z
info: C2, -n 300000, at the cut: ls -la /var/log/pods/p11_liner_147d2b20-d00e-499d-8329-6402debab7dd/probe/ in the worker:
info: C2, -n 300000, at the cut:   total 27304
info: C2, -n 300000, at the cut:   drwxr-xr-x 1 root root       52 Nov 17 01:51 .
info: C2, -n 300000, at the cut:   drwxr-xr-x 1 root root       10 Nov 17 01:51 ..
info: C2, -n 300000, at the cut:   -rw-r----- 1 root root   387510 Nov 17 01:51 0.log
info: C2, -n 300000, at the cut:   -rw-r----- 1 root root 27568766 Nov 17 01:51 0.log.20261117-015114
info: C2, -n 300000, at the end: ls -la /var/log/pods/p11_liner_147d2b20-d00e-499d-8329-6402debab7dd/probe/ in the worker:
info: C2, -n 300000, at the end:   total 50848
info: C2, -n 300000, at the end:   drwxr-xr-x 1 root root      100 Nov 17 01:51 .
info: C2, -n 300000, at the end:   drwxr-xr-x 1 root root       10 Nov 17 01:51 ..
info: C2, -n 300000, at the end:   -rw-r----- 1 root root 11542614 Nov 17 01:51 0.log
info: C2, -n 300000, at the end:   -rw-r----- 1 root root   875951 Nov 17 01:51 0.log.20261117-015114.gz
info: C2, -n 300000, at the end:   -rw-r----- 1 root root 39644191 Nov 17 01:51 0.log.20261117-015125
info: C2, -n 300000: reopen 1: LAST 2026-11-17T01:51:15.085978375Z (N 106787), AT 1; first line returned: N 256094 at 2026-11-17T01:51:25.040761475Z; lines at LAST that came back: 0 of AT 1; lines skipped: 0; the rule saw a gap: true
info: C2, -n 300000: 150694 lines taken; missing N: 149306 (106788-256093); repeats: 0; N outside 1 to 300000: 0; lines whose N is not one more than the N before: 1; timestamps before the one before: 0
info: C2, -n 300000: the lines lost: 149306; the first missing N 106788, the last missing N 256093
info: C2, -n 300000: the last line read before the cut: N 106787 at 2026-11-17T01:51:15.085978375Z; AT 1
info: C2, -n 300000: the first line returned after the reopen: N 256094 at 2026-11-17T01:51:25.040761475Z
info: C2, -n 300000: the lines at LAST that came back: 0, against AT 1
info: C2, -n 300000: the rule saw a gap: true
info: C2, -n 300000: 149306 lines lost, gap seen
```

At the cut the follower had read past the first rotation, as in C: LAST, N 106,787, sat in the new `0.log`, which held 387,510 bytes, about 1,480 lines. During the 12-second wait the liner still wrote, and the kubelet looked again and rotated at 01:51:25. The file that held LAST and every line after it became `0.log.20261117-015125`, and the first rotated file was compressed to `0.log.20261117-015114.gz`. The reopened stream returned only the newest `0.log`, from its first line, N 256,094. That file's 11,542,614 bytes fit the 43,907 lines from N 256,094 to 300,000, at about 263 bytes each. The first line returned was 9.95 s later than LAST, and none of the AT lines at LAST came back, so the rule saw the gap. The lines lost were exactly the lines after LAST in the second rotated file: 149,306 lines, N 106,788 to 256,093. They were still on the node's disk, but the API did not return them.

**D. The same text.** Two cuts, two reopens, exactly 50,000 lines:

```
info: D: reopen 1: LAST 2026-11-17T01:35:24.424608633Z (N 0), AT 1; first line returned: "{\"ok\":false,\"class\":\"refused\"}" at 2026-11-17T01:35:24.416411095Z; lines at LAST that came back: 1 of AT 1; lines skipped: 10000; the rule saw a gap: false
info: D: reopen 2: LAST 2026-11-17T01:35:24.439124813Z (N 0), AT 1; first line returned: "{\"ok\":false,\"class\":\"refused\"}" at 2026-11-17T01:35:24.416411095Z; lines at LAST that came back: 1 of AT 1; lines skipped: 30000; the rule saw a gap: false
info: D: 50000 lines read; 50000 distinct timestamps; lines whose text is not {"ok":false,"class":"refused"}: 0
info: D: the largest number of lines that shared one timestamp: 1
ok: D: the stream was cut after 10,000 and after 30,000 lines read and opened again each time (cuts: 2); exactly 50,000 lines read (read: 50000)
```

The whole output fell in one second, so each reopen returned the log from its first line. The rule skipped 9,999 lines before LAST and the one at LAST, and then 29,999 and one. The first 30,000 lines span 22.7 ms (22.4 ms in run 3, 22.2 ms in run 4), about 1.3 million lines a second. **Still, no two lines shared a timestamp, in runs 1, 3 or 4, so AT was 1 at every reopen.** The runtime stamps each line with its own `time.Now()` at nanosecond resolution. The counting of AT above 1 was never exercised in a cluster.

**E. The last piece.** The piece without a line end came back without one, with a timestamp of its own. `ReadString` returned it together with `io.EOF`:

```
info: E: the first line as received: "2026-11-17T01:35:25.862032785Z {\"ok\":true,\"detail\":{\"i\":1,\"pad\":\"\"}}\n"
info: E: 10 lines taken, each with its line end
info: E: the last piece as received: "2026-11-17T01:35:25.862223292Z {\"ok\":true,\"detail\":{\"i\":11}}"
info: E: it ends with \n: false
info: E: its timestamp: 2026-11-17T01:35:25.862223292Z
info: E: the error that ended the stream: EOF (io.EOF: true)
info: E: the container's terminated state: exit code 0, reason Completed
ok: l.Down() returned no error
p11: A ok, B ok, C no line lost, D ok, E recorded
```

containerd writes a piece that ends without a line end with the tag `P`, for partial (logger.go, lines 168 to 179 and 199 to 202). The kubelet takes the line end off a `P` entry before it serves it (logs.go, lines 158 to 162).

**No stream ended early.** In the four runs, every stream ended either at a cut or with `io.EOF` after the container had ended. The program never printed `stream N ended … while the container still ran`.

**The interrupt.** In run 2 the signal came while B followed its stream. The run counted as failed, B to E counted as not run, and `Down` deleted the cluster:

```
ok: A, -n 150000: the one stream ended after the container had ended; the stream ended with io.EOF (it ended with EOF); N runs from 1 to 150000 with no gap and no repeat; no timestamp is before the one before it
info: A: the -n that rotated: 150000
info: pod liner with -n 60000 -rate 2000: phase Running, running, 506ms after it was created; uid 45675ba7-bf34-4599-b638-6cc794835748
FAILED: B: context canceled
ok: l.Down() returned no error
FAILED: the run was interrupted
FAILED: experiment B did not run to its end
FAILED: experiment C did not run to its end
FAILED: experiment D did not run to its end
FAILED: experiment E did not run to its end
p11: A ok, B FAILED, C FAILED, D FAILED, E FAILED
```

**Volumes.** `nerdctl volume ls -q` counted 57 before and after each run. Each run started one registry, four in all, and none left a volume.

## Decision

**A followed stream needs no reopening at a rotation.** The kubelet's follower moves to the new file by itself. The tool opens a stream again only when it ends while the container runs, which never happened here.

**For Task 29, the reopening rule as it was run:**

1. Keep LAST, the runtime's timestamp of the last line taken, and AT, how many lines taken carry exactly LAST.
2. Open again with follow and timestamps on, and sinceTime set to LAST truncated to the second.
3. Skip a line whose timestamp is before LAST; of the lines at LAST, skip the first AT; take every line after them, and update LAST and AT from it.
4. A gap: the first line returned is later than LAST, or fewer than AT lines at LAST came back.

Read with `ReadString('\n')`, and keep a piece that comes back together with `io.EOF`. That piece is the last piece of a container that ended; it has its timestamp and no line end.

**What the tool can detect when lines are lost.** A reopened stream reads only the current file, so lines that sit in a rotated file never come back (C, C2). When LAST sits in a rotated file, the first line returned is the first line of the current file. That line is later than LAST, and none of the AT lines at LAST come back, so the rule reports a gap. C2 measured this: 149,306 lines were lost, the first line returned was 9.95 s later than LAST, 0 of AT 1 came back, and the rule saw the gap. A loss needs the kubelet to rotate between the end of a stream and its reopen, so the tool opens a stream again at once. The tool knows both timestamps, so it knows the stretch of time in which lines may be missing, from LAST to the first line returned; section 6 ("The log stream") makes a window with such a loss not verified. The tool cannot tell how many lines were lost. Nor can it tell a loss from the case where LAST was the last line of a rotated file: there it reports a gap that lost nothing, which errs toward not verified. That case was not met. The rule detects nothing inside a stream that never broke; A and the first streams of C and C2 show that such a stream lost nothing across a rotation. The proof counted lines by `detail.i`, which the probe contract does not have, so the tool sees only what the rule sees.

**What is not measured: AT above 1.** No two lines shared a timestamp, even at about 1.3 million lines a second. containerd stamps every line with a call of its own when it writes it, `timeBuffer = time.Now().AppendFormat(timeBuffer[:0], timestampFormat)` with `timestampFormat = time.RFC3339Nano` (logger.go at v2.3.4, lines 134 and 38), so consecutive lines get different nanosecond stamps. In each of the three full runs, five reopens at AT 1 (three in B, two in D) read every line once. The counting for AT above 1 is pinned by Task 29's unit test `TestFollowReopensWithoutLossOrRepeat`, not by this proof.

Plan assumed: "A log followed from its start through the API (follow and timestamps on) gives every line once, in order, across the kubelet's rotations."
Holds: yes
Plan assumed: "A stream that ended while the container runs is opened again with sinceTime set to the second of the last line read. The lines of that second that were already read are skipped by counting them, so two lines with the same timestamp and the same text are neither lost nor read twice."
Holds: not measured here
Plan assumed: "When the lines after the break are no longer returned (they sit in a rotated file), the follower can see it: the first line returned is later than the last line read plus what it skipped, and the count does not match. The tool then reports a gap."
Holds: yes
