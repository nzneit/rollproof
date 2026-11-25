# Points 3, 4 and 20: what GitHub's runner does with `env`, outputs, the job summary and a cancel

## Question

Points 3, 4 and 20 of section 14. Point 3: does `env:` on the step that calls a composite action reach the action's inner run steps, and do the job's and the workflow's `env:` reach them? Point 4: are the outputs that a step wrote kept when the step then exits non-zero or is cancelled, does a composite action pass them on, and which of two values written for one output is kept? Point 20: which signals does GitHub's runner send a step when the run is cancelled or a time limit passes, to which process and when, and does a shell pass them on to its child? And the facts about the job summary that section 10 ("On a signal", "GitHub job summary") and the action's own test rest on: does every step get a new summary file, is a summary written at a cancel processed, can a later step of the job read it, what does masking do to it, what happens above 1 MiB, and how does it render?

## Machine and versions

- GitHub's hosted runner, image `ubuntu-24.04` version 20261116.376.1, Ubuntu 24.04.5 LTS, x64. Each job's first lines give the runner, the system and the image; these are from `files-then-job-timeout`, and the other two jobs print the same values:

  ```
  Current runner version: '2.338.2'
  Ubuntu
  24.04.5
  LTS
  Image: ubuntu-24.04
  Version: 20261116.376.1
  ```

  Each job ran on a machine of its own: the provisioner's `Region:` line says `eastus2` for `files-then-job-timeout`, `centralus` for `step-timeout` and `eastus` for `user-cancel`.
- The runner, 2.338.2. Every recorder whose parent was the runner names it as `/home/runner/actions-runner/cached/2.338.2/bin/Runner.Worker`. The runner's source was read at 2.338.0, at commit `7e211dc69c347b78fc398e630e74facb11a13235` of `actions/runner` (2026-11-19); the run used 2.338.2. A source place below is from that reading and says "read".
- The shell of every run step: `shell: /usr/bin/bash --noprofile --norc -e -o pipefail {0}`, as each step's log gives it. Python: `python3` from the PATH. The logs show neither version. The image's software list, `images/ubuntu/Ubuntu2404-Readme.md` of `actions/runner-images` at commit `0e63d4f9d2c29043b4cf1d2f40d1812a7cde1369`, which describes image version 20261116.376.1 (its line 10), gives Bash 5.2.21 (line 16), Python 3.12.3 (line 27), GitHub CLI 2.104.0 (line 111) and the kernel 7.0.0-1017-azure (line 9). These are read, not shown by the run.
- No container engine, no kind and no cluster: the three jobs need no checkout.

## Method

The name of the repository in which the jobs ran is written `REPOSITORY` in the lines quoted below.

The workflow file is `.github/workflows/rollproof-proofs.yml` at commit `cd7bc1f6eb0635c75832de73c47464ed9c026b32` of the branch `rollproof-step6-design`. It starts only by hand, and its input `part` picks the jobs: `part=all` runs `files-then-job-timeout` and `step-timeout`, with three jobs that other records cover; `part=runner-user-cancel` runs `user-cancel` alone, because that job cancels its own run. The workflow sets `env:` at its own level, `WF_LEVEL: workflow` and `OVERRIDE_ME: workflow`.

**The first step of each job** writes three files into the workspace:

- `.proof/act/action.yml`, a throwaway composite action. It has two inputs, `mode` (default `env`) and `label` (default `composite`), and two outputs: `verdict`, mapped to `steps.inner.outputs.verdict`, and `ref-in-env`, mapped to `steps.dump.outputs.ref_in_env`. It has two inner run steps, both with `shell: bash`:
  - `dump`, which runs when `mode` is `env`. Its own `env:` is `OVERRIDE_ME: inner-wins`, `EXPR_PREVIOUS_VERSION: ${{ env.PREVIOUS_VERSION }}`, `EXPR_JOB_LEVEL: ${{ env.JOB_LEVEL }}` and `REF_IN_ENV: ${{ github.action_ref }}`. It runs `.proof/dump.sh` into `q1-composite.txt` in the workspace, writes `ref_in_env=$REF_IN_ENV` to `$GITHUB_OUTPUT`, and writes the line `S6-INNER-ENV summary marker` to `$GITHUB_STEP_SUMMARY`.
  - `inner`, which runs when `mode` is not `env`, with `MODE` and `LABEL` taken from the inputs in its `env:`. With `mode: fail` it writes `verdict=fail` to `$GITHUB_OUTPUT` and `S6-COMPOSITE-FAIL summary marker` to the summary, and exits 1. With any other mode it runs `exec python3 "$GITHUB_WORKSPACE/.proof/rec.py" "$LABEL" 150`: the recorder replaces the shell.

  The step writes the action's expression opener as `@{{` and turns it into `${{` with `sed`, so that the workflow does not evaluate the action's expressions itself.
- `.proof/dump.sh`, which prints `NAME=VALUE`, or `NAME=<unset>`, for `PREVIOUS_VERSION`, `JOB_LEVEL`, `WF_LEVEL`, `OVERRIDE_ME`, `EXPR_PREVIOUS_VERSION`, `EXPR_JOB_LEVEL`, `GITHUB_ACTIONS`, `CI`, `GITHUB_OUTPUT`, `GITHUB_STEP_SUMMARY`, `RUNNER_TEMP`, `RUNNER_OS`, `RUNNER_ARCH`, `GITHUB_ACTION_PATH`, `GITHUB_ACTION_REF`, `INPUT_MODE` and `GITHUB_WORKSPACE`.
- `.proof/rec.py`, the recorder, a Python program of about 40 lines with two arguments, a label and a deadline in seconds. It appends lines of the form `EPOCH +SINCE TEXT` to `sig-LABEL.log` in the workspace, where EPOCH is the wall clock in seconds and SINCE the time since it started, by the monotonic clock. Its first three lines give its PID, its parent's PID, its process group and its session, its own command line, and its parent's command line (from `/proc/PID/cmdline`). It catches SIGINT, SIGTERM and SIGHUP: for each it writes `signal NAME` and goes on running. At the first signal only, it appends `verdict=fail` to `$GITHUB_OUTPUT` and `S6-SIGNAL-SUMMARY LABEL` to `$GITHUB_STEP_SUMMARY`, and then writes `wrote verdict and summary marker`. Every 0.5 s, until its deadline, it writes `alive ppid=N`. It does not exit on SIGINT or SIGTERM, so its last `alive` line dates the moment that something it cannot catch ended it, and a change of N dates the end of its parent.

