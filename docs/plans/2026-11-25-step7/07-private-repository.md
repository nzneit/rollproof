# Task 8: the private repository, and the push

Read section 13 ("Graduation", its step 3) and section 14, point 34 ("The owner and the name", "Private before public"), and the README's Review Focus 5, before this task.

This task needs the maintainer's word before its Step 4. Until that word it stops at Step 3.

### Task 8: the repository is created private and empty, and the audited clone's `main` is pushed to it

**Files:**
- Modify, outside any repository: `WORK/split/.git/config` (one remote added)
- Create on GitHub: `OWNER/rollproof`, private

**Interfaces:**
- Consumes: `WORK/split/`, `WORK/commit-map`, `WORK/audit-after-split-history.txt` (Task 7); Task 5's record in the clone's history (the trademark searches precede any push).
- Produces: `OWNER/rollproof` on GitHub, private, its default branch `main` at the clone's HEAD. `WORK/repo/` (Task 9) clones it.

- [ ] **Step 1: Check that Task 7's result is still here**

Each alone: `ls /tmp/opencode/rollproof-graduation/split/.git/filter-repo/commit-map /tmp/opencode/rollproof-graduation/commit-map /tmp/opencode/rollproof-graduation/audit-after-split-history.txt`. Expected: three names. When one is missing (a reboot empties `/tmp`), Task 7 has not survived: it is repeated in full from its Step 2, under the word it was already given, before this task goes on.

- [ ] **Step 2: Repeat the search on the day of the push**

```
bash /tmp/opencode/rollproof-graduation/publish-audit.sh HOST LOG history /tmp/opencode/rollproof-graduation/split > /tmp/opencode/rollproof-graduation/audit-on-push-day.txt
```

Expected: exit 0, and the file's last line reads `0 distinct lines in 0 files`. Anything else stops the task as Task 7's Step 7 does.

- [ ] **Step 3: Check that the name is still free, and stop for the maintainer's word**

Each alone: `gh auth status` shows an account logged in to `github.com`; `gh repo view OWNER/rollproof` fails with "Could not resolve to a Repository". When the repository exists, stop and report: it was not this plan that made it.

Then ask, in these words or plainer: "The split is audited and clean. May I create the private repository `OWNER/rollproof` and push the clone's `main` to it? Nothing else is pushed."

**State while it waits:** the clone audited and clean, in `WORK/split`; nothing on GitHub.

- [ ] **Step 4: After the word, create, add the remote, push**

Each alone, with `OWNER` replaced by the account's name:

```
gh repo create OWNER/rollproof --private
git -C /tmp/opencode/rollproof-graduation/split remote add origin https://github.com/OWNER/rollproof.git
git -C /tmp/opencode/rollproof-graduation/split push origin main
```

Expected from the first: the new repository's address. Expected from the push: `main -> main` and no error. When GitHub refuses the push, with a message of its push protection or of a login: stop; report the message to the maintainer; change no configuration of git, of `gh` or of the repository.

- [ ] **Step 5: Check what is on GitHub**

```
gh repo view OWNER/rollproof --json nameWithOwner,visibility,isPrivate,defaultBranchRef
```

Expected: `"visibility": "PRIVATE"`, `"isPrivate": true`, and `main` as the default branch's name. When the default branch is another name: `gh repo edit OWNER/rollproof --default-branch main`, then this command again.

Then, each alone: `git -C /tmp/opencode/rollproof-graduation/split rev-parse HEAD`, and `git -C /tmp/opencode/rollproof-graduation/split ls-remote --heads origin`. Expected: the second prints one line, `main` and the hash the first printed.

- [ ] **Step 6: Report**

The repository's address, that it is private, the hash of its `main`, and that one branch and no tag was pushed.

**Done when:** `OWNER/rollproof` exists, is private, and its `main` is the audited clone's HEAD; or the maintainer has GitHub's message for a refused push.
