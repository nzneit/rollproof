# rollproof

`rollproof` is a working name.

It tests, in a CI job or on a laptop, whether deploying a new version of a Kubernetes workload, and rolling it back, causes downtime.

Nothing is released; the design is docs/specs/2026-11-13-rollproof-design.md.

What works now, on Linux with nerdctl: `rollproof run` creates its own kind cluster; deploys through Helm, with or without a previous release (a run with none tests a restart), or through the commands driver; runs the `setup` commands; judges with the built-in HTTP probe or with a custom probe that `docs/probe-contract.md` describes; takes images from the engine or from an archive; and writes `result.json`, the console report and the evidence folder.

`registries` tells the cluster's nodes how to pull from a host. A `mirror` replaces the host: the nodes pull its images from the mirror alone, and a mirror that lacks an image fails the pull. A `ca` adds a certificate authority for the place the nodes pull from. `username` and `password` go into a credentials file that every node mounts; any pod that mounts that path of its node can therefore read it, so give credentials that can only read and expire soon. Write Docker Hub as `docker.io`. `as` in `images` stores an image under the repository that the real manifests name, so the manifest under test is the one that deploys. A registry password is masked as `***` in the console, in `result.json` and in the evidence folder, in its plain form, its JSON spellings (with and without HTML escaping), its Go-quoted spelling, and the base64 of `USERNAME:PASSWORD` (padded and not); nothing else is masked. A mirror that the nodes must reach without a proxy goes into the caller's own `NO_PROXY`, which kind passes into the nodes (read in kind's source, not run).

Every fixture of the design's section 12 gives its expected verdict under Helm 3.22.1 and Helm 4.3.2, and the first fixture also on `kindest/node:v1.34.11`, in one run of the suite on 2026-11-19 and 2026-11-20.

To run the suite: `go run ./hack/fetchhelm` fetches under `.tools/` the Helm binaries and, for linux/amd64 only, kubectl 1.37.0, which the suite needs; the suite's first run pulls busybox 1.37.0 by its digest into `.tools/`; `go test -tags e2e -p 1 ./e2e/ -timeout 360m` runs the suite in about two and a half hours.

The mirror job, `go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m` (with `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before it on nerdctl), runs the first fixture through a mirror that needs a login, sits under a path and serves TLS from a private authority, with the nodes' route to public registries cut, and then a run whose candidate the mirror lacks. Its workflow in the host repository, `rollproof-mirror.yml`, starts only by hand.

Every key of version 1 is built: `registries` and `as` in `images` came with step 5.

Run the unit tests with `go test ./...`.