**`files-then-job-timeout`** has `timeout-minutes: 1` and, at the job's level, `env: JOB_LEVEL: job`. After the first step, in order:

1. "env into a composite action" (`id: q1`): `uses: ./.proof/act` with `mode: env`. The step's `env:` is `PREVIOUS_VERSION: 1.4.2`, `OVERRIDE_ME: caller`, `CI: from-caller`, `GITHUB_ACTIONS: "false"`, `RUNNER_TEMP: /tmp/hijacked-temp` and `GITHUB_STEP_SUMMARY: /tmp/hijacked-summary`.
2. "a plain step fails after writing outputs" (`plain`): writes `verdict=fail`, then the output `multi` in the delimiter form (`multi<<S6EOF`, `line one`, `line two`, `S6EOF`), then `S6-PLAIN-FAIL summary marker` to the summary, and exits 1.
3. "a composite action's inner step fails after writing outputs" (`comp`): the action with `mode: fail`.
4. "an exec'd program writes the output" (`execd`): `exec python3 -c '…'`, a program that appends `verdict=fail-exec` to `$GITHUB_OUTPUT` and exits 1.
5. "an unparsable output line" (`bad`): `printf 'good=1\nthis line has no separator\nafter=2\n' >> "$GITHUB_OUTPUT"`. The script exits 0.
6. "masking and Markdown" (`mask`): `echo "::add-mask::S6SECRETVALUE"`; then it appends to its summary a heading, a table, fenced code, a `<details>` block, `<sub>` and `<kbd>`, and the line `masked value: S6SECRETVALUE`; and it writes its own summary path to the output `path`.
7. "a summary above 1 MiB" (`big`): `head -c 1048577 /dev/zero | tr '\0' 'a' >> "$GITHUB_STEP_SUMMARY"`.
8. "a new empty summary file" (`empty`): writes `empty=yes` when its summary file is empty as it starts, `empty=no` when not, and its summary path to `path`.
9. "a plain exec'd step under the job's timeout" (`jt`): `exec python3 "$GITHUB_WORKSPACE/.proof/rec.py" jt-plain-exec 150`, so that the recorder is the step's process. The job's limit of 1 minute ends it.
10. "cancelled() runs after the job's timeout", with `if: cancelled()`: prints the time.
11. "read back earlier summaries" (`readback`), with `if: always()`: lists `$RUNNER_TEMP/_runner_file_commands`, counts the raw files (`step_summary_*` without the suffix `-scrubbed`) and the `-scrubbed` copies that hold each marker, and counts the lines of the `mask` step's raw file and of its copy that hold `S6SECRETVALUE` and `***`.
12. "assert and print", with `if: always()`: 34 checks, each printed as a row `| PASS or FAIL | topic | what | expected | actual |` and appended to this step's summary; then `q1-composite.txt`; then the recorder's log without its `alive` lines, with its first and last `alive` lines. It exits with the number of failed checks.

Steps 3 to 9 have `if: ${{ !cancelled() }}`. A running step whose condition tests true again when the job is cancelled is not cancelled (read: `src/Runner.Worker/StepsRunner.cs`, lines 145 to 186), so a step that must be cancelled cannot have `always()`.

**`step-timeout`** has `timeout-minutes: 8`. After the first step:

1. "a child without exec, under a step timeout" (`stchild`), with `timeout-minutes: 1`: `python3 "$GITHUB_WORKSPACE/.proof/rec.py" st-child 150`, with no `exec`. Bash is the step's process and the recorder is its child.
2. "a composite action's inner exec, under a step timeout on the uses step" (`stcomp`), with `if: ${{ !cancelled() }}` and `timeout-minutes: 1` on the `uses:` step: the action with `mode: record` and `label: st-composite`. Its inner step `exec`s the recorder.
3. "print", with `if: always()`: the outcomes of both steps and the `verdict` of `stcomp`; each recorder's log without its `alive` lines, its first and last `alive` lines, and the number of `alive` lines for each parent PID in turn (`uniq -c`); the listing of `$RUNNER_TEMP/_runner_file_commands`; and the files there that hold `S6-SIGNAL-SUMMARY st-composite`.

**`user-cancel`** has `timeout-minutes: 12` and `permissions: actions: write`. After the first step:

1. "arm the canceller": starts in the background, with `nohup` and the job's token, `sleep 25; echo "cancel sent at $(date +%s.%N)"; gh run cancel "$GITHUB_RUN_ID" --repo "$GITHUB_REPOSITORY"`, with its output in `canceller.log`, and prints `armed at EPOCH`. This is a user's cancel through GitHub's API.
2. "a composite action's inner exec, under a user's cancel" (`uc`): the action with `mode: record` and `label: uc-composite`. Its condition is the default, so the cancel reaches it.
3. "cancelled() step", with `if: cancelled()`: prints the time.
4. "always() step, then the cancel window", with `if: always()`: prints the time, the outcome and `verdict` of `uc`, `canceller.log`, the recorder's log without its `alive` lines and its last `alive` line, the listing, and the files that hold `S6-SIGNAL-SUMMARY uc-composite`. Then it prints `still running at EPOCH` and sleeps 15 s, until the input `window-seconds` (default 20) has passed.

