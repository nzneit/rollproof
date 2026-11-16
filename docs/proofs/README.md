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

Task 11 fills this list from the records.

| Point | Question | Result | Decision | Plan assumed | Holds | Tasks that rest on it |
| --- | --- | --- | --- | --- | --- | --- |
| 1, first half | | | | | | |
| 1, second half | | | | | | |
| 5, command lines | | | | | | |
| 8, nerdctl | | | | | | |
| 8, Docker | | | | | | |
| 9 | | | | | | |
| 10 | | | | | | |
| 11 | | | | | | |
| 12 | | | | | | |
