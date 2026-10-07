// Command release cross-compiles localdns and packages release archives plus
// a checksums.txt file. It only uses the Go toolchain, so it runs the same way
// on Linux, macOS and Windows.
//
//	go run ./tools/release -version 0.1.0 -out dist
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const module = "github.com/devehab/locly-dns"

var defaultTargets = "linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64"

func main() {
	version := flag.String("version", "", "version to embed, e.g. 0.1.0 (required)")
	out := flag.String("out", "dist", "output directory")
	targets := flag.String("targets", defaultTargets, "comma-separated GOOS/GOARCH list")
	flag.Parse()
	if *version == "" {
		fmt.Fprintln(os.Stderr, "release: -version is required")
		os.Exit(2)
	}
	if err := run(strings.TrimPrefix(*version, "v"), *out, strings.Split(*targets, ",")); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(version, out string, targets []string) error {
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o750); err != nil {
		return err
	}
	commit := gitCommit()
	var archives []string
	for _, t := range targets {
		goos, goarch, ok := strings.Cut(strings.TrimSpace(t), "/")
		if !ok {
			return fmt.Errorf("bad target %q", t)
		}
		name, err := buildTarget(version, commit, out, goos, goarch)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", goos, goarch, err)
		}
		archives = append(archives, name)
		fmt.Println("built", filepath.Join(out, name))
	}
	return writeChecksums(out, archives)
}

func gitCommit() string {
	b, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func buildTarget(version, commit, out, goos, goarch string) (string, error) {
	tmp, err := os.MkdirTemp("", "localdns-release-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	bin := "localdns"
	if goos == "windows" {
		bin += ".exe"
	}
	ldflags := fmt.Sprintf("-s -w -X %s/internal/version.Version=%s -X %s/internal/version.Commit=%s",
		module, version, module, commit)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", filepath.Join(tmp, bin), "./cmd/localdns")
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	files := map[string]string{bin: filepath.Join(tmp, bin)}
	for _, extra := range []string{"README.md", "LICENSE"} {
		if _, err := os.Stat(extra); err == nil {
			files[extra] = extra
		}
	}
	base := fmt.Sprintf("localdns_%s_%s", goos, goarch)
	if goos == "windows" {
		name := base + ".zip"
		return name, writeZip(filepath.Join(out, name), files)
	}
	name := base + ".tar.gz"
	return name, writeTarGz(filepath.Join(out, name), files)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeTarGz(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range sortedKeys(files) {
		data, err := os.ReadFile(files[name])
		if err != nil {
			return err
		}
		mode := int64(0o644)
		if name == "localdns" {
			mode = 0o755
		}
		hdr := &tar.Header{Name: name, Mode: mode, Size: int64(len(data)), ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Close()
}

func writeZip(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	zw := zip.NewWriter(f)
	for _, name := range sortedKeys(files) {
		data, err := os.ReadFile(files[name])
		if err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Unix(0, 0)})
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

func writeChecksums(out string, names []string) error {
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		f, err := os.Open(filepath.Join(out, name))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(h.Sum(nil)), name)
	}
	return os.WriteFile(filepath.Join(out, "checksums.txt"), []byte(b.String()), 0o644)
}