**`last-value`** was added to the workflow file later the same day, at commit `a5051f0`, after the three jobs above had run, because the design had by then come to rest on one more fact: the tool is to write `verdict` when it starts and again when it ends (section 8 of the design). It has `timeout-minutes: 3`. Its first step writes a second throwaway composite action, `.proof/twice/action.yml`, with one input, `end` (default `pass`), and two outputs, `verdict` and `out`, each mapped to the inner step's output of that name. The action's one inner step writes three outputs in the delimiter form (`NAME<<S6DELIM`, the value, `S6DELIM`): `out` as `/tmp/results`, `verdict` as `not-verified`, then `verdict` again as the value of `end`; with `end: fail` it then exits 1. After the first step:

1. "the plain form, twice" (`plain`): `echo "verdict=not-verified" >> "$GITHUB_OUTPUT"`, then `echo "verdict=pass" >> "$GITHUB_OUTPUT"`.
2. "the delimiter form, twice" (`delim`): the same two values of `verdict`, each in the delimiter form.
3. "one form, then the other" (`mixed`): the output `a` as `not-verified` in the delimiter form and then as `pass` in the plain form; the output `b` as `not-verified` in the plain form and then as `pass` in the delimiter form.
4. "through a composite action, ending well" (`comp_pass`): the action with its default.
5. "through a composite action, the second value fail and exit 1" (`comp_fail`): the action with `end: fail`, on a step with `continue-on-error: true`.
6. "assert and print", with `if: always()`: 10 checks, printed as rows `| PASS or FAIL | what | expected | actual |`. It exits with the number of failed checks.

The commands, each one command with no shell feature, so the same in bash and fish:

```
gh workflow run rollproof-proofs.yml --ref rollproof-step6-design -f part=all
gh workflow run rollproof-proofs.yml --ref rollproof-step6-design -f part=runner-user-cancel
gh workflow run rollproof-proofs.yml --ref rollproof-step6-design -f part=runner-last-value
```

Each ran once on 2026-11-22. Run 45014226495 (`part=all`) held the jobs `files-then-job-timeout` (job 137182961269, from 10:48:44 to 10:49:53 UTC) and `step-timeout` (job 137182961754, from 10:48:44 to 10:51:06). Run 45014479499 (`part=runner-user-cancel`) held the job `user-cancel` (job 137183735031, from 10:51:56 to 10:53:02), with `window-seconds` at its default. Run 45016867677 (`part=runner-last-value`), at commit `a5051f0`, held the job `last-value` (job 137190945198, at 11:20:58 and 11:20:59), on the same image and runner (`Current runner version: '2.338.2'`, `Version: 20261116.376.1`); it ended in success. Another dispatch of the day, `gh workflow run rollproof-proofs.yml --ref rollproof-step6-design -f part=podman-node-setting`, ran a job that this record does not cover. The workflow had never run before. `main` held an older file of that name then, with one job and no input; GitHub accepted the dispatch of the branch's file, and the branch's `part` input applied. Whether GitHub accepts a dispatch for a workflow whose file is on no branch but the chosen one was not tried. As designed, `files-then-job-timeout` ended cancelled by its own limit of 1 minute, `step-timeout` failed because both its steps timed out, and `user-cancel` ended cancelled by its own `gh run cancel`. The jobs' logs were saved; every line below is quoted from them, with the log's time stamp where a time is used.

**Times.** The recorder's EPOCH and the log's time stamps come from the same machine's clock. In each job, the first step printed `setup done at EPOCH` in a log line stamped 2 to 5 ms later. In the order `files-then-job-timeout`, `step-timeout`, `user-cancel`:

```
2026-11-22T10:48:44.7732086Z setup done at 1795344524.770374899
2026-11-22T10:48:44.3660628Z setup done at 1795344524.363627395
2026-11-22T10:51:56.6362925Z setup done at 1795344716.631534428
```

Where this record puts an EPOCH on the log's time of day, it counts from its job's line here.

## Result

All 34 checks of `files-then-job-timeout` passed, and the last line of its last step was `failed checks: 0`. Below, the checks are shown by topic, each with the other lines that show the result.

**`env`.** The step's, the job's and the workflow's `env:` reached the inner run step of the composite action, and the inner step's own `env:` won. `${{ env.NAME }}` in the action's file saw the step's and the job's values:

```
| PASS | env | step env reaches inner run step | `1.4.2` | `1.4.2` |
| PASS | env | job env reaches inner run step | `job` | `job` |
| PASS | env | workflow env reaches inner run step | `workflow` | `workflow` |
| PASS | env | inner env wins | `inner-wins` | `inner-wins` |
| PASS | env | env expression sees step env | `1.4.2` | `1.4.2` |
| PASS | env | env expression sees job env | `job` | `job` |
```

What the inner step's process printed, the first six lines of `q1-composite.txt`:

```
PREVIOUS_VERSION=1.4.2
JOB_LEVEL=job
WF_LEVEL=workflow
OVERRIDE_ME=inner-wins
EXPR_PREVIOUS_VERSION=1.4.2
EXPR_JOB_LEVEL=job
```

**The default variables.** A caller's `GITHUB_ACTIONS`, `RUNNER_TEMP` and `GITHUB_STEP_SUMMARY` did not reach the inner step; a caller's `CI` did. A composite action's inner step had no `INPUT_` variable:

```
| PASS | variables | GITHUB_ACTIONS not overridable | `true` | `true` |
| PASS | variables | CI overridable | `from-caller` | `from-caller` |
| PASS | variables | RUNNER_TEMP not overridable | `/home/runner/work/_temp` | `/home/runner/work/_temp` |
| PASS | variables | GITHUB_STEP_SUMMARY not overridable | `runner-path` | `runner-path` |
| PASS | variables | no INPUT_ variables in composite | `<unset>` | `<unset>` |
```

The rest of `q1-composite.txt`:

