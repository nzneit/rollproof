# Task 5: the maintainer's five trademark searches, recorded

Read section 14, point 14, the design's paragraph that begins "`rollproof` is a working name", section 13 ("Before"), and `docs/proofs/p14-name.md` whole, before this task.

This task rests on nothing and nothing rests on it but Task 6. It goes as far as its stop and waits for the maintainer while Tasks 1 to 4 go on. The maintainer's ruling of 2026-11-24, which point 34 keeps, is that the maintainer runs the five searches in a browser before anything is pushed: the databases refuse a script (`docs/proofs/p14-name.md`, "Trademarks and companies").

### Task 5: what the five databases showed for the word `rollproof`, written into the record of the name check

**Files:**
- Modify: `docs/proofs/p14-name.md` (its "Trademarks and companies"; the third bullet of its Decision; its third pair), `docs/proofs/README.md` (the row for point 14; "Still unproved"), `docs/specs/2026-11-13-rollproof-design.md` (the paragraph on the name; section 13, "Before"; point 14, its last two sentences)

**Interfaces:**
- Consumes: nothing from another task.
- Produces: the third pair of `p14-name.md` with `Holds: yes`, or the plan stopped.

- [ ] **Step 1: Stop for the maintainer's report**

Ask, in these words or plainer: "What did each of the five searches show for the word `rollproof`: TMview, EUIPO's eSearch, WIPO's Global Brand Database, the USPTO's search and the United Kingdom's? For each: no mark, or the marks and their classes." Take the answer as the maintainer gives it; a database the maintainer could not search is written as not searched, with the reason.

**A mark for software, in the nice classes 9 or 42, stops the plan.** Point 14: "A mark for software stops the plan for graduation, and the name is chosen again." When the report names one, write nothing, commit nothing, and report the mark to the maintainer: the rest of this plan waits for a new name and a new design.

**State while it waits:** nothing committed; Task 6 waits for this task.

- [ ] **Step 2: Write the report into `docs/proofs/p14-name.md`**

Each cell comes from the maintainer's report, in the maintainer's words or trimmed from them, with the day; nothing is written from memory.

1. Under "### Trademarks and companies", after the table, one sentence and one table: "The maintainer searched the five databases in a browser on DATE, for the word `rollproof`:" then `| Database | What the search showed | Finding |` with five rows, in the order of Step 1's question. A row's Finding is `none`, or `noted` for a mark outside classes 9 and 42, or `not searched` with the reason.
2. Under "## Decision", the bullet that begins "**The trademark half is open.**" is replaced by: "**The trademark half is closed.** The maintainer searched the five databases in a browser on DATE, before anything was pushed, and found no mark for software in the nice classes 9 or 42. What each showed is above." When a mark outside those classes was noted, the bullet names it.
3. The third pair's second line becomes `Holds: yes`. When one database was not searched, it becomes `Holds: not measured here`, and the Decision says which.
4. The line under "Machine and versions" that names the day and the tools gains: "The five searches of the trademark databases were made by the maintainer in a browser on DATE."

- [ ] **Step 3: `docs/proofs/README.md` and the design**

In `docs/proofs/README.md`:

1. The row of point 14: its Result ends with what the five searches showed, in a clause; its Decision no longer says that the maintainer searches before the split; its `Holds` is `yes`, or `not measured here` as the record says.
2. In "Still unproved", the bullet that begins "From `p14-name`:" loses "the trademark databases, TMview, EUIPO's eSearch, WIPO's Global Brand Database, the USPTO's search and the United Kingdom's, which refuse a script; the maintainer searches them in a browser before the split". A database not searched stays there, with its reason.

In the design:

1. The paragraph after the status header: "The trademark databases refuse a script; the maintainer searches them in a browser before graduation." becomes "The trademark databases refuse a script; the maintainer searched them in a browser on DATE, and what they showed is in the same record."
2. Section 13, "**Before.**": after "The name was checked on 2026-11-24 (`docs/proofs/p14-name.md`).", one sentence: "Its trademark half was searched on DATE (the same record)."
3. Point 14, its last two sentences: "The trademark databases refuse a script, so the maintainer searches them in a browser before anything is pushed. A mark for software stops the plan for graduation, and the name is chosen again." become "The trademark databases refuse a script; the maintainer searched them in a browser on DATE, before anything was pushed, and found no mark for software (`docs/proofs/p14-name.md`)."

When a sentence of this step says "found no mark for software" and the report noted a mark outside the two classes, the sentence adds "in the nice classes 9 and 42".

- [ ] **Step 4: The checks, and the commit**

From the repository root: the publication search (`tree`) and `bun tools/citations/cli.ts repo` both exit 0. Then:

```
git add projects/rollproof/docs/proofs/p14-name.md projects/rollproof/docs/proofs/README.md projects/rollproof/docs/specs/2026-11-13-rollproof-design.md
git commit -m "Record the maintainer's five trademark searches for the name rollproof"
```

**Done when:** the record of the name check says what each of the five searches showed, its third pair holds, and the design says so; or a mark for software was reported and the plan is stopped.
