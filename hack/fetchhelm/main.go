// Command fetchhelm puts Helm 3.22.1 at .tools/helm-3/helm and Helm 4.3.2 at
// .tools/helm-4/helm, for the machine's own CPU architecture, and kubectl
// 1.37.0 at .tools/kubectl, for linux/amd64 only: on another architecture it
// says that it skipped kubectl, which the e2e suite needs, and still exits 0.
// Each download is checked against a SHA-256 written in this program; a
// mismatch leaves no file and exits 1. A binary that is already there with the
// right checksum is not fetched again.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type key struct{ version, arch string }

// checksums holds the SHA-256 of each release archive on get.helm.sh.
var checksums = map[key]string{
	{"3.22.1", "amd64"}: "896cf63684a42a723d61c980369d6e588330927deb13676ca0d85006dd6da0ea",
	{"3.22.1", "arm64"}: "4c29e286b279d170fa048ddfc3e4e83dcd069ca9c52102766ba672d7149e694d",
	{"4.3.2", "amd64"}:  "d1c0278d46beab79582d7e50f5b3dc206944ba8e577cde618513fd07daa53983",
	{"4.3.2", "arm64"}:  "df31cb61680ef728f82c770e5280dd565617bee6b039d37e85c677288be4bbb0",
}

// kubectlVersion is the kubectl that the fixtures run, and kubectlSum the
// SHA-256 of its linux/amd64 binary, as dl.k8s.io publishes it beside the
// binary.
const (
	kubectlVersion = "1.37.0"
	kubectlSum     = "6129359f4e1f3848a5572ccb0b26cf28b8ca08cef38c95a765b2f64a2c961a2f"
)

// versions lists the Helm versions to fetch, with the folder name of each.
var versions = []struct{ version, major string }{
	{"3.22.1", "helm-3"},
	{"4.3.2", "helm-4"},
}