```
GITHUB_ACTIONS=true
CI=from-caller
GITHUB_OUTPUT=/home/runner/work/_temp/_runner_file_commands/set_output_7749b0e0-f113-4260-ac57-2a3af7b420ea
GITHUB_STEP_SUMMARY=/home/runner/work/_temp/_runner_file_commands/step_summary_7749b0e0-f113-4260-ac57-2a3af7b420ea
RUNNER_TEMP=/home/runner/work/_temp
RUNNER_OS=Linux
RUNNER_ARCH=X64
GITHUB_ACTION_PATH=/home/runner/work/REPOSITORY/REPOSITORY/./.proof/act
GITHUB_ACTION_REF=
INPUT_MODE=<unset>
GITHUB_WORKSPACE=/home/runner/work/REPOSITORY/REPOSITORY
```

The log's display of the inner step's `env:` shows the caller's values for those names, not what the process got:

```
##[group]Run bash "$GITHUB_WORKSPACE/.proof/dump.sh" > "$GITHUB_WORKSPACE/q1-composite.txt"
…
env:
  WF_LEVEL: workflow
  OVERRIDE_ME: inner-wins
  JOB_LEVEL: job
  PREVIOUS_VERSION: 1.4.2
  CI: from-caller
  GITHUB_ACTIONS: false
  RUNNER_TEMP: /tmp/hijacked-temp
  GITHUB_STEP_SUMMARY: /tmp/hijacked-summary
  EXPR_PREVIOUS_VERSION: 1.4.2
  EXPR_JOB_LEVEL: job
  REF_IN_ENV: 
```

For a `./` action, `github.action_ref` was empty both in the variable `GITHUB_ACTION_REF` and through the inner step's `env:` (`REF_IN_ENV` above, and the step's output):

```
printed, no assertion: ref seen through inner env for a ./ action: '' (null expected)
```

The source agrees, read: the runner sets `GITHUB_ACTIONS=true` after it has copied the step's environment (`src/Runner.Sdk/ProcessInvoker.cs`, line 282), and sets `CI=true` only when neither the step's nor the runner's own environment has `CI` (lines 286 to 289); it writes the `github` and `runner` contexts over the step's environment just before the start (`src/Runner.Worker/Handlers/ScriptHandler.cs`, lines 290 to 300); only the container and JavaScript handlers add `INPUT_` variables (`src/Runner.Worker/Handlers/Handler.cs`, line 175, called from `ContainerActionHandler.cs` line 39 and `NodeScriptActionHandler.cs` line 39); and the inner steps' `env:` is merged in three layers, the global `env`, the caller's step `env:`, and the inner step's own (`src/Runner.Worker/Handlers/CompositeActionHandler.cs`, lines 262, 268 and 290).

**Outputs.** An output was kept after its step exited 1, through a composite action as well, and when an `exec`ed program wrote it. A value in the delimiter form came back without the newline before the delimiter. An unparsable line failed a step whose script exited 0; the line before it was kept and the line after it lost:

```
| PASS | outputs | plain output kept after exit 1 | `fail` | `fail` |
| PASS | outputs | multi-line output (newline shown as \n) | `line one\nline two` | `line one\nline two` |
| PASS | outputs | plain step outcome | `failure` | `failure` |
| PASS | outputs | composite output after inner exit 1 | `fail` | `fail` |
| PASS | outputs | composite step outcome | `failure` | `failure` |
| PASS | outputs | output written by an exec program | `fail-exec` | `fail-exec` |
| PASS | outputs | line before the bad one kept | `1` | `1` |
| PASS | outputs | line after the bad one lost | `` | `` |
| PASS | outputs | bad line fails the step | `failure` | `failure` |
```

The two error lines of the step `bad`:

```
2026-11-22T10:48:45.0003927Z ##[error]Unable to process file command 'output' successfully.
2026-11-22T10:48:45.0006978Z ##[error]Invalid format 'this line has no separator'
```

After a cancel, the output that the recorder wrote at SIGINT was kept in all three cases where a signal reached it: a plain step under the job's limit, and a composite action under a step's limit and under a user's cancel:

```
| PASS | outputs | output of a cancelled plain step | `fail` | `fail` |
stcomp outcome=failure (failure expected) verdict=fail (fail expected)
uc outcome=cancelled (cancelled expected) verdict=fail (fail expected)
```

When a step wrote two values for one output, the last was the one kept: in the plain form, in the delimiter form, in both orders of the two, and through a composite action's outputs, also when the inner step then exited 1 under `continue-on-error`. All 10 checks of `last-value` passed:

```
| PASS | plain form twice: the last value | `pass` | `pass` |
| PASS | delimiter form twice: the last value | `pass` | `pass` |
| PASS | delimiter form, then plain: the last value | `pass` | `pass` |
| PASS | plain form, then delimiter: the last value | `pass` | `pass` |
| PASS | composite action, ending well: the last value | `pass` | `pass` |
| PASS | composite action, ending well: the other output | `/tmp/results` | `/tmp/results` |
| PASS | composite action, exit 1: the last value | `fail` | `fail` |
| PASS | composite action, exit 1: the other output | `/tmp/results` | `/tmp/results` |
| PASS | composite action, exit 1: outcome | `failure` | `failure` |
| PASS | composite action, exit 1: conclusion under continue-on-error | `success` | `success` |
failed checks: 0
```

The first value of each pair was `not-verified`. Under `continue-on-error` the step's `outcome` was `failure` and its `conclusion` `success`, and a later step read `verdict` as `fail`.

Read: the runner processes a step's files in a `finally` after the step's process has ended, whatever its result (`src/Runner.Worker/ActionRunner.cs`, lines 280 to 282); a line with neither `=` nor `<<` raises `Invalid format` (`src/Runner.Worker/FileCommandManager.cs`, line 438), which the runner reports as the first error line above and turns into a failed step (line 85 and the lines after it).

**The job summary: a new empty file for each step.** The step `empty`, which came right after the step that wrote 1048577 bytes, found its file empty, and its path differed from the `mask` step's:

```
| PASS | summary | each step starts with an empty file | `yes` | `yes` |
| PASS | summary | each step gets a new file | `new` | `new` |
```

