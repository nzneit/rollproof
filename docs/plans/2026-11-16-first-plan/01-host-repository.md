> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

## Part 1: The host repository

### Task 1: Record the incubation in the host repository

This is the one task that changes the host repository's own files, so it follows the host's rule for changing a rule: the row first, then the files that cite it, in one commit. **The maintainer agrees its text before this task starts**; a change to the text changes the three lines that follow from it.

This plan's folder is committed with this task and not before. Until `projects/` is a folder that the citation check does not read, the check reads the plan's files and reports every path in them that is not built yet.

This file quotes the incubating repository's citation tool and its rule files. In what follows, `ROW` stands for the ID of a row of that repository's log and `RESEARCH/` for the path of its research folder; neither is published with the project.

Paths and commands in this task are from the repository root.

**Files:**
- Modify: `docs/decisions-and-open-items.md` (one row, directly under the table's header, above its newest row)
- Modify: `AGENTS.md` (the list under "How to work")
- Modify: `CLAUDE.md` (the list of notes)
- Modify: `README.md` (the table of folders)
- Modify: `tools/citations/model.ts:80-85`
- Modify: `tools/citations/README.md:37`, `tools/citations/DESIGN.md` (lines 18, 30, 39, 63-64, 124, 126, 326, 345)
- Test: `tools/citations/integrity.test.ts`, `tools/citations/load.test.ts`
- Add to the commit: `projects/rollproof/docs/plans/2026-11-16-first-plan/` (this plan)

**Interfaces:**
- Consumes: nothing from another task.
- Produces: `UNREAD_FOLDERS: readonly string[] = ["sketches/", "RESEARCH/", "projects/"]`
  The folders whose Markdown files the citation check does not read. A path
  into one still resolves when another document cites it.

- [ ] **Step 1: Write the failing tests**

In `tools/citations/integrity.test.ts`, add this case directly after the case named "research reports are not checked":

```ts
  ["incubating projects are not checked",
    () => repo({ "projects/x/docs/plan.md": "See ROW and `internal/nope.go` and `docs/nope.md`.\n" }), []],
  ["paths that only resemble projects/ are checked",
    () => repo({ "projects.md": "ROW\n", "docs/projects/x.md": "ROW\n", "Projects/x.md": "ROW\n" }),
    ["Projects/x.md:1 error unknown-row", "docs/projects/x.md:1 error unknown-row", "projects.md:1 error unknown-row"]],
```

In `tools/citations/load.test.ts`, replace the last case of `failsClosed`, named "no Markdown outside sketches/ and RESEARCH/", with:

```ts
  ["no Markdown outside sketches/, RESEARCH/ and projects/",
    { ...Object.fromEntries(Object.keys(BASE).filter((p) => p !== UNBUILT).map((p) => [p, null])),
      "sketches/a.md": "x\n", "RESEARCH/2026-11-10/r.md": "x\n", "projects/x/README.md": "x\n" },
    {}, ["no tracked Markdown", "sketches/", "RESEARCH/", "projects/"]],
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `bun test tools/citations`
Expected: FAIL in "incubating projects are not checked", with `projects/x/docs/plan.md:1 error unknown-row` among the findings, and in the replaced `load.test.ts` case, because a file under `projects/` is still read.

- [ ] **Step 3: Add `projects/` to `UNREAD_FOLDERS` in `tools/citations/model.ts`**

Replace the comment and the constant at lines 80 to 85 with:

```ts
/** Folders whose Markdown files the snapshot does not read, so checkRepo never checks them:
 *  sketches/ holds unreviewed suggestions, RESEARCH/ holds dated research snapshots that
 *  are not edited after the day they are written (ROW), and projects/ holds incubating
 *  projects, whose files cite no row of the log (ROW). A path lies in one when it starts with
 *  that text at the repository root, compared case-sensitively. Paths into them still resolve,
 *  because path resolution uses the tracked-file list. */
export const UNREAD_FOLDERS: readonly string[] = ["sketches/", "RESEARCH/", "projects/"]
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `bun test tools/citations`
Expected: PASS, with no failing test.

- [ ] **Step 5: Add the row to `docs/decisions-and-open-items.md`**

Insert one line directly under the header of the Decisions table, above its newest row. It is one line in the file.

The row's text is not published with the project: it belongs to the incubating repository's log. Section 13 of the design says what it decided.

- [ ] **Step 6: Add the matching lines to the two rule files and to the README**

In `AGENTS.md`, add this item after item 6 of the list under "How to work":

```markdown
7. **Incubating projects stand apart.** A project that will leave this repository lives under `projects/<name>/`, laid out as its own repository root. The rules of this file do not apply inside `projects/`, a project's files cite no row of the log, and the citation check does not read them. A change that a project needs outside its own directory follows the rules of this file. [ROW]
```

In `CLAUDE.md`, add this note at the end of the list:

```markdown
- `projects/` holds projects that incubate here and will leave for a repository of their own (ROW). Inside it the rules above, `docs/plan-template.md` and `docs/review-rubric.md` do not apply; a project keeps its own design and plans under its own `docs/` folder.
```

In `README.md`, add this row to the table of folders, directly above the row for `sketches/`:

```markdown
| `projects/` | Projects that incubate here and will leave for a repository of their own. Each is laid out as its own repository root. This repository's rules do not apply inside, and the citation check does not read the files there (ROW). The first is `projects/rollproof/`. |
```

- [ ] **Step 7: Make the citation tool's own documents say three folders**

- `tools/citations/README.md`, line 37: "outside `sketches/` and `RESEARCH/`. Files in those two folders" becomes "outside `sketches/`, `RESEARCH/` and `projects/`. Files in those three folders".
- `tools/citations/DESIGN.md`: line 18 gains a second sentence, "Files under `projects/` are not checked; they belong to incubating projects, which cite no row of the log (ROW)." At lines 30, 39, 63-64, 124, 326 and 345, "`sketches/` and `RESEARCH/`" becomes "`sketches/`, `RESEARCH/` and `projects/`", with or without the backticks that the line has, and line 124's "neither folder" becomes "none of the three folders". Line 126 gains a last sentence: "`projects/` is not read for the same reason: a finding there could be cleared only by making a project cite this log (ROW)."

- [ ] **Step 8: Run the check on the repository**

Run: `bun test tools/citations`
Expected: PASS.

Run: `bun tools/citations/cli.ts repo`
Expected: `citation check: 0 errors, 0 warnings`

- [ ] **Step 9: Commit**

```bash
git add docs/decisions-and-open-items.md AGENTS.md CLAUDE.md README.md tools/citations/model.ts tools/citations/README.md tools/citations/DESIGN.md tools/citations/integrity.test.ts tools/citations/load.test.ts projects/rollproof/docs/plans/2026-11-16-first-plan
git commit -m "Record that projects incubate under projects/, and keep the citation check out of them"
```

Expected: the pre-commit hook prints `citation check: 0 errors, 0 warnings` and the commit is made.
