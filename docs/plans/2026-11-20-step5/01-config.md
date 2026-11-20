# Task 1: the forms of `host`, `as` and `mirror`, and the `ca`

Read sections 8 ("Types", "What else is rejected", "Secrets") and 9 ("Registries: credentials, mirrors and certificate authorities"), and section 14, point 31, before this task.

### Task 1: Every rejection of section 8 for `registries` and `as`

`registries` and `images.*.as` stay on `NotBuilt` in this task (decision 1): the checks run when the file loads, and `run` still stops such a file after them.

**Files:**
- Create: `internal/config/registries.go`
- Modify: `internal/config/validate.go` (the images loop and the registries loop at the end of `validate`)
- Create: `internal/config/registries_test.go`
- Create: `internal/config/testdata/corp-ca.pem`
- Modify: `internal/config/testdata/full.yaml` (`ca: corp-ca.pem`), `internal/config/load_test.go` (`TestLoadFull`'s `CA`), `internal/config/validate_test.go` (`TestPathsAreRelativeToTheFile` writes the `ca` it names)

**Interfaces:**
- Consumes: `index.reject(path, problem string) error` and `Load` as they are.
- Produces: `func (c *Config) AsHosts() []string`
  The registry hosts that the entries of images name in as, each once,
  sorted; nil when no entry has as. A host is the part of as before its
  first slash, as written.

- [ ] **Step 1: Write the failing tests**

In `registries_test.go`, a helper `writeCA(t *testing.T, path string)` that writes a self-signed ECDSA P-256 certificate in PEM, mode 0644, made with `crypto/x509`; `writeKeyPEM` (a PEM `EC PRIVATE KEY` block alone) and `writeDER` (the certificate's DER bytes) for the `ca` cases. `testdata/corp-ca.pem` is one certificate written once by `writeCA` and checked in; nothing checks its dates.

The planted value is lower case here, so that only the upper-case case trips "must be lower case"; a helper `rejectedQuiet(t, err, path, problem)` calls `rejected(t, err, path)`, checks that the text holds `problem`, and checks that it holds neither `S3CRET-VALUE` nor `s3cret-value`.

```go
func TestRegistriesRejects(t *testing.T) {
	entry := func(lines string) string { return variant(t, base, "deploy:\n", "registries:\n"+lines+"deploy:\n") }
	as := func(v string) string {
		return variant(t, base, "deploy:\n", "images:\n  orders:\n    local: orders:candidate\n    as: \""+v+"\"\ndeploy:\n")
	}
	const own = "names the entry's own host"
	const tool = "rollproof.test is the tool's own host inside the run"
	const hub = "write docker.io for Docker Hub"
	const noHost = "a name with neither a dot nor a port is not a registry host"
	for _, tc := range []struct{ name, text, path, problem string }{
		{"a host with a scheme", entry("  - host: \"https://s3cret-value.example\"\n"), "registries[0].host", "must not carry a scheme"},
		{"a host with a path", entry("  - host: \"registry.example/s3cret-value\"\n"), "registries[0].host", "must not carry a path"},
		{"a host with a user", entry("  - host: \"s3cret-value@registry.example\"\n"), "registries[0].host", "must not carry a user name or a password"},
		{"a host in upper case", entry("  - host: \"Registry.S3CRET-VALUE.example\"\n"), "registries[0].host", "must be lower case"},
		{"a host with neither a dot nor a port", entry("  - host: \"s3cret-value\"\n"), "registries[0].host", noHost},
		{"an IPv6 host without brackets", entry("  - host: \"fd00::5\"\n"), "registries[0].host", "an IPv6 address must be written in brackets"},
		{"a port out of range", entry("  - host: \"s3cret-value.example:99999\"\n"), "registries[0].host", "the port must be 1 to 65535"},
		{"an empty label", entry("  - host: \"s3cret-value..example\"\n"), "registries[0].host", "is not a registry host as an image's name writes it"},
		{"a dot at the end", entry("  - host: \"s3cret-value.example.\"\n"), "registries[0].host", "is not a registry host as an image's name writes it"},
		{"index.docker.io as a host", entry("  - host: \"index.docker.io\"\n    mirror: \"s3cret-value.example\"\n"), "registries[0].host", hub},
		{"registry-1.docker.io as a host", entry("  - host: \"registry-1.docker.io\"\n    mirror: \"s3cret-value.example\"\n"), "registries[0].host", hub},
		{"rollproof.test as a host", entry("  - host: \"rollproof.test\"\n    mirror: \"s3cret-value.example\"\n"), "registries[0].host", tool},
		{"a username with no password", entry("  - host: \"registry.example\"\n    username: s3cret-value\n"), "registries[0]", "username and password come together or not at all"},
		{"a password with no username", entry("  - host: \"registry.example\"\n    password: ${env.P}\n"), "registries[0]", "username and password come together or not at all"},
		{"a mirror with a scheme", entry("  - host: \"registry.example\"\n    mirror: \"https://mirror.example/s3cret-value\"\n"), "registries[0].mirror", "must not carry a scheme; HTTPS is implied"},
		{"a mirror with a user and a password", entry("  - host: \"registry.example\"\n    mirror: \"u:s3cret-value@mirror.example\"\n"), "registries[0].mirror", "must not carry a user name or a password"},
		{"a mirror with a query", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/x?s3cret-value\"\n"), "registries[0].mirror", "must not carry a query"},
		{"a mirror with a fragment", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/x#s3cret-value\"\n"), "registries[0].mirror", "must not carry a fragment"},
		{"a mirror's host in upper case", entry("  - host: \"registry.example\"\n    mirror: \"Mirror.example/s3cret-value\"\n"), "registries[0].mirror", "must be lower case"},
		{"a mirror's host with neither a dot nor a port", entry("  - host: \"registry.example\"\n    mirror: \"mirror/s3cret-value\"\n"), "registries[0].mirror", noHost},
		{"a mirror's path with a .. segment", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/a/../s3cret-value\"\n"), "registries[0].mirror", "the path must not have a . or .. segment"},
		{"a mirror's path with an escaped character", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/s3cret%2Dvalue\"\n"), "registries[0].mirror", "the path may hold only letters, digits, -, ., _ and ~"},
		{"a mirror's path with a / at its end", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/s3cret-value/\"\n"), "registries[0].mirror", "the path must have no empty segment and no / at its end"},
		{"a mirror's path with an empty segment", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example//s3cret-value\"\n"), "registries[0].mirror", "the path must have no empty segment and no / at its end"},
		{"a mirror on its own host", entry("  - host: \"registry.example\"\n    mirror: \"registry.example/s3cret-value\"\n"), "registries[0].mirror", own},
		{"a mirror on its own host with :443", entry("  - host: \"registry.example\"\n    mirror: \"registry.example:443/s3cret-value\"\n"), "registries[0].mirror", own},
		{"a host with :443 and its mirror without", entry("  - host: \"registry.example:443\"\n    mirror: \"registry.example/s3cret-value\"\n"), "registries[0].mirror", own},
		{"a mirror on rollproof.test", entry("  - host: \"registry.example\"\n    mirror: \"rollproof.test/s3cret-value\"\n"), "registries[0].mirror", tool},
		{"an as with a tag", as("registry.example/team/orders:s3cret-value"), "images.orders.as", "must not carry a tag"},
		{"an as with a digest", as("registry.example/team/s3cret-value@sha256:" + strings.Repeat("a", 64)), "images.orders.as", "must not carry a digest"},
		{"an as with no host", as("team/s3cret-value"), "images.orders.as", noHost},
		{"an as with no path", as("s3cret-value.example"), "images.orders.as", "must name a repository with its host written out"},
		{"an as in upper case", as("registry.example/Team/S3CRET-VALUE"), "images.orders.as", "must be lower case"},
		{"an as whose path is not a repository's", as("registry.example/team/-s3cret-value"), "images.orders.as", "the path is not a repository path as an image's name writes it"},
		{"an as on docker.io with one segment", as("docker.io/s3cret-value"), "images.orders.as", "on docker.io the path needs two segments or more; write docker.io/library/NAME"},
		{"an as on index.docker.io", as("index.docker.io/library/s3cret-value"), "images.orders.as", hub},
		{"an as on registry-1.docker.io", as("registry-1.docker.io/library/s3cret-value"), "images.orders.as", hub},
		{"an as on rollproof.test", as("rollproof.test/s3cret-value"), "images.orders.as", tool},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWith(t, t.TempDir(), tc.text, envOf(map[string]string{"P": planted}))
			rejectedQuiet(t, err, tc.path, tc.problem)
		})
	}
}
```

A second test gives the `ca` cases, each in a project folder from `project(t, "certs")`:

```go
func TestCARejects(t *testing.T) {
	// For each case, a file or folder at certs/ca.pem, then the entry
	//   - host: "registry.example"
	//     ca: certs/ca.pem
	// rejected at registries[0].ca with the problem:
	//   nothing at the path                 -> "does not exist"
	//   a folder                            -> "is not a file the tool can read"
	//   an empty file                       -> "holds no certificate in PEM form"
	//   writeKeyPEM, a private key alone    -> "holds no certificate in PEM form"
	//   writeDER, a certificate in DER      -> "holds no certificate in PEM form"
}
```

```go
func TestRegistriesAccepts(t *testing.T) {
	// Each loads with no error; the planted CA is writeCA's, at certs/ca.pem.
	hosts := []string{"registry.example", "registry.example:5000", "10.1.2.3", "10.1.2.3:5000",
		"localhost", "localhost:5000", "[fd00::5]", "[fd00::5]:5000", "myregistry:5000", "docker.io",
		"index.docker.io:443", "rollproof.test:5000"} // the last two: decision 13
	mirrors := []string{"mirror.example", "mirror.example/registry-example",
		"cache.example/v2/proxy-project", "mirror.example:8443/A_b.c~d-e", "mirror.example/cache/v2"}
	ases := []string{"registry.example/team/orders", "registry.example:5000/orders",
		"docker.io/library/orders", "docker.io/team/orders", "localhost:5000/a/b", "[fd00::5]:5000/a",
		"registry.example/a__b/c.d/e-f"}
	// Also accepted, as two entries each:
	//   registry.example and registry.example:443 (compared as written);
	//   a.example with mirror b.example/x, beside the entry b.example (a mirror on another
	//   entry's host takes nothing from it, section 9);
	//   an entry with a host alone, and one with a ca of two certificates and text around them.
}

func TestAsHosts(t *testing.T) {
	// images: a with as registry.example/team/a, b with as registry.example/team/b,
	// c with as docker.io/library/c, d with no as -> []string{"docker.io", "registry.example"};
	// no as at all -> nil.
}
```

In `TestLoadFull`, the expected `CA` becomes `filepath.Join(filepath.Dir(real), "corp-ca.pem")`; in `TestPathsAreRelativeToTheFile`, `writeCA(t, filepath.Join(dir, "certs", "corp-ca.pem"))` before the load.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestRegistriesRejects|TestCARejects|TestRegistriesAccepts|TestAsHosts|TestLoadFull|TestPathsAreRelativeToTheFile' -v`
Expected: FAIL: the form cases load with no error, `AsHosts` is not defined, and `TestLoadFull` reads `corp-ca.pem` at the old path.

- [ ] **Step 3: Implement the checks in `internal/config/registries.go`, and `(*Config).AsHosts` with its interface comment from the Interfaces block**

Four unexported functions, each returning a phrase that quotes no value, "" when the value is sound: `hostProblem(host string) string`, `asProblem(as string) string`, `mirrorProblem(mirror, host string) string` and `caProblem(path string) string`; and `validateRegistries(c *Config, x *index) error`. The phrases are the test's, word for word.

`hostProblem` checks, in this order: `://`; `/`; `@`; any upper-case letter; more than one `:` with no `[`; the grammar of decision 15 (a DNS name, an IPv4 address or a bracketed IPv6 address that `net.ParseIP` reads, then an optional `:PORT`); the port's range; then neither a dot, nor a port, nor `localhost`, nor brackets. `index.docker.io`, `registry-1.docker.io` and `rollproof.test` are compared as written (decision 13), by the callers that reject them: `host` and the host of an `as`, and `rollproof.test` also as a mirror's host.

`asProblem` checks upper case on the whole value first, then `@` (a digest), then that a `/` exists, then a `:` after the last `/` (a tag), then `hostProblem` of the part before the first `/`, then each path component against `^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`, then the three named hosts, then a path of one segment on `docker.io`.

`mirrorProblem` checks `://`, `@`, `?` and `#` on the whole value first, then splits at the first `/`: `hostProblem` of the host part, then each path segment (empty, `.` or `..`, a character outside `[A-Za-z0-9._~-]`), then `rollproof.test`, then the entry's own host, both sides with a trailing `:443` taken off.

`caProblem` follows `checkArchive`: `os.Stat`, a regular file, then `pem.Decode` in a loop until a `CERTIFICATE` block parses with `x509.ParseCertificate`.

In `validate`, the images loop calls `asProblem` on a non-empty `As` after "exactly one of local and archive", with the path `images.NAME.as`. `validateRegistries` replaces the registries loop at the end of `validate`: for each entry in order, `host` required, `hostProblem`, the named hosts, the same host as an earlier entry (as today), then `mirror` and `ca` when set. The check that `username` and `password` come together stays in `convert`, which runs before.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ -v` and `go vet ./...`
Expected: PASS, including `TestNotBuilt`, whose cases are unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/config/registries.go internal/config/registries_test.go internal/config/validate.go internal/config/validate_test.go internal/config/load_test.go internal/config/testdata/full.yaml internal/config/testdata/corp-ca.pem
git commit -m "Check the forms of host, as and mirror, and the ca, when the configuration loads"
```