```
  EMPTY_PATH: /home/runner/work/_temp/_runner_file_commands/step_summary_46821ae7-32c2-4f3b-a6b5-e601a7f580c7
  MASK_PATH: /home/runner/work/_temp/_runner_file_commands/step_summary_5bc35b35-c56c-4a22-a58d-f1e696c6930c
```

The step `readback` listed the folder. These are its `step_summary_` lines:

```
-rw-r--r-- 1 runner runner 1048577 Nov 22 10:48 step_summary_2b258ad2-7bb1-43f6-97b3-adb767172b57
-rw-r--r-- 1 runner runner       0 Nov 22 10:49 step_summary_3e2faff2-1c8a-433e-992f-b56f5226114e
-rw-r--r-- 1 runner runner       0 Nov 22 10:48 step_summary_46821ae7-32c2-4f3b-a6b5-e601a7f580c7
-rw-r--r-- 1 runner runner      29 Nov 22 10:48 step_summary_4bcfb6cc-7896-4065-b178-4a404deb7939
-rw-r--r-- 1 runner runner      29 Nov 22 10:48 step_summary_4bcfb6cc-7896-4065-b178-4a404deb7939-scrubbed
-rw-r--r-- 1 runner runner       0 Nov 22 10:49 step_summary_54ff3262-3c54-442d-b69a-79a8fccc5232
-rw-r--r-- 1 runner runner     216 Nov 22 10:48 step_summary_5bc35b35-c56c-4a22-a58d-f1e696c6930c
-rw-r--r-- 1 runner runner     206 Nov 22 10:48 step_summary_5bc35b35-c56c-4a22-a58d-f1e696c6930c-scrubbed
-rw-r--r-- 1 runner runner      28 Nov 22 10:48 step_summary_7749b0e0-f113-4260-ac57-2a3af7b420ea
-rw-r--r-- 1 runner runner      28 Nov 22 10:48 step_summary_7749b0e0-f113-4260-ac57-2a3af7b420ea-scrubbed
-rw-r--r-- 1 runner runner       0 Nov 22 10:48 step_summary_92952d01-be38-4c76-ae4a-340a9dea0ad8
-rw-r--r-- 1 runner runner       0 Nov 22 10:48 step_summary_be777d7f-c509-4c67-9533-d3d6c72424b4
-rw-r--r-- 1 runner runner       0 Nov 22 10:48 step_summary_bfc71411-ed3b-4f44-b373-03c7c3e43c52
-rw-r--r-- 1 runner runner       0 Nov 22 10:48 step_summary_c4b671e0-402e-47d4-8500-3ea82b277f0d
-rw-r--r-- 1 runner runner      33 Nov 22 10:48 step_summary_dc1b47f0-4fa1-48b3-a7c7-4f8abd523f5a
-rw-r--r-- 1 runner runner      33 Nov 22 10:48 step_summary_dc1b47f0-4fa1-48b3-a7c7-4f8abd523f5a-scrubbed
-rw-r--r-- 1 runner runner       0 Nov 22 10:48 step_summary_e6fcb283-300b-4092-889f-dc4a0ff3d985
-rw-r--r-- 1 runner runner      32 Nov 22 10:49 step_summary_f1fc408f-94bd-44ce-9180-4e5f85268d72
-rw-r--r-- 1 runner runner      32 Nov 22 10:49 step_summary_f1fc408f-94bd-44ce-9180-4e5f85268d72-scrubbed
```

There are 14 raw files, and 14 steps had started by then: the first step; the two `uses:` steps and the two inner steps that ran (`q1.dump`, `comp.inner`); `plain`, `execd`, `bad`, `mask`, `big`, `empty` and `jt`; the `cancelled()` step; and `readback` itself. The two inner steps that the log shows as skipped (`q1.inner` and `comp.dump`, each with `duration_ms=0`) had none. The sizes name the steps that wrote: 28 bytes is `S6-INNER-ENV summary marker` and its newline, in the file that `q1-composite.txt` gives as the inner step's own `GITHUB_STEP_SUMMARY`; 29 is `plain`'s marker, 33 `comp.inner`'s, 216 the `mask` step's text, 1048577 the `big` step's, and 32 `S6-SIGNAL-SUMMARY jt-plain-exec` and its newline. The file at `EMPTY_PATH` is 0 bytes. Each step's other files (`set_output_`, `set_env_`, `add_path_` and the rest) carry the same suffix as its summary file.

**The job summary: the files stay on disk for later steps.** The raw files and their `-scrubbed` copies of earlier steps, the composite action's inner steps among them, were on disk in the job's last steps, a minute after they were written:

```
plain_raw=1
plain_scrubbed=1
comp_raw=1
inner_env_raw=1
```

```
| PASS | summary | plain summary on disk later | `1` | `1` |
| PASS | summary | plain summary masked copy on disk | `1` | `1` |
| PASS | summary | composite inner summary on disk | `1` | `1` |
| PASS | summary | composite env-step summary on disk | `1` | `1` |
```

A `-scrubbed` copy exists only for a file that is neither empty nor above 1 MiB, as the listing shows. Read: the runner skips an empty file (`src/Runner.Worker/FileCommandManager.cs`, lines 241 to 245), returns before the copy for a file above the limit (lines 247 to 253), writes the copy line by line through the job's secret masker (lines 256 to 267), and queues the copy for upload (lines 269 to 275); it empties `RUNNER_TEMP` only when the job starts and when it ends (`src/Runner.Worker/JobRunner.cs`, lines 180, 303 and 392), and makes a new file for every step without deleting the earlier ones (`FileCommandManager.cs`, lines 42 to 56, in a new instance for each step, `src/Runner.Worker/ActionRunner.cs`, line 171).

**The job summary: masking.** The raw file of the `mask` step held the value once, and its copy held `***` instead. The copy is 10 bytes shorter: 216 − 13 + 3 = 206, the 13 characters of `S6SECRETVALUE` replaced by 3.

