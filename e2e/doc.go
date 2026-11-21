// Package e2e runs the real rollproof binary against the fixture service,
// one fixture of the design's section 12 at a time, under Helm 3 and Helm 4.
//
// The fixture rows carry the build tag e2e. The tests of config_test.go carry
// none: they check the run description, the window marks and the actions with
// no cluster, so `go test ./...` runs them, and `go test -tags e2e ./e2e/`
// runs them too.
//
// The suite runs every row of section 12 under both Helm majors and takes
// about two and a half hours. It needs Linux, a container engine that kind supports
// (docker, nerdctl or podman), the Helm binaries and kubectl under .tools/,
// which `go run ./hack/fetchhelm` puts there (kubectl for linux/amd64 only),
// and busybox's archive at .tools/busybox-1.37.0.tar, which the first run of
// the suite pulls and saves.
// Each run creates the cluster rollproof-e2e and deletes it at its end, so
// the suite runs one test at a time:
//
//	go test -tags e2e -p 1 ./e2e/ -v -timeout 360m
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
