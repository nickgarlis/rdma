package main

import (
	"archive/tar"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// mainlineURL is Ubuntu's archive of builds of upstream kernel releases.
const mainlineURL = "https://kernel.ubuntu.com/mainline"

// ubuntuKernel returns the kernel image and module directory of an Ubuntu
// mainline build of the given upstream version, such as "7.2.6",
// downloading and unpacking it into cache the first time.
func ubuntuKernel(version, cache string) (kernel, modules string, err error) {
	version = strings.TrimPrefix(version, "v")
	dir := filepath.Join(cache, "ubuntu-mainline", version)

	kernel = filepath.Join(dir, "vmlinuz")
	modules = filepath.Join(dir, "modules")
	done := filepath.Join(dir, ".complete")

	if _, err := os.Stat(done); err == nil {
		return kernel, modules, nil
	}

	// Start over if an earlier attempt was interrupted.
	if err := os.RemoveAll(dir); err != nil {
		return "", "", err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}

	base := fmt.Sprintf("%s/v%s/amd64", mainlineURL, version)
	sums, err := fetchChecksums(base + "/CHECKSUMS")
	if err != nil {
		return "", "", fmt.Errorf("mainline kernel %s: %w", version, err)
	}

	image, err := findPackage(sums, `^linux-image-unsigned-.*-generic_.*_amd64\.deb$`)
	if err != nil {
		return "", "", err
	}

	mods, err := findPackage(sums, `^linux-modules-.*-generic_.*_amd64\.deb$`)
	if err != nil {
		return "", "", err
	}

	for _, pkg := range []string{image, mods} {
		path := filepath.Join(dir, pkg)
		if err := download(base+"/"+pkg, path, sums[pkg]); err != nil {
			return "", "", err
		}

		err := unpackDeb(path, func(name string) string {
			if strings.HasPrefix(name, "boot/vmlinuz-") {
				return kernel
			}

			// [usr/]lib/modules/<release>/... becomes modules/...; older
			// packages do not use /usr.
			rest, ok := strings.CutPrefix(strings.TrimPrefix(name, "usr/"), "lib/modules/")
			if !ok {
				return ""
			}

			if _, rel, ok := strings.Cut(rest, "/"); ok {
				return filepath.Join(modules, rel)
			}

			return ""
		})
		if err != nil {
			return "", "", fmt.Errorf("unpack %s: %w", pkg, err)
		}

		if err := os.Remove(path); err != nil {
			return "", "", err
		}
	}

	for _, p := range []string{kernel, filepath.Join(modules, "modules.builtin")} {
		if _, err := os.Stat(p); err != nil {
			return "", "", fmt.Errorf("mainline kernel %s: unexpected package layout: %w", version, err)
		}
	}

	if err := os.WriteFile(done, nil, 0o644); err != nil {
		return "", "", err
	}

	return kernel, modules, nil
}

// fetchChecksums returns the SHA-256 checksums from a mainline CHECKSUMS
// file, by file name.
func fetchChecksums(url string) (map[string]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	sums := make(map[string]string)
	inSHA256 := false

	s := bufio.NewScanner(resp.Body)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "#") {
			inSHA256 = strings.Contains(line, "Checksums-Sha256")
			continue
		}

		if sum, name, ok := strings.Cut(line, "  "); ok && inSHA256 {
			sums[name] = sum
		}
	}

	if err := s.Err(); err != nil {
		return nil, err
	}

	if len(sums) == 0 {
		return nil, fmt.Errorf("no SHA-256 checksums in %s", url)
	}

	return sums, nil
}

func findPackage(sums map[string]string, pattern string) (string, error) {
	re := regexp.MustCompile(pattern)
	for name := range sums {
		if re.MatchString(name) {
			return name, nil
		}
	}

	return "", fmt.Errorf("no package matching %s", pattern)
}

// download fetches url into path and checks its SHA-256 checksum.
func download(url, path, sum string) error {
	log.Printf("downloading %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}

	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return fmt.Errorf("download %s: SHA-256 is %s, want %s", url, got, sum)
	}

	return f.Close()
}

// unpackDeb extracts the regular files of a .deb package whose data.tar is
// uncompressed, as in Ubuntu mainline builds. dest maps a file's path in the
// package, without a leading "./", to where it is written, or to "" to skip
// it.
func unpackDeb(path string, dest func(name string) string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := arMember(f, "data.tar")
	if err != nil {
		return err
	}

	tr := tar.NewReader(data)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		if h.Typeflag != tar.TypeReg {
			continue
		}

		out := dest(strings.TrimPrefix(h.Name, "./"))
		if out == "" {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}

		w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}

		if _, err := io.Copy(w, tr); err != nil {
			w.Close()
			return err
		}

		if err := w.Close(); err != nil {
			return err
		}
	}
}

// arMember returns a reader for the named member of an ar archive, the
// container format of .deb packages.
func arMember(r io.Reader, name string) (io.Reader, error) {
	br := bufio.NewReader(r)

	magic := make([]byte, 8)
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != "!<arch>\n" {
		return nil, errors.New("not an ar archive")
	}

	hdr := make([]byte, 60)
	for {
		if _, err := io.ReadFull(br, hdr); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("no %s in package", name)
			}

			return nil, err
		}

		member := strings.TrimSuffix(strings.TrimSpace(string(hdr[0:16])), "/")

		var size int64
		if _, err := fmt.Sscan(strings.TrimSpace(string(hdr[48:58])), &size); err != nil {
			return nil, fmt.Errorf("bad ar header: %w", err)
		}

		if member == name {
			return io.LimitReader(br, size), nil
		}

		if strings.HasPrefix(member, "data.tar.") {
			return nil, fmt.Errorf("unsupported compressed %s", member)
		}

		// Members are padded to an even size.
		if _, err := br.Discard(int(size + size%2)); err != nil {
			return nil, err
		}
	}
}
