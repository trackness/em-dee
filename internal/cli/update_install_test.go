package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Install-path tests.
//
// Spec: docs/superpowers/specs/2026-05-11-em-dee-design.md §12.6
// Plan: docs/superpowers/plans/2026-05-11-em-dee-implementation.md Task 6.4
//
// Strategy: an in-process httptest.Server serves a synthetic GitHub
// release (latest endpoint + asset URLs). Tests construct synthetic
// tar.gz archives in memory; the updater seam is a no-op that records
// the bytes it would have applied. No real network and no real
// executable swap.

// TestParseChecksums covers the goreleaser-produced format
// ("<hex>  <filename>") plus a couple of defensive cases.
func TestParseChecksums(t *testing.T) {
	in := []byte(strings.Join([]string{
		"aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899  em-dee_linux_amd64.tar.gz",
		"99887766554433221100ffeeddccbbaa99887766554433221100ffeeddccbbaa  em-dee_darwin_arm64.tar.gz",
		"",
		"   ", // pure whitespace
		"deadbeef *binary-mode-marker.bin",
		"malformed-line",
	}, "\n"))
	got := parseChecksums(in)
	want := map[string]string{
		"em-dee_linux_amd64.tar.gz":  "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
		"em-dee_darwin_arm64.tar.gz": "99887766554433221100ffeeddccbbaa99887766554433221100ffeeddccbbaa",
		"binary-mode-marker.bin":     "deadbeef",
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d entries, want %d: %+v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("checksum[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello, em-dee")
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	good := map[string]string{"file.tar.gz": hexSum}
	if err := verifyChecksum(good, "file.tar.gz", data); err != nil {
		t.Errorf("matching checksum should not error; got %v", err)
	}
	// Mismatch case — spec §12.6 mandates abort on mismatch.
	bad := map[string]string{"file.tar.gz": "0000000000000000000000000000000000000000000000000000000000000000"}
	if err := verifyChecksum(bad, "file.tar.gz", data); err == nil {
		t.Errorf("mismatched checksum should error")
	}
	// Missing entry — explicit error, not a silent pass.
	if err := verifyChecksum(good, "other.tar.gz", data); err == nil {
		t.Errorf("missing checksum entry should error")
	}
}

// makeTarGz produces an in-memory tar.gz with a single member at the
// archive root. Mirrors what goreleaser produces for unix archives.
func makeTarGz(t *testing.T, name string, contents []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name:     name,
		Mode:     0o755,
		Size:     int64(len(contents)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatalf("write contents: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func TestExtractBinary_TarGz(t *testing.T) {
	want := []byte("\x7fELF...fake binary contents")
	archive := makeTarGz(t, "em-dee", want)
	got, err := extractBinary(archive, "linux")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extracted bytes differ: got %q want %q", got, want)
	}
}

func TestExtractBinary_MissingMember(t *testing.T) {
	archive := makeTarGz(t, "other-binary", []byte("payload"))
	if _, err := extractBinary(archive, "linux"); err == nil {
		t.Errorf("expected error when binary member is absent")
	}
}

func TestExpectedAssetName(t *testing.T) {
	cases := []struct {
		os, arch, want string
	}{
		{"linux", "amd64", "em-dee_linux_amd64.tar.gz"},
		{"darwin", "arm64", "em-dee_darwin_arm64.tar.gz"},
		{"windows", "amd64", "em-dee_windows_amd64.zip"},
	}
	for _, c := range cases {
		if got := expectedAssetName(c.os, c.arch); got != c.want {
			t.Errorf("expectedAssetName(%q,%q) = %q want %q", c.os, c.arch, got, c.want)
		}
	}
}

// fakeRelease wires an httptest.Server that serves a GitHub-style
// /releases/latest payload and the archive + checksums assets.
type fakeRelease struct {
	tag       string
	archive   []byte
	assetName string
	checksums []byte
	server    *httptest.Server
}

func newFakeRelease(t *testing.T, tag, assetName string, archive []byte, overrideChecksums []byte) *fakeRelease {
	t.Helper()
	fr := &fakeRelease{tag: tag, archive: archive, assetName: assetName, checksums: overrideChecksums}
	mux := http.NewServeMux()
	// Pre-build the checksums.txt for the asset (unless an override is
	// supplied — used by the mismatch test).
	if fr.checksums == nil {
		sum := sha256.Sum256(archive)
		fr.checksums = []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName))
	}
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		// The asset URLs need to point back at this same server so
		// the install path can resolve them. We can't compute that
		// until the server is running, so we use a placeholder and
		// rewrite below.
		w.Header().Set("Content-Type", "application/json")
		base := fr.server.URL
		payload := releasePayload{
			TagName: fr.tag,
			Assets: []releaseAsset{
				{Name: fr.assetName, BrowserDownloadURL: base + "/dl/" + fr.assetName},
				{Name: "checksums.txt", BrowserDownloadURL: base + "/dl/checksums.txt"},
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write(fr.checksums)
		case strings.HasSuffix(r.URL.Path, "/"+assetName):
			_, _ = w.Write(fr.archive)
		default:
			http.NotFound(w, r)
		}
	})
	fr.server = httptest.NewServer(mux)
	t.Cleanup(fr.server.Close)
	return fr
}