```
mask_raw=1
mask_scrubbed=0
mask_stars=1
```

```
| PASS | summary | raw file holds the secret | `1` | `1` |
| PASS | summary | masked copy hides the secret | `0` | `0` |
| PASS | summary | masked copy has *** | `1` | `1` |
```

The log masks the value too, once `::add-mask::` has run. The `mask` step's own script, which the log prints before the step runs, shows the value; the next step's script, printed after, does not:

```
2026-11-22T10:48:45.0143314Z echo "::add-mask::S6SECRETVALUE"
2026-11-22T10:48:45.0161183Z   echo 'masked value: S6SECRETVALUE'
2026-11-22T10:49:53.4753016Z   echo "mask_raw=$(grep -c *** "$MASK_PATH" || true)"
```

**The job summary: above 1 MiB.** The step that appended 1048577 bytes, one byte above 1048576, got an error annotation and stayed green; its raw file stayed, and it has no `-scrubbed` copy:

```
2026-11-22T10:48:45.0464499Z ##[error]$GITHUB_STEP_SUMMARY upload aborted, supports content up to a size of 1024k, got 1024k. For more information see: https://docs.github.com/actions/using-workflows/workflow-commands-for-github-actions#adding-a-markdown-summary
```

```
| PASS | summary | over 1 MiB keeps the step green | `success` | `success` |
```

The annotation names the same size twice. Read: the runner divides the file's size by 1024 as an integer (`src/Runner.Worker/FileCommandManager.cs`, line 249), so 1048577 bytes is reported as `1024k`, the limit's own figure.

**The job summary: written at the cancel signal.** In each of the three cases where SIGINT reached the recorder, the marker that it wrote at SIGINT was in the step's raw file and in its `-scrubbed` copy afterwards, so the runner processed the summary of the cancelled step. For the job's limit:

```
jt_raw=1
jt_scrubbed=1
| PASS | summary | summary of a cancelled step on disk | `1` | `1` |
| PASS | summary | summary of a cancelled step processed (masked copy) | `1` | `1` |
```

For the step's limit on the composite action (`step-timeout`), and for the user's cancel of the composite action (`user-cancel`); each file is 31 bytes, the marker and its newline:

```
/home/runner/work/_temp/_runner_file_commands/step_summary_e980ad56-f9ef-4121-b0e0-b7d94524ca74
/home/runner/work/_temp/_runner_file_commands/step_summary_e980ad56-f9ef-4121-b0e0-b7d94524ca74-scrubbed
```

```
/home/runner/work/_temp/_runner_file_commands/step_summary_430ac096-57b8-42cf-aa57-aed5f6c8ce45
/home/runner/work/_temp/_runner_file_commands/step_summary_430ac096-57b8-42cf-aa57-aed5f6c8ce45-scrubbed
```

**The job summary: how it renders.** Not looked at. The `mask` step's summary holds a table, fenced code, a `<details>` block, `<sub>`, `<kbd>` and the masked value; only a person looking at the run's page can say how GitHub shows them.

**Signals.** In the three cases where the step's first process was the recorder, it got SIGINT, then SIGTERM 7.50 to 7.55 s later, and was ended about 10 s after SIGINT. In the case of a shell with a child, the child got no signal at all:

| Case | Job, step | The step's first process, by the recorder's parent line | SIGTERM after SIGINT | Last `alive` after SIGINT | The step's outcome | Output and summary written at SIGINT |
| --- | --- | --- | --- | --- | --- | --- |
| The job's limit, a plain step that `exec`s the recorder | `files-then-job-timeout`, `jt` | the recorder; parent `/home/runner/actions-runner/cached/2.338.2/bin/Runner.Worker spawnclient 142 145` | 7.502 s | 9.680 s | cancelled | kept |
| A step's `timeout-minutes` on the `uses:` step; the inner step `exec`s the recorder | `step-timeout`, `stcomp` | the recorder; parent the same `Runner.Worker` line | 7.501 s | 9.614 s | failure | kept |
| A user's cancel; a composite action whose inner step `exec`s the recorder | `user-cancel`, `uc` | the recorder; parent the same `Runner.Worker` line | 7.545 s | 9.693 s | cancelled | kept |
| A step's `timeout-minutes` on a shell with a child and no `exec` | `step-timeout`, `stchild` | bash; the recorder's parent is `/usr/bin/bash --noprofile --norc -e -o pipefail /home/runner/work/_temp/17323df3-83bf-4d6b-b9bd-ba06faf4c89e.sh` | no signal | none: alive until the job's end | failure | none written: no signal came |

The recorders' lines, without the `alive` lines between; the intervals in the table are differences of their EPOCH values. `jt`:

```
1795344525.109 +0.000 start pid=2011 ppid=1976 pgid=1671 sid=1671
1795344525.109 +0.000 self: python3 /home/runner/work/REPOSITORY/REPOSITORY/.proof/rec.py jt-plain-exec 150
1795344525.109 +0.000 parent: /home/runner/actions-runner/cached/2.338.2/bin/Runner.Worker spawnclient 142 145
1795344583.452 +58.343 signal SIGINT
1795344583.452 +58.343 wrote verdict and summary marker
1795344590.954 +65.845 signal SIGTERM
first alive: 1795344525.109 +0.000 alive ppid=1976
last alive:  1795344593.132 +68.023 alive ppid=1976
```

`stcomp`:

```
1795344596.980 +0.000 start pid=1926 ppid=1882 pgid=1791 sid=1791
1795344596.980 +0.000 self: python3 /home/runner/work/REPOSITORY/REPOSITORY/.proof/rec.py st-composite 150
1795344596.980 +0.000 parent: /home/runner/actions-runner/cached/2.338.2/bin/Runner.Worker spawnclient 142 145
1795344656.885 +59.906 signal SIGINT
1795344656.885 +59.906 wrote verdict and summary marker
1795344664.386 +67.406 signal SIGTERM
first alive: 1795344596.980 +0.000 alive ppid=1882
last alive:  1795344666.499 +69.520 alive ppid=1882
ppid changes:
    140 ppid=1882
```

