package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
)

// Install path for `em-dee update` (non-`--check`).
//
// Spec: docs/superpowers/specs/2026-05-11-em-dee-design.md §12.6
// Plan: docs/superpowers/plans/2026-05-11-em-dee-implementation.md Task 6.4
//
// Flow (numbered to match spec §12.6 step list):
//   1. GitHub Releases API → latest release metadata + asset list.
//   2. Pick the platform-appropriate archive asset
//      (em-dee_<os>_<arch>.<ext>) matching goreleaser's name_template
//      from .goreleaser.yaml. Mismatched naming = no asset = abort.
//   3. Download the archive AND `checksums.txt` from the same release.
//   4. Verify the archive's SHA256 against checksums.txt; mismatch
//      aborts without touching the running binary.
//   5. Extract the `em-dee` binary out of the archive (tar.gz on unix,
//      zip on windows).
//   6. selfupdate.Apply atomic-replaces the running binary.

// archiveExt returns the goreleaser archive extension for the current
// runtime.GOOS, matching the format_overrides block in
// .goreleaser.yaml: zip on windows, tar.gz everywhere else.
func archiveExt(goos string) string {
	if goos == "windows" {
		return "zip"
	}
	return "tar.gz"
}

// expectedAssetName returns the archive filename the running platform
// should look for in the release's asset list. The format must mirror
// the `name_template: em-dee_{{ .Os }}_{{ .Arch }}` rule in
// .goreleaser.yaml — any drift here silently breaks self-update for
// users on this platform until somebody notices, which is why this
// function is a single shared source of truth used by both the asset
// resolver and the checksum parser.
func expectedAssetName(goos, goarch string) string {
	return fmt.Sprintf("em-dee_%s_%s.%s", goos, goarch, archiveExt(goos))
}

// releaseAsset is the subset of GitHub's release.assets[] payload that
// the install path needs. Field names match the API verbatim.
type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// releasePayload is the full subset of the GitHub release object the
// install path consumes. tag_name doubles as the version string the
// "updated old -> new" message prints.
type releasePayload struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

// fetchLatestRelease GETs the latest-release metadata via the GitHub
// API. Returns the parsed payload plus the raw status code so callers
// can differentiate rate-limit / 404 / parse failures in their error
// messages (spec §12.6 failure modes).
func fetchLatestRelease(ctx context.Context, client *http.Client, apiURL string) (releasePayload, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return releasePayload{}, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return releasePayload{}, 0, fmt.Errorf("network unavailable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return releasePayload{}, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return releasePayload{}, resp.StatusCode, fmt.Errorf("github api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var p releasePayload
	if err := json.Unmarshal(body, &p); err != nil {
		return releasePayload{}, resp.StatusCode, fmt.Errorf("parse release JSON: %w", err)
	}
	return p, resp.StatusCode, nil
}

// findAsset returns the asset matching `name`, or an error listing the
// available names so the user can see what went wrong if goreleaser's
// naming drifted from this binary's expectation.
func findAsset(payload releasePayload, name string) (releaseAsset, error) {
	for _, a := range payload.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	names := make([]string, 0, len(payload.Assets))
	for _, a := range payload.Assets {
		names = append(names, a.Name)
	}
	return releaseAsset{}, fmt.Errorf("no asset named %q in release %s; available: %s", name, payload.TagName, strings.Join(names, ", "))
}

// parseChecksums reads a `checksums.txt` produced by goreleaser
// (`sha256sum`-compatible: "<hex>  <filename>\n") into a filename → hex
// map. Blank lines and leading/trailing whitespace are tolerated.
// Lines without exactly two whitespace-separated fields are skipped
// (defensive — the file shouldn't have them, but we'd rather drop a
// malformed line than fail the whole parse).
func parseChecksums(data []byte) map[string]string {
	out := make(map[string]string)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// sha256sum format uses two spaces between hash and filename,
		// but `strings.Fields` handles any whitespace count.
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		// Filename may be prefixed with `*` (binary mode marker from
		// sha256sum -b). Strip it so the lookup matches the bare name.
		name := strings.TrimPrefix(parts[1], "*")
		out[name] = parts[0]
	}
	return out
}

