# rollproof

`rollproof` is a working name.

It tests, in a CI job or on a laptop, whether deploying a new version of a Kubernetes workload, and rolling it back, causes downtime.

Nothing is released; the design is docs/specs/2026-11-13-rollproof-design.md.

What works now, on Linux with nerdctl: `rollproof run` creates its own kind cluster; deploys through Helm, with or without a previous release (a run with none tests a restart), or through the commands driver; runs the `setup` commands; judges with the built-in HTTP probe or with a custom probe that `docs/probe-contract.md` describes; takes images from the engine or from an archive; and writes `result.json`, the console report and the evidence folder.

Every fixture of the design's section 12 gives its expected verdict under Helm 3.22.1 and Helm 4.3.2, and the first fixture also on `kindest/node:v1.34.11`, in one run of the suite on 2026-11-19 and 2026-11-20.

To run the suite: `go run ./hack/fetchhelm` fetches under `.tools/` the Helm binaries and, for linux/amd64 only, kubectl 1.37.0, which the suite needs; the suite's first run pulls busybox 1.37.0 by its digest into `.tools/`; `go test -tags e2e -p 1 ./e2e/ -timeout 360m` runs the suite in about two and a half hours.

Every key of version 1 is built: `registries` and `as` in `images` came with step 5.

Run the unit tests with `go test ./...`.