func TestRunUpdateInstall_Success(t *testing.T) {
	binaryBytes := []byte("\x7fELF...new em-dee")
	archive := makeTarGz(t, "em-dee", binaryBytes)
	fr := newFakeRelease(t, "v1.3.0", "em-dee_linux_amd64.tar.gz", archive, nil)

	var applied []byte
	stub := updaterFunc(func(b []byte) error {
		applied = append(applied, b...)
		return nil
	})

	env := updateCheckEnv{
		version: "1.2.3",
		client:  fr.server.Client(),
		exePath: "/usr/local/bin/em-dee",
		apiURL:  fr.server.URL + "/releases/latest",
		goos:    "linux",
		goarch:  "amd64",
	}
	result, err := runUpdateInstall(context.Background(), env, stub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.code != 0 {
		t.Errorf("success code = %d, want 0; msg=%q", result.code, result.message)
	}
	if !strings.Contains(result.message, "1.2.3") || !strings.Contains(result.message, "1.3.0") {
		t.Errorf("success message should report old and new version; got %q", result.message)
	}
	if !bytes.Equal(applied, binaryBytes) {
		t.Errorf("updater received %q, want %q", applied, binaryBytes)
	}
}

func TestRunUpdateInstall_ChecksumMismatch(t *testing.T) {
	archive := makeTarGz(t, "em-dee", []byte("real binary"))
	// Override checksums.txt so the lookup matches by name but the
	// hash differs from the archive's actual SHA256 (spec §12.6).
	bogus := []byte("0000000000000000000000000000000000000000000000000000000000000000  em-dee_linux_amd64.tar.gz\n")
	fr := newFakeRelease(t, "v1.3.0", "em-dee_linux_amd64.tar.gz", archive, bogus)

	called := false
	stub := updaterFunc(func(b []byte) error {
		called = true
		return nil
	})

	env := updateCheckEnv{
		version: "1.2.3",
		client:  fr.server.Client(),
		exePath: "/usr/local/bin/em-dee",
		apiURL:  fr.server.URL + "/releases/latest",
		goos:    "linux",
		goarch:  "amd64",
	}
	result, err := runUpdateInstall(context.Background(), env, stub)
	if err == nil {
		t.Errorf("expected error on checksum mismatch")
	}
	if result.code == 0 {
		t.Errorf("mismatch should not exit 0")
	}
	if called {
		t.Errorf("updater should NOT be called on checksum mismatch — binary must stay in place")
	}
	if !strings.Contains(strings.ToLower(result.message), "checksum") {
		t.Errorf("message should mention checksum; got %q", result.message)
	}
}

func TestRunUpdateInstall_AssetNotFound(t *testing.T) {
	// Release exists but ships only a different platform's asset, so
	// the lookup fails. Tests the "platform unsupported" path.
	archive := makeTarGz(t, "em-dee", []byte("payload"))
	fr := newFakeRelease(t, "v1.3.0", "em-dee_darwin_arm64.tar.gz", archive, nil)

	stub := updaterFunc(func(b []byte) error {
		t.Errorf("updater must not be invoked when no asset matches")
		return nil
	})
	env := updateCheckEnv{
		version: "1.2.3",
		client:  fr.server.Client(),
		exePath: "/usr/local/bin/em-dee",
		apiURL:  fr.server.URL + "/releases/latest",
		goos:    "linux",
		goarch:  "amd64",
	}
	result, err := runUpdateInstall(context.Background(), env, stub)
	if err == nil {
		t.Errorf("expected error when matching asset is missing")
	}
	if result.code == 0 {
		t.Errorf("missing asset should not exit 0")
	}
	if !strings.Contains(result.message, "em-dee_linux_amd64.tar.gz") {
		t.Errorf("message should name the missing asset; got %q", result.message)
	}
}

func TestRunUpdateInstall_AlreadyLatest(t *testing.T) {
	archive := makeTarGz(t, "em-dee", []byte("payload"))
	fr := newFakeRelease(t, "v1.2.3", "em-dee_linux_amd64.tar.gz", archive, nil)
	stub := updaterFunc(func(b []byte) error {
		t.Errorf("updater must not run when already on latest")
		return nil
	})
	env := updateCheckEnv{
		version: "1.2.3",
		client:  fr.server.Client(),
		exePath: "/usr/local/bin/em-dee",
		apiURL:  fr.server.URL + "/releases/latest",
		goos:    "linux",
		goarch:  "amd64",
	}
	result, _ := runUpdateInstall(context.Background(), env, stub)
	if result.code != 0 {
		t.Errorf("already-latest should exit 0; got %d", result.code)
	}
	if !strings.Contains(result.message, "latest version") {
		t.Errorf("message should mention latest version; got %q", result.message)
	}
}

func TestRunUpdateInstall_PackageManagerShortCircuit(t *testing.T) {
	stub := updaterFunc(func(b []byte) error {
		t.Errorf("updater must not run for go-install path")
		return nil
	})
	env := updateCheckEnv{
		version: "1.2.3",
		exePath: "/Users/james/go/bin/em-dee",
	}
	result, _ := runUpdateInstall(context.Background(), env, stub)
	if result.code != 0 {
		t.Errorf("package-manager short-circuit should exit 0; got %d", result.code)
	}
	if !strings.Contains(result.message, "go install") {
		t.Errorf("expected go install suggestion; got %q", result.message)
	}
}

func TestRunUpdateInstall_PermissionDenied(t *testing.T) {
	binaryBytes := []byte("new em-dee")
	archive := makeTarGz(t, "em-dee", binaryBytes)
	fr := newFakeRelease(t, "v1.3.0", "em-dee_linux_amd64.tar.gz", archive, nil)
	stub := updaterFunc(func(b []byte) error {
		return fmt.Errorf("open /usr/local/bin/em-dee: permission denied")
	})
	env := updateCheckEnv{
		version: "1.2.3",
		client:  fr.server.Client(),
		exePath: "/usr/local/bin/em-dee",
		apiURL:  fr.server.URL + "/releases/latest",
		goos:    "linux",
		goarch:  "amd64",
	}
	result, err := runUpdateInstall(context.Background(), env, stub)
	if err == nil {
		t.Errorf("expected error on permission denied")
	}
	if !strings.Contains(strings.ToLower(result.message), "sudo") {
		t.Errorf("permission error should suggest sudo per spec §12.6; got %q", result.message)
	}
}

// TestUpdate_LiveCommandInstallNetworkFailure wires the full cobra
// command tree to exercise the non-`--check` path through cobra. We
// inject an http.Client with a transport that always errors, so the
// install pipeline runs through `runUpdateInstall` and hits the
// network failure mode — proving cobra → RunE → runUpdateInstall is
// wired correctly and that the updater stub does NOT get called when
// the metadata fetch fails (spec §12.6 network failure mode).
func TestUpdate_LiveCommandInstallNetworkFailure(t *testing.T) {
	stubApplied := false
	stub := updaterFunc(func(b []byte) error {
		stubApplied = true
		return nil
	})
	root := NewRootCmd(Options{
		Version:          "1.0.0",
		updateHTTPClient: &http.Client{Transport: &stubTransport{err: errFakeNetwork}},
		updateExePath:    "/usr/local/bin/em-dee",
		updateApply:      stub,
	})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"update"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected non-nil error on network failure")
	}
	if ec, ok := err.(*exitCodeError); !ok || ec.code == 0 {
		t.Errorf("expected exitCodeError with non-zero code; got %T %v", err, err)
	}
	if stubApplied {
		t.Errorf("updater must not be called when metadata fetch fails")
	}
	if !strings.Contains(strings.ToLower(buf.String()), "network") {
		t.Errorf("expected network in failure message; got %q", buf.String())
	}
}

