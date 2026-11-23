// Package e2e runs the real rollproof binary against the fixture service,
// one fixture of the design's section 12 at a time, under Helm 3 and Helm 4.
//
// The fixture rows carry the build tag e2e. The tests of config_test.go,
// mirror_helpers_test.go and runenv_test.go carry none: they check the run
// description, the window marks, the actions, the harness's environment and
// the mirror job's helpers with no cluster, so `go test ./...` runs them, and
// `go test -tags e2e ./e2e/` runs them too.
//
// The suite runs every row of section 12 under the Helm majors that the
// variable ROLLPROOF_E2E_HELM selects: 3 or 4 for one of them, both when it is
// not set, as a developer's run leaves it. The fourteenth row's control runs
// under Helm 3 either way. Under both majors the suite takes about two and a
// half hours on a workstation. It needs Linux, a container engine that kind
// supports (docker, nerdctl or podman), the Helm binaries and kubectl under
// .tools/, which `go run ./hack/fetchhelm` puts there (kubectl for linux/amd64
// only), and busybox's archive at .tools/busybox-1.37.0.tar, which the first
// run of the suite pulls and saves.
// Each run creates the cluster rollproof-e2e and deletes it at its end, so
// the suite runs one test at a time:
//
//	go test -tags e2e -p 1 ./e2e/ -v -timeout 360m
//	env ROLLPROOF_E2E_HELM=4 go test -tags e2e -p 1 ./e2e/ -v -timeout 180m
//
// The harness keeps GITHUB_OUTPUT and GITHUB_STEP_SUMMARY out of the runs it
// starts, so that on GitHub a job's summary is not the sum of its runs'. The
// first fixture's test and the mirror job's runs get files of their own for
// both, github-output and github-summary in their results folder, and read
// them. The results folders stay under rollproof-e2e-results in the system's
// temporary folder.
//
// The mirror job carries the build tag mirror, shares harness_test.go with
// the suite, and runs, one test at a time, with its own command, not the
// suite's:
//
//	go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m
//
// with `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before it on
// nerdctl, and `CGO_ENABLED=0` in the environment on Docker, as the workflow
// sets it.
package e2e
