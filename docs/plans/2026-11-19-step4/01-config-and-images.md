# Step 4 plan, part 1: `archive`

> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints, its decisions and its "Interfaces between tasks" apply to every task here.

### Task 1: `archive`, from the configuration's check to the nodes' pull

Design: section 8, "The config file" ("Paths", "What else is rejected": an image archive that holds more than one image); section 9, "How the caller's image gets in" (`archive: PATH`, "Into the nodes"); README decision 2 (what an archive is) and Review Focus 3. Decision 1's exception: `images.NAME.archive` leaves `config.NotBuilt` in this task, because `images.Store` is already joined from end to end and `wire.go` reads nothing that would panic.

**Files:**
- Modify: `internal/config/validate.go` (the archive's check in `validate`, its comment at the top of `validate`; new `checkArchive`), `internal/config/notbuilt.go` (`images.NAME.archive` leaves the list)
- Create: `internal/images/archive.go` (`storeArchive`)
- Modify: `internal/images/local.go` (`storeLocal` ends with `storeArchive`), `internal/images/images.go` (`Store` takes `archive`; its comment)
- Test: `internal/config/validate_test.go` (new tests, the helpers `writeArchive`, `writeTar`, `writeFile` and `mkdir`, `TestPathsAreRelativeToTheFile`), `internal/config/notbuilt_test.go` (`TestNotBuilt`), new `internal/images/archive_test.go`, `internal/images/own_test.go` (`TestStoreRefusesWhatIsNotBuilt`), `internal/images/images_test.go` (new cluster test; `tags` moves out)

**Interfaces:**
- Consumes: `tarball.LoadManifest`, `tarball.ImageFromPath` (`github.com/google/go-containerregistry/pkg/v1/tarball`, already a dependency); `pushImage` and `pull` in `internal/images/images.go` as built.
- Produces: `func checkArchive(path string) (problem string)`
  The problem with the image archive at path, an absolute path, as a phrase
  that quotes no value; empty when the file is a tar that holds a
  manifest.json naming exactly one image the tool can read. A missing file
  gives "does not exist"; more than one image "holds more than one image";
  anything else "is not an image archive the tool can read".
- Produces: `func (c *Config) NotBuilt() []string`
  The keys of this configuration that this build cannot run yet, sorted;
  empty when it can run all of it. images.NAME.archive is no longer among
  them; images.NAME.as and the others stay until their tasks.
- Produces: `func storeArchive(ctx context.Context, c *cluster.Cluster, entry, file, what string) (config.ImageRef, error)`
  Pushes the one image of the archive file into the run's registry by digest,
  under the repository entry, and returns TestHost/ENTRY and the digest. what
  names the file in an error's text, such as "the archive". No tag is
  written. An error is a Condition with ClusterFailed, "images.ENTRY: ...".
- Produces: `func Store(ctx context.Context, c *cluster.Cluster, binary string, images map[string]config.Image, timeout time.Duration) (Stored, error)`
  Packages the tool's binary at the given path as the probe's image and has
  the control-plane node pull it; stores each entry of images, from the
  engine (local) or from its archive file, by digest, and has every node
  pull it. Nothing is stored under a tag. timeout bounds each node's pull.
  An error is a Condition with ClusterFailed. An entry with as is an
  InternalError.

- [ ] **Step 1: Write the failing tests**

  In `internal/config/validate_test.go`, four helpers and four tests. The archive's file name holds the planted value, so `rejected` proves that no text quotes the path. The file gains the imports `archive/tar`, `bytes`, `fmt`, `maps` and `slices`, and go-containerregistry's `name`, `v1`, `v1/random` and `v1/tarball`.

  ```go
  // writeArchive writes at path a tar such as docker save writes, holding n small images.
  func writeArchive(t *testing.T, path string, n int) {
  	t.Helper()
  	refs := map[name.Reference]v1.Image{}
  	for i := range n {
  		img, err := random.Image(64, 1)
  		if err != nil {
  			t.Fatal(err)
  		}
  		ref, err := name.ParseReference(fmt.Sprintf("rollproof.test/img:%d", i))
  		if err != nil {
  			t.Fatal(err)
  		}
  		refs[ref] = img
  	}
  	if err := tarball.MultiRefWriteToFile(path, refs); err != nil {
  		t.Fatal(err)
  	}
  }

  // writeTar writes at path a tar that holds the given files, in the order of their names.
  func writeTar(t *testing.T, path string, files map[string]string) {
  	t.Helper()
  	var b bytes.Buffer
  	tw := tar.NewWriter(&b)
  	for _, n := range slices.Sorted(maps.Keys(files)) {
  		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(files[n]))}); err != nil {
  			t.Fatal(err)
  		}
  		if _, err := tw.Write([]byte(files[n])); err != nil {
  			t.Fatal(err)
  		}
  	}
  	if err := tw.Close(); err != nil {
  		t.Fatal(err)
  	}
  	writeFile(t, path, b.String())
  }

  func writeFile(t *testing.T, path, text string) {
  	t.Helper()
  	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
  		t.Fatal(err)
  	}
  }

  func mkdir(t *testing.T, path string) {
  	t.Helper()
  	if err := os.Mkdir(path, 0o755); err != nil {
  		t.Fatal(err)
  	}
  }

  // archiveConfig is base with one image entry whose archive is the file S3CRET-VALUE.tar.
  func archiveConfig(t *testing.T) string {
  	return variant(t, base, "deploy:\n", "images:\n  orders:\n    archive: S3CRET-VALUE.tar\ndeploy:\n")
  }

  // Review Focus 3.
  func TestArchiveThatIsNotAnImage(t *testing.T) {
  	for _, tc := range []struct {
  		name string
  		make func(t *testing.T, path string)
  	}{
  		{"a folder", func(t *testing.T, p string) { mkdir(t, p) }},
  		{"an empty file", func(t *testing.T, p string) { writeFile(t, p, "") }},
  		{"not a tar", func(t *testing.T, p string) { writeFile(t, p, "not a tar S3CRET-VALUE\n") }},
  		{"an OCI layout with no manifest.json", func(t *testing.T, p string) {
  			writeTar(t, p, map[string]string{"oci-layout": `{"imageLayoutVersion":"1.0.0"}`, "index.json": `{"schemaVersion":2,"manifests":[]}`})
  		}},
  		{"a manifest.json that lists no image", func(t *testing.T, p string) {
  			writeTar(t, p, map[string]string{"manifest.json": "[]"})
  		}},
  		{"a manifest.json whose config is not in the tar", func(t *testing.T, p string) {
  			writeTar(t, p, map[string]string{"manifest.json": `[{"Config":"blobs/sha256/absent","RepoTags":[],"Layers":[]}]`})
  		}},
  	} {
  		t.Run(tc.name, func(t *testing.T) {
  			dir := t.TempDir()
  			tc.make(t, filepath.Join(dir, "S3CRET-VALUE.tar"))
  			_, err := loadWith(t, dir, archiveConfig(t), noEnv)
  			rejected(t, err, "images.orders.archive (line ")
  			rejected(t, err, "): is not an image archive the tool can read")
  		})
  	}
  }

  func TestArchiveWithTwoImages(t *testing.T) {
  	dir := t.TempDir()
  	writeArchive(t, filepath.Join(dir, "S3CRET-VALUE.tar"), 2)
  	_, err := loadWith(t, dir, archiveConfig(t), noEnv)
  	rejected(t, err, "images.orders.archive (line ")
  	rejected(t, err, "): holds more than one image")
  }

  func TestArchiveThatDoesNotExist(t *testing.T) {
  	_, err := loadWith(t, t.TempDir(), archiveConfig(t), noEnv)
  	rejected(t, err, "images.orders.archive (line ")
  	rejected(t, err, "): does not exist")
  }

  func TestArchiveWithOneImage(t *testing.T) {
  	dir, real := project(t)
  	writeArchive(t, filepath.Join(dir, "S3CRET-VALUE.tar"), 1)
  	c, err := loadWith(t, dir, archiveConfig(t), noEnv)
  	if err != nil {
  		t.Fatal(err)
  	}
  	if got, want := c.Images["orders"].Archive, filepath.Join(real, "S3CRET-VALUE.tar"); got != want {
  		t.Errorf("archive %q, want %q", got, want)
  	}
  }
  ```

  Change `TestPathsAreRelativeToTheFile`: `project(t, "chart", "images")`, then `writeArchive(t, filepath.Join(dir, "images", "orders.tar"), 1)` before the load; drop the comment line "Task 22's later plan reads the archive; this build only makes its path absolute."

  In `internal/config/notbuilt_test.go`, `TestNotBuilt` loads every row with `loadWith(t, dir, …, noEnv)` from one `dir := t.TempDir()` that holds `writeArchive(t, filepath.Join(dir, "orders.tar"), 1)`, so that the row "an image from an archive" loads; that row now wants `nil`. Add the row "an image from an archive with as": `images:\n  orders:\n    archive: orders.tar\n    as: registry.example/team/orders\n`, wanting `[]string{"images.orders.as"}`.

  New `internal/images/archive_test.go` (`//go:build linux`), against an in-process registry (`httptest.NewServer(registry.New())` from `github.com/google/go-containerregistry/pkg/registry`), with `c := &cluster.Cluster{PushRegistry: strings.TrimPrefix(srv.URL, "http://")}`:

  ```go
  //go:build linux

  package images

  // imports: encoding/json, net/http, net/http/httptest, os, path/filepath, strconv, strings, testing;
  // go-cmp's cmp; go-containerregistry's name, v1, random, registry, remote and tarball;
  // rollproof/internal/cluster, rollproof/internal/config and rollproof/internal/verdict.

  // archiveRegistry starts a registry in the process and returns a Cluster whose run's registry it is.
  func archiveRegistry(t *testing.T) *cluster.Cluster {
  	t.Helper()
  	srv := httptest.NewServer(registry.New())
  	t.Cleanup(srv.Close)
  	return &cluster.Cluster{PushRegistry: strings.TrimPrefix(srv.URL, "http://")}
  }

  // archiveOf writes n random images to a new archive and returns its path.
  func archiveOf(t *testing.T, n int) string {
  	t.Helper()
  	refs := map[name.Reference]v1.Image{}
  	for i := range n {
  		img, err := random.Image(64, 1)
  		if err != nil {
  			t.Fatal(err)
  		}
  		ref, err := name.ParseReference("rollproof.test/img:" + strconv.Itoa(i))
  		if err != nil {
  			t.Fatal(err)
  		}
  		refs[ref] = img
  	}
  	file := filepath.Join(t.TempDir(), "orders.tar")
  	if err := tarball.MultiRefWriteToFile(file, refs); err != nil {
  		t.Fatal(err)
  	}
  	return file
  }

  func TestStoreArchivePushesByDigest(t *testing.T) {
  	c := archiveRegistry(t)
  	file := archiveOf(t, 1)
  	img, err := tarball.ImageFromPath(file, nil)
  	if err != nil {
  		t.Fatal(err)
  	}
  	digest, err := img.Digest()
  	if err != nil {
  		t.Fatal(err)
  	}

  	ref, err := storeArchive(t.Context(), c, "orders", file, "the archive")
  	if err != nil {
  		t.Fatal(err)
  	}
  	want := config.ImageRef{Repository: "rollproof.test/orders", Digest: digest.String()}
  	if d := cmp.Diff(want, ref); d != "" {
  		t.Errorf("stored as (-want +got):\n%s", d)
  	}
  	pushed, err := name.NewDigest(c.PushRegistry+"/orders@"+digest.String(), name.Insecure)
  	if err != nil {
  		t.Fatal(err)
  	}
  	if _, err := remote.Head(pushed); err != nil {
  		t.Errorf("the run's registry does not hold %s: %v", pushed, err)
  	}
  	if got := tags(t, c.PushRegistry, "orders"); len(got) != 0 {
  		t.Errorf("the run's registry has tags %v for orders", got)
  	}
  }

  func TestStoreArchiveFailures(t *testing.T) {
  	c := archiveRegistry(t)
  	notTar := filepath.Join(t.TempDir(), "orders.tar")
  	if err := os.WriteFile(notTar, []byte("not a tar\n"), 0o644); err != nil {
  		t.Fatal(err)
  	}
  	for _, tc := range []struct {
  		name, file, text string
  		exact            bool
  	}{
  		{"two images", archiveOf(t, 2), "images.orders: the archive holds more than one image", true},
  		{"not a tar", notTar, "images.orders: the archive cannot be read: ", false},
  	} {
  		t.Run(tc.name, func(t *testing.T) {
  			_, err := storeArchive(t.Context(), c, "orders", tc.file, "the archive")
  			cond := verdict.AsCondition(err)
  			if err == nil || cond.Code != verdict.ClusterFailed {
  				t.Fatalf("storeArchive: %v, want cluster-failed", err)
  			}
  			if tc.exact && cond.Text != tc.text || !tc.exact && !strings.HasPrefix(cond.Text, tc.text) {
  				t.Errorf("text %q, want %q", cond.Text, tc.text)
  			}
  		})
  	}
  }
  ```

  `tags` is the helper of `images_test.go`, which carries the tag `cluster`. Move it into `archive_test.go`, which both builds see, and delete it from `images_test.go` with the imports `encoding/json` and `net/http`, which only it used there.

  In `internal/images/own_test.go`, `TestStoreRefusesWhatIsNotBuilt` keeps only the entry with `as`; the entry `{Archive: "/tmp/orders.tar"}` goes.

  In `internal/images/images_test.go` (tag `cluster`), the archive is the engine's own format (decision 2):

  ```go
  func TestStoreArchiveIntoEveryNode(t *testing.T) {
  	ctx := context.Background()
  	c := testCluster(t)
  	dir, bin := tinyProgram(t)
  	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\nCOPY app /app\nENTRYPOINT [\"/app\"]\n"), 0o644); err != nil {
  		t.Fatal(err)
  	}
  	if out, err := exec.Command(c.Engine.Binary, "build", "-t", "rollproof-test-img:archive", dir).CombinedOutput(); err != nil {
  		t.Fatalf("build the image: %v\n%s", err, out)
  	}
  	t.Cleanup(func() {
  		if out, err := exec.Command(c.Engine.Binary, "rmi", "rollproof-test-img:archive").CombinedOutput(); err != nil {
  			t.Errorf("remove the local image rollproof-test-img:archive: %v\n%s", err, out)
  		}
  	})
  	file := filepath.Join(t.TempDir(), "app.tar")
  	if out, err := exec.Command(c.Engine.Binary, "save", "-o", file, "rollproof-test-img:archive").CombinedOutput(); err != nil {
  		t.Fatalf("save the image: %v\n%s", err, out)
  	}

  	s, err := Store(ctx, c, bin, map[string]config.Image{"app": {Archive: file}}, 2*time.Minute)
  	if err != nil {
  		t.Fatalf("Store: %v", err)
  	}
  	app := s.Images["app"]
  	if app.Repository != "rollproof.test/app" || !strings.HasPrefix(app.Digest, "sha256:") {
  		t.Errorf("app stored as %+v", app)
  	}
  	for _, n := range c.Nodes() {
  		if out, err := c.Exec(ctx, n, "crictl", "inspecti", app.Ref()); err != nil {
  			t.Errorf("%s does not hold %s: %v\n%s", n, app.Ref(), err, out)
  		}
  	}
  	if got := tags(t, c.PushRegistry, "app"); len(got) != 0 {
  		t.Errorf("the run's registry has tags %v for app", got)
  	}
  }
  ```

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/config/ -run 'TestArchive|TestNotBuilt|TestPathsAreRelativeToTheFile' -v` / Expected: FAIL: the three archive tests that must reject (`TestArchiveThatIsNotAnImage`, `TestArchiveWithTwoImages`, `TestArchiveThatDoesNotExist`) load the file, and `TestNotBuilt`'s archive row gives `images.orders.archive` (`TestArchiveWithOneImage` and `TestPathsAreRelativeToTheFile` pass already).
  Run: `go test ./internal/images/ -run 'TestStoreArchive|TestStoreRefusesWhatIsNotBuilt' -v` / Expected: FAIL to build: `storeArchive` is not defined.

- [ ] **Step 3: Implement `checkArchive` in `internal/config/validate.go` and `storeArchive` in `internal/images/archive.go`, and change `Store` and `storeLocal`**, with the interface comments from the Interfaces block.
  - `checkArchive`: `os.Stat` (a missing file: "does not exist"; a folder or any other error: "is not an image archive the tool can read"); then `tarball.LoadManifest`; zero descriptors, or an error: "is not an image archive the tool can read"; more than one: "holds more than one image"; then `tarball.ImageFromPath(path, nil)` must succeed. `validate` calls it in the loop over `images`, after the check "exactly one of local and archive", for an entry with `archive`, and rejects with `x.reject("images."+name+".archive", problem)`. Replace the comment above `validate` that leaves the check to a later plan with one sentence: an image archive is read when the file is loaded, so that both of its rejections come before a cluster exists.
  - `storeArchive`: the body of `local.go` from `tarball.LoadManifest` to the push, with `what` in place of "the engine's archive of " + local, and a `len(m) == 0` case beside `len(m) > 1` ("holds no image"). `storeLocal` saves as now and then returns `storeArchive(ctx, c, entry, file, "the engine's archive of "+local)`.
  - `NotBuilt` (`internal/config/notbuilt.go`): drop the test of `im.Archive`; its comment's example stays.
  - `Store`: the refusal loop refuses only `as`, with the text `"images." + n + ": as is not built yet"`; the store loop calls `storeArchive(ctx, c, n, im.Archive, "the archive")` for an entry with `Archive` and `storeLocal` otherwise, then has every node pull the result as now.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/config/ ./internal/images/ -v` / Expected: PASS.
  Run: `go vet ./...`, then `go vet -tags cluster ./...` / Expected: no output from either.
  Run: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 ./internal/images/ -run 'TestStoreArchiveIntoEveryNode|TestStoreOwnAndLocal' -v` / Expected: PASS.

- [ ] **Step 5: Commit**
  `git add internal/config/validate.go internal/config/validate_test.go internal/config/notbuilt.go internal/config/notbuilt_test.go internal/images/archive.go internal/images/archive_test.go internal/images/local.go internal/images/images.go internal/images/own_test.go internal/images/images_test.go` and `git commit -m "Read an image archive when the configuration is loaded, store its one image by digest in the run's registry and every node, and let a run use it"`