// verifyChecksum returns nil if the SHA256 of `data` matches the hex
// digest the checksums file lists for `name`. Missing entry =
// explicit error (spec §12.6 mandates verification, not best-effort).
func verifyChecksum(checksums map[string]string, name string, data []byte) error {
	want, ok := checksums[name]
	if !ok {
		return fmt.Errorf("checksum for %q not found in checksums.txt", name)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", name, got, want)
	}
	return nil
}

// extractBinary pulls the `em-dee` binary out of the archive bytes.
// `goos` selects the archive format (windows → zip, else tar.gz) so
// the call shape stays the same from the caller's perspective.
//
// The binary inside the archive is named `em-dee` on unix and
// `em-dee.exe` on windows (goreleaser's default for binary names).
func extractBinary(archive []byte, goos string) ([]byte, error) {
	binaryName := "em-dee"
	if goos == "windows" {
		binaryName = "em-dee.exe"
	}
	if goos == "windows" {
		return extractZipMember(archive, binaryName)
	}
	return extractTarGzMember(archive, binaryName)
}

// extractTarGzMember scans a gzip-compressed tar archive for a regular
// file whose base name matches `target` and returns its bytes.
func extractTarGzMember(data []byte, target string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// goreleaser's archives place files at the root; defensive
		// base-name match covers any future change to subdirectories.
		base := hdr.Name
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if base != target {
			continue
		}
		// Cap at 200 MiB so a corrupt archive can't blow up RAM.
		const maxBinarySize = 200 << 20
		buf, err := io.ReadAll(io.LimitReader(tr, maxBinarySize))
		if err != nil {
			return nil, fmt.Errorf("tar copy: %w", err)
		}
		return buf, nil
	}
	return nil, fmt.Errorf("binary %q not found in archive", target)
}

// extractZipMember does the windows-equivalent of extractTarGzMember.
func extractZipMember(data []byte, target string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("zip open: %w", err)
	}
	for _, f := range zr.File {
		base := f.Name
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if base != target {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("zip open member: %w", err)
		}
		const maxBinarySize = 200 << 20
		buf, err := io.ReadAll(io.LimitReader(rc, maxBinarySize))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("zip copy: %w", err)
		}
		return buf, nil
	}
	return nil, fmt.Errorf("binary %q not found in archive", target)
}

// downloadBytes fetches a URL and returns the body. Wrapped here so
// the asset download and checksums.txt download share consistent
// error handling.
func downloadBytes(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		// Authenticated downloads are still rate-limited but at a
		// higher cap; non-essential for asset CDN but helps the
		// checksums.txt fetch which goes through api.github.com.
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}
	// Cap downloads at 200 MiB so a misbehaving CDN can't blow out the
	// process. Real archives are ~10 MiB.
	const maxDownloadSize = 200 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return body, nil
}