`uc`:

```
1795344716.823 +0.000 start pid=1967 ppid=1940 pgid=1869 sid=1869
1795344716.823 +0.000 self: python3 /home/runner/work/REPOSITORY/REPOSITORY/.proof/rec.py uc-composite 150
1795344716.823 +0.000 parent: /home/runner/actions-runner/cached/2.338.2/bin/Runner.Worker spawnclient 142 145
1795344742.642 +25.820 signal SIGINT
1795344742.642 +25.820 wrote verdict and summary marker
1795344750.187 +33.365 signal SIGTERM
last alive: 1795344752.335 +35.513 alive ppid=1940
```

`stchild`:

```
1795344524.390 +0.000 start pid=1907 ppid=1906 pgid=1791 sid=1791
1795344524.390 +0.000 self: python3 /home/runner/work/REPOSITORY/REPOSITORY/.proof/rec.py st-child 150
1795344524.390 +0.000 parent: /usr/bin/bash --noprofile --norc -e -o pipefail /home/runner/work/_temp/17323df3-83bf-4d6b-b9bd-ba06faf4c89e.sh
first alive: 1795344524.391 +0.000 alive ppid=1906
last alive:  1795344666.439 +142.049 alive ppid=1
ppid changes:
    135 ppid=1906
    150 ppid=1
```

The end of the step. The recorder does not exit on SIGINT or SIGTERM, so a signal that it cannot catch ended it. The runner's line that closes each step is stamped about 10.0 s after SIGINT and 2.5 s after SIGTERM:

```
2026-11-22T10:49:53.4591931Z ##[error]The operation was canceled.
2026-11-22T10:51:06.8904187Z ##[error]The action has timed out.
2026-11-22T10:52:32.7021696Z ##[error]The operation was canceled.
```

Counted from each job's `setup done` line, the three SIGINTs fall at 10:49:43.455, 10:50:56.887 and 10:52:22.647, so these lines come 10.004 s, 10.003 s and 10.055 s after SIGINT, and 2.502 s, 2.502 s and 2.510 s after SIGTERM. The last `alive` lines, 9.680 s, 9.614 s and 9.693 s after SIGINT, fit: the next was due about 0.5 s later. Read: the runner sends SIGINT with `kill(pid)` to the process it started, waits 7.5 s, sends SIGTERM, waits 2.5 s, and then calls `Process.Kill()` on the same process (`src/Runner.Sdk/ProcessInvoker.cs`, lines 32 and 33 for the two waits, 443 to 465 for the order, 832 for the `kill`, 855 to 869 for the last step). GitHub's documentation calls that last step a kill of the process tree (`content/actions/reference/workflows-and-actions/workflow-cancellation.md`, line 21, of `github/docs` at commit `d7487f5debb5a5a2a6e65e6f9fa539266f5f8db6`, read); on Linux the source kills that one PID.

The limits that started the sequence. The job's limit of 1 minute sent SIGINT at 10:49:43.455, 59.358 s after the job's first log line (`2026-11-22T10:48:44.0964545Z Current runner version: '2.338.2'`). The step's limit on `stcomp` sent it 59.937 s after the step's line `2026-11-22T10:49:56.9501756Z ##[group]Run ./.proof/act`.

**The child without `exec`.** No signal line: neither SIGINT nor SIGTERM reached the child. Its parent changed from bash (1906) to PID 1 between its 135th and 136th `alive` lines, of 285 in all from +0.000 to +142.049, a mean spacing of 142.049 / 284 = 0.50017 s; so bash ended about 67.02 to 67.52 s after the recorder started. The runner logged the step's timeout at 72.490 s, counting the recorder's start, EPOCH 1795344524.390, as 10:48:44.392 from the `setup done` line:

```
2026-11-22T10:48:44.3736114Z ##[group]Run python3 "$GITHUB_WORKSPACE/.proof/rec.py" st-child 150
2026-11-22T10:49:56.8826137Z ##[error]The action 'a child without exec, under a step timeout' has timed out after 1 minutes.
```

These times fit the sequence read in the source. If the step's minute is counted from its `Run` line, 0.02 s before the recorder started, it ran out at about +59.98 s; bash got SIGINT then and did not end, and the SIGTERM 7.5 s later, at about +67.48 s, falls inside the window in which bash ended. The runner then waited 5 s more for the step's output pipes, which the child still held (read: `src/Runner.Sdk/ProcessInvoker.cs`, lines 485 to 499, the wait at line 494), and logged the timeout at +72.49 s, against 59.98 + 7.5 + 5.0 = 72.48 s. That bash does not act on a SIGINT while it waits for a foreground child is in bash's manual, section SIGNALS, read in the manual of bash 5.3, not of the image's 5.2.21.

The child outlived the step. Its last `alive` line, EPOCH 1795344666.439 with `ppid=1`, is at 10:51:06.441, 69.56 s after the step's timeout line; the `print` step, which read its log, started at `print started at 1795344666.902414086`. At the end of the job the runner killed it as an orphan; PID 1907 is the child's, by its start line:

```
2026-11-22T10:51:06.9368474Z Cleaning up orphan processes
2026-11-22T10:51:06.9575531Z Terminate orphan process: pid (1907) (python3)
```

That is 70.075 s after the step's timeout line. Read: the runner marks every process it starts with `RUNNER_TRACKING_ID` and, at the job's end, kills each process that carries the job's value (`src/Runner.Worker/JobExtension.cs`, lines 588 to 593 and 891 to 930).

The process groups. In `step-timeout`, the child of bash and the recorder that the worker started itself in the next step were both in process group 1791 (`pgid=1791` in both start lines), which is neither's PID nor either parent's: the runner gives a step no process group of its own.