// TestRunUpdateInstall_RateLimited verifies that a 403 from the
// GitHub API surfaces the user-actionable GITHUB_TOKEN hint on the
// install path (spec §12.6). Previously the install path collapsed
// all non-200 responses into a generic "github api returned 403", so
// the hint that --check produces was lost — see PR #7 review item H1.
func TestRunUpdateInstall_RateLimited(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	stub := updaterFunc(func(b []byte) error {
		t.Errorf("updater must not run on metadata 403")
		return nil
	})
	env := updateCheckEnv{
		version: "1.2.3",
		client:  srv.Client(),
		exePath: "/usr/local/bin/em-dee",
		apiURL:  srv.URL + "/releases/latest",
		goos:    "linux",
		goarch:  "amd64",
	}
	result, err := runUpdateInstall(context.Background(), env, stub)
	if err == nil {
		t.Errorf("expected error on 403")
	}
	if result.code == 0 {
		t.Errorf("403 should not exit 0")
	}
	if !strings.Contains(result.message, "GITHUB_TOKEN") {
		t.Errorf("403 message should mention GITHUB_TOKEN; got %q", result.message)
	}
}

// TestRunUpdateInstall_NoReleaseYet verifies that a 404 from the
// GitHub API surfaces the "no release found yet" message on the
// install path, mirroring --check (spec §12.6, PR #7 review item H1).
func TestRunUpdateInstall_NoReleaseYet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	stub := updaterFunc(func(b []byte) error {
		t.Errorf("updater must not run on metadata 404")
		return nil
	})
	env := updateCheckEnv{
		version: "1.2.3",
		client:  srv.Client(),
		exePath: "/usr/local/bin/em-dee",
		apiURL:  srv.URL + "/releases/latest",
		goos:    "linux",
		goarch:  "amd64",
	}
	result, err := runUpdateInstall(context.Background(), env, stub)
	if err == nil {
		t.Errorf("expected error on 404")
	}
	if result.code == 0 {
		t.Errorf("404 should not exit 0")
	}
	if !strings.Contains(result.message, "no release found yet") {
		t.Errorf("404 message should say 'no release found yet'; got %q", result.message)
	}
}

// TestUpdate_LiveCommandInstallPackageManagerShortCircuit verifies the
// install path refuses to run when the binary was installed via a
// package manager (spec §12.6). Cobra → RunE → runUpdateInstall →
// short-circuit before any network call.
func TestUpdate_LiveCommandInstallPackageManagerShortCircuit(t *testing.T) {
	stubApplied := false
	stub := updaterFunc(func(b []byte) error {
		stubApplied = true
		return nil
	})
	root := NewRootCmd(Options{
		Version: "1.0.0",
		// Transport errors on any request — ensures the short-circuit
		// happens before the network is touched.
		updateHTTPClient: &http.Client{Transport: &stubTransport{err: errFakeNetwork}},
		updateExePath:    "/Users/james/go/bin/em-dee",
		updateApply:      stub,
	})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"update"})
	if err := root.Execute(); err != nil {
		t.Fatalf("package-manager short-circuit should succeed (exit 0); got %v", err)
	}
	if stubApplied {
		t.Errorf("updater must not run when install method is go-install")
	}
	if !strings.Contains(buf.String(), "go install") {
		t.Errorf("expected go install suggestion in output; got %q", buf.String())
	}
}
