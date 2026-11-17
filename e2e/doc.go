// Package e2e runs the real rollproof binary against the fixture service,
// one fixture of the design's section 12 at a time, under Helm 3 and Helm 4.
//
// Its tests carry the build tag e2e. They need Linux, a container engine
// that kind supports (docker, nerdctl or podman), and the Helm binaries that
// `go run ./hack/fetchhelm` puts under .tools/. A run takes about 4 minutes.
// Each run creates the cluster rollproof-e2e and deletes it at its end, so
// the suite runs one test at a time:
//
//	go test -tags e2e -p 1 ./e2e/ -v -timeout 120m
package e2e