func main() {
	dir := flag.String("dir", ".tools", "folder that receives the binaries")
	print := flag.Bool("print-checksums", false, "print the checksum of each archive and the published one, then exit")
	flag.Parse()
	const base = "https://get.helm.sh"
	const kubectlBase = "https://dl.k8s.io"
	var err error
	if *print {
		err = printChecksums(base, kubectlBase)
	} else {
		err = fetch(base, checksums, *dir)
		if err == nil && runtime.GOARCH == "amd64" {
			err = fetchKubectl(kubectlBase, runtime.GOARCH, kubectlSum, *dir)
		} else if err == nil {
			fmt.Printf("kubectl %s: skipped: it is fetched for linux/amd64 only, and the e2e suite needs it\n", kubectlVersion)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// fetch puts each pinned Helm binary at dir/helm-M/helm. A binary whose record
// file helm.sha256 matches the table and the file on disk is left alone.
func fetch(base string, sums map[key]string, dir string) error {
	for _, v := range versions {
		if err := fetchOne(base, sums, dir, v.version, v.major); err != nil {
			return err
		}
	}
	return nil
}

func fetchOne(base string, sums map[key]string, dir, version, major string) error {
	arch := runtime.GOARCH
	want := sums[key{version, arch}]
	if want == "" {
		return fmt.Errorf("helm %s: no checksum for architecture %s", version, arch)
	}
	bin := filepath.Join(dir, major, "helm")
	record := filepath.Join(dir, major, "helm.sha256")
	if present(bin, record, want) {
		fmt.Printf("helm %s: already present\n", version)
		return nil
	}

	resp, err := http.Get(fmt.Sprintf("%s/helm-v%s-linux-%s.tar.gz", base, version, arch))
	if err != nil {
		return fmt.Errorf("helm %s: %w", version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("helm %s: download returned status %d", version, resp.StatusCode)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "helm-archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		return fmt.Errorf("helm %s: %w", version, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("helm %s: checksum mismatch: got %s, want %s", version, got, want)
	}

	if err := os.MkdirAll(filepath.Join(dir, major), 0o755); err != nil {
		return err
	}
	binSum, err := unpack(tmp.Name(), "linux-"+arch+"/helm", bin)
	if err != nil {
		return fmt.Errorf("helm %s: %w", version, err)
	}
	if err := os.WriteFile(record, []byte(want+" "+binSum+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("helm %s: fetched to %s\n", version, bin)
	return nil
}

// fetchKubectl puts kubectl 1.37.0 for linux/arch at dir/kubectl with mode 0755, from
// base/release/v1.37.0/bin/linux/ARCH/kubectl, checked against sum, the
// SHA-256 of the binary. A binary whose record dir/kubectl.sha256 names sum
// and matches the file is not fetched again. An arch other than amd64, or a
// mismatch, is an error that names "kubectl 1.37.0" and leaves no file.
func fetchKubectl(base, arch, sum, dir string) error {
	what := "kubectl " + kubectlVersion
	if arch != "amd64" {
		return fmt.Errorf("%s: no checksum for architecture %s", what, arch)
	}
	bin := filepath.Join(dir, "kubectl")
	// The archive and the binary are one file here, so the record names the sum twice.
	record := filepath.Join(dir, "kubectl.sha256")
	if present(bin, record, sum) {
		fmt.Printf("%s: already present\n", what)
		return nil
	}

	resp, err := http.Get(fmt.Sprintf("%s/release/v%s/bin/linux/%s/kubectl", base, kubectlVersion, arch))
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: download returned status %d", what, resp.StatusCode)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	tmp, err := os.CreateTemp(dir, "kubectl-download-*")
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return fmt.Errorf("%s: checksum mismatch: got %s, want %s", what, got, sum)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if err := os.Rename(tmp.Name(), bin); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if err := os.WriteFile(record, []byte(sum+" "+sum+"\n"), 0o644); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	fmt.Printf("%s: fetched to %s\n", what, bin)
	return nil
}

// present reports whether record names archiveSum and the binary has the
// SHA-256 that record names.
func present(bin, record, archiveSum string) bool {
	rec, err := os.ReadFile(record)
	if err != nil {
		return false
	}
	f := strings.Fields(string(rec))
	if len(f) != 2 || f[0] != archiveSum {
		return false
	}
	b, err := os.ReadFile(bin)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]) == f[1]
}

// unpack copies the entry name of the gzipped tar at archive to dst with mode
// 0755, through dst+".tmp", and returns the SHA-256 of the copied bytes.
func unpack(archive, name, dst string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return "", fmt.Errorf("archive has no entry %s", name)
		}
		if err != nil {
			return "", err
		}
		if hdr.Name != name || hdr.Typeflag != tar.TypeReg {
			continue
		}
		tmp := dst + ".tmp"
		out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return "", err
		}
		h := sha256.New()
		_, err = io.Copy(io.MultiWriter(out, h), tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Chmod(tmp, 0o755)
		}
		if err == nil {
			err = os.Rename(tmp, dst)
		}
		if err != nil {
			os.Remove(tmp)
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
}

// printChecksums prints, for each Helm version and architecture, the SHA-256 of
// the archive and the checksum that the release host publishes beside it, and
// the same for kubectl's linux/amd64 binary from kubectlBase.
func printChecksums(base, kubectlBase string) error {
	for _, v := range versions {
		for _, arch := range []string{"amd64", "arm64"} {
			url := fmt.Sprintf("%s/helm-v%s-linux-%s.tar.gz", base, v.version, arch)
			sum, err := hashURL(url)
			if err != nil {
				return err
			}
			fmt.Printf("%s %s %s\n", v.version, arch, sum)
			text, err := getText(url + ".sha256sum")
			if err != nil {
				return err
			}
			fmt.Printf("  published: %s\n", text)
		}
	}
	url := fmt.Sprintf("%s/release/v%s/bin/linux/amd64/kubectl", kubectlBase, kubectlVersion)
	sum, err := hashURL(url)
	if err != nil {
		return err
	}
	fmt.Printf("kubectl %s amd64 %s\n", kubectlVersion, sum)
	text, err := getText(url + ".sha256")
	if err != nil {
		return err
	}
	fmt.Printf("  published: %s\n", text)
	return nil
}

func hashURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(h, resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func getText(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b)), err
}