**The user's cancel.** The canceller printed `cancel sent` just before `gh run cancel` started, 25.009 s after `armed at 1795344716.672924842`; the recorder got SIGINT at 1795344742.642, 0.960 s after `cancel sent`:

```
cancel sent at 1795344741.682016065
✓ Request to cancel workflow 45014479499 submitted.
```

The inner step ended cancelled, and then the `cancelled()` step and the `always()` step both ran:

```
2026-11-22T10:52:32.7030362Z ##[end-action id=uc.inner;outcome=cancelled;conclusion=cancelled;duration_ms=35915]
2026-11-22T10:52:32.7191166Z cancelled() step ran at 1795344752.718766856
2026-11-22T10:52:32.7421304Z always() step started at 1795344752.740498401
2026-11-22T10:52:32.7583993Z still running at 1795344752
2026-11-22T10:52:47.7625044Z still running at 1795344767
2026-11-22T10:53:02.7745226Z Cleaning up orphan processes
```

The `always()` step ran to the end of its own window, which with a 15 s sleep lasted 30.032 s from its first line to the job's cleanup line; nothing stopped it. The cleanup line came 41.088 s after `cancel sent`, counted from this job's `setup done` line. In `files-then-job-timeout` too, after the job's limit, the `cancelled()` step and both `always()` steps ran:

```
2026-11-22T10:49:53.4707555Z cancelled() step ran at 1795344593.470402418
```

## Decision

The plan for step 6 does this because of the result:

- **`env`.** The action passes nothing for `env`: it has no input for it. A caller who wants a variable in the tool's environment sets `env:` on the step that calls the action, or at the job's or the workflow's level. A name that the action's inner step sets in its own `env:` wins over a caller's, so that step sets only what it needs. Point 3's ways out, `env:` at the job's level only and an input that carries names and values, are not needed.
- **Outputs.** The tool writes `verdict` and `out` to `$GITHUB_OUTPUT` itself, and the action's `outputs:` map the inner step's (`value: ${{ steps.ID.outputs.verdict }}`, with an `id` on the inner step). They are kept when the tool exits non-zero and when it is stopped by a cancel or a limit, and the composite action passes them on in each case. The tool writes `verdict` twice, as `not-verified` when it has its results folder and again when the verdict exists, and a caller reads the second; a run that is killed between the two leaves the first. A caller's step with `continue-on-error` still gives a later step the `verdict` of a run that failed. An unparsable line fails the step and loses every output written after it, so the tool writes each output in the delimiter form (`NAME<<DELIMITER`, the value, the delimiter), with a delimiter that the value cannot hold on a line of its own.
- **Signals.** The action's inner step starts the tool with `exec`, so that the tool is the step's first process, the process that gets the runner's signals. After a cancel the tool has 7.5 s: SIGTERM comes 7.5 s after SIGINT, and by the tool's own rule (section 10, "On a signal") a second signal ends it at once. So section 10's "about 10 seconds" becomes 7.5 s, and in that time the tool writes the final `result.json`, the outputs and the summary. A caller's limit on the action's step (`timeout-minutes` on the `uses:` step) reaches the tool the same way, as a SIGINT to the `exec`ed tool; the step's outcome is then `failure`, not `cancelled`. A shell that ran the tool without `exec` would pass it nothing, and the tool would run on after the step, until the job's end.
- **The action's own test.** It may read a step's summary from its `-scrubbed` copy under `$RUNNER_TEMP/_runner_file_commands/`, found by its content, in a later step of the same job. That copy is masked and is what the runner uploads (read). GitHub does not document the folder or the copy, so the test fails, and says why, when the folder or the copy is missing.

What was not measured:

- How the job summary renders on the run's page: the table, the fenced code, `<details>`, `<sub>`, `<kbd>` and the masked value of the `mask` step. A person has to look; the maintainer is asked to look at the summary of run 45014226495, job `files-then-job-timeout`.
- How long an `always()` step may run after a cancel. The run used a window of 20 s, and the step ran 30.032 s without being stopped. Read: the runner's listener kills the worker after the job's cancel timeout, 5 minutes by default, less 15 s, about 4 min 45 s (`src/Runner.Listener/JobDispatcher.cs`, lines 1279 to 1285; `src/Sdk/DTPipelines/Pipelines/PipelineConstants.cs`, line 17). The tool does not depend on it: it ends within 7.5 s.
- A shell's child under a user's cancel or a job's limit: the child case ran only under a step's limit. The source sends the same signals in all three cases (read: each cancels the step's token, and `src/Runner.Sdk/ProcessInvoker.cs` answers a cancelled token in one way, lines 443 to 465).
- A self-hosted runner; Windows and macOS runners; GitHub Enterprise Server; the `ubuntu-26.04` image; any runner release other than 2.338.2. Each job ran once.

Plan assumed: "`env:` on the step that calls a composite action reaches the action's inner run steps, as do the job's and the workflow's `env:`; an inner step's own `env:` wins."
Holds: yes
Plan assumed: "The outputs a step wrote are kept when the step then exits non-zero or is cancelled, and a composite action passes an inner step's output on in both cases."
Holds: yes
Plan assumed: "When a step writes two values for one output, GitHub's runner keeps the last, in the plain form, in the delimiter form and through a composite action's outputs."
Holds: yes
Plan assumed: "On a cancel, on a job's timeout and on a step's timeout alike, GitHub's runner sends SIGINT to the step's first process only, SIGTERM 7.5 s later, and ends it 2.5 s after that. A child of a shell gets no signal. A program that an inner step of a composite action starts with `exec` is that first process."
Holds: yes
Plan assumed: "Every step, an inner step of a composite action included, gets a new empty summary file. A step's summary and its masked copy stay under `$RUNNER_TEMP/_runner_file_commands/` until the job ends. A summary above 1 MiB is not uploaded and its step stays green."
Holds: yes
Plan assumed: "The job summary renders tables, fenced code, `<details>` and raw HTML on the run's page, with a masked value shown as `***`."
Holds: not measured here
