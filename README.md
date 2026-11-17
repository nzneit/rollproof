# rollproof

`rollproof` is a working name.

It tests, in a CI job or on a laptop, whether deploying a new version of a Kubernetes workload, and rolling it back, causes downtime.

Nothing is released; the design is docs/specs/2026-11-13-rollproof-design.md.

What works now, on Linux with nerdctl: `rollproof run` creates its own kind cluster, installs a previous release with Helm, deploys the candidate and rolls it back, all under the built-in HTTP probe. The first four fixtures of the design's section 12 give their expected verdicts under Helm 3.22.1 and Helm 4.3.2. That suite needs the Helm binaries under `.tools/`; `go run ./hack/fetchhelm` fetches them first, then run `go test -tags e2e -p 1 ./e2e/ -timeout 120m`.

Not built yet: `registries`, `setup`, `deploy.commands`, `probe.custom`, `archive` and `as` in `images`, and a Helm run with no `previous`. A configuration that names one of them ends as `config-rejected`, with the text "not built yet" and the key's name.

Run the unit tests with `go test ./...`.