// checksumsURL returns the GitHub asset URL for the checksums.txt file
// produced by goreleaser. Both releases/latest/download and the
// per-release URL pattern work; we use the same release's asset URL
// to make sure the checksums and the archive came from the same tag.
func checksumsURL(payload releasePayload) (string, error) {
	for _, a := range payload.Assets {
		if a.Name == "checksums.txt" {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("release %s has no checksums.txt asset", payload.TagName)
}

// updaterFunc is the seam through which the actual binary replace
// happens. Production wires it to selfupdate.Apply; tests substitute
// a no-op so the install pipeline can be exercised without touching
// the running executable.
type updaterFunc func(newBinary []byte) error

// runUpdateInstall is the full install pipeline. Returns a message to
// print and an exit code (0 = success, non-zero = failure mode). Like
// runUpdateCheck above, all side effects come through the env so unit
// tests cover every branch.
//
// The exit codes are deliberately non-three-state (unlike --check) —
// the install path is either successful (0) or failed (non-zero). The
// caller wraps the failure into an *exitCodeError so cobra surfaces it.
func runUpdateInstall(ctx context.Context, env updateCheckEnv, apply updaterFunc) (updateCheckResult, error) {
	method := detectInstallMethod(env.exePath)
	if method.viaPackageManager {
		// Same short-circuit as --check; don't pretend to install when
		// the install method isn't ours to manage.
		return updateCheckResult{
			code:    0,
			message: fmt.Sprintf("em-dee was installed via a package manager; update with: %s", method.suggestion),
		}, nil
	}

	apiURL := releaseAPIURL
	if env.apiURL != "" {
		apiURL = env.apiURL
	}

	payload, _, err := fetchLatestRelease(ctx, env.client, apiURL)
	if err != nil {
		// fetchLatestRelease's error string already includes context
		// (e.g. "network unavailable", "github api returned 403").
		return updateCheckResult{code: 2, message: err.Error()}, err
	}

	latest := strings.TrimPrefix(payload.TagName, "v")
	current := strings.TrimPrefix(env.version, "v")
	if latest == "" {
		return updateCheckResult{code: 2, message: "release payload has no tag_name"}, nil
	}
	if current == latest {
		// Spec §12.6: "already on latest" exits 0 with a clear message.
		return updateCheckResult{
			code:    0,
			message: fmt.Sprintf("you are on the latest version (%s)", current),
		}, nil
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if env.goos != "" {
		goos = env.goos
	}
	if env.goarch != "" {
		goarch = env.goarch
	}
	wantName := expectedAssetName(goos, goarch)

	asset, err := findAsset(payload, wantName)
	if err != nil {
		return updateCheckResult{code: 2, message: err.Error()}, err
	}
	sumsURL, err := checksumsURL(payload)
	if err != nil {
		return updateCheckResult{code: 2, message: err.Error()}, err
	}

	archive, err := downloadBytes(ctx, env.client, asset.BrowserDownloadURL)
	if err != nil {
		return updateCheckResult{code: 2, message: fmt.Sprintf("download archive: %v", err)}, err
	}
	sumsBytes, err := downloadBytes(ctx, env.client, sumsURL)
	if err != nil {
		return updateCheckResult{code: 2, message: fmt.Sprintf("download checksums: %v", err)}, err
	}

	checksums := parseChecksums(sumsBytes)
	if err := verifyChecksum(checksums, wantName, archive); err != nil {
		// Spec §12.6 security: mismatch aborts, binary unchanged.
		return updateCheckResult{
			code:    2,
			message: fmt.Sprintf("aborting: %v", err),
		}, err
	}

	binary, err := extractBinary(archive, goos)
	if err != nil {
		return updateCheckResult{code: 2, message: fmt.Sprintf("extract: %v", err)}, err
	}

	if err := apply(binary); err != nil {
		// selfupdate's permission error is the canonical case here.
		// Surface a sudo hint per spec §12.6 failure modes.
		if isPermissionError(err) {
			return updateCheckResult{
				code:    2,
				message: fmt.Sprintf("cannot replace binary (permission denied): %v\nhint: re-run with sudo", err),
			}, err
		}
		return updateCheckResult{code: 2, message: fmt.Sprintf("install: %v", err)}, err
	}

	return updateCheckResult{
		code:    0,
		message: fmt.Sprintf("updated %s → %s", current, latest),
	}, nil
}

// isPermissionError detects the "can't write to /usr/local/bin" case
// across OS-specific error wrappers. os.IsPermission catches the
// common path; we also check the string fallback for wrapped errors
// that don't unwrap into an os.PathError.
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied") || strings.Contains(msg, "access is denied")
}
