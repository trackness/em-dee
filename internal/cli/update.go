package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"
)

// updateCheckTimeout is the upper bound on the GitHub API request.
// 15s balances tolerating slow networks with not stranding the user
// on a hung connection (spec §12.6 lists network unavailable as a
// failure mode, exit 2). Documented choice per plan Task 3.6.
const updateCheckTimeout = 15 * time.Second

// releaseAPIURL is the GitHub Releases API endpoint queried for the
// latest tag. Repo path matches spec §12.1 (`trackness/em-dee`).
const releaseAPIURL = "https://api.github.com/repos/trackness/em-dee/releases/latest"

// exitCodeError wraps a process exit code so the cobra layer can map
// a `--check` outcome to the three-state convention from spec §12.6
// (0 = up-to-date, 1 = update available, 2 = error) without abusing
// fmt.Errorf for control flow.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string { return e.msg }

// installMethod is the resolved install-shape for the running binary.
// viaPackageManager == true means we refuse self-update and instead
// print `suggestion`; false means proceed with the network check.
type installMethod struct {
	viaPackageManager bool
	suggestion        string
}

// detectInstallMethod inspects an executable path and returns whether
// the binary was installed via a package manager (`go install`,
// homebrew, linuxbrew) per spec §12.6's rules. Pure: takes the path
// as an argument so unit tests cover every rule without touching the
// real filesystem.
//
// Rule order matters: go-install paths are detected first because
// they're the most common false-positive for the "proceed" branch (a
// developer's `$HOME/go/bin/` looks like a regular bin path otherwise).
func detectInstallMethod(exePath string) installMethod {
	// Normalise Windows backslashes so the rules below can use
	// forward-slash patterns uniformly. Windows go-install detection
	// works through this normalisation (`C:\Users\me\go\bin\em-dee.exe`
	// → contains `/go/bin/`) — no runtime.GOOS gate needed.
	p := strings.ReplaceAll(exePath, "\\", "/")

	// Unix + Windows go-install detection: $GOPATH/bin, $HOME/go/bin,
	// $(go env GOBIN). We can't read GOPATH from inside the running
	// em-dee binary reliably (the user may have a different env than
	// at install time), so check the common locations.
	if strings.Contains(p, "/go/bin/") {
		return installMethod{
			viaPackageManager: true,
			suggestion:        "go install github.com/trackness/em-dee/cmd/em-dee@latest",
		}
	}

	// Homebrew (macOS Apple Silicon, Intel Cellar, linuxbrew). The
	// "Cellar" rule covers /usr/local/Cellar/em-dee/... on Intel Macs.
	if strings.Contains(p, "/opt/homebrew/") ||
		strings.Contains(p, "/usr/local/Cellar/") ||
		strings.Contains(p, "/home/linuxbrew/.linuxbrew/") {
		return installMethod{
			viaPackageManager: true,
			suggestion:        "brew upgrade trackness/tap/em-dee",
		}
	}

	return installMethod{}
}

// updateCheckEnv is the dependency-injected context for runUpdateCheck
// and runUpdateInstall (Task 6.4). Each field has a production default
// (filled by newUpdateCmd when not otherwise supplied); tests construct
// the env directly.
//
// apiURL / goos / goarch are seams for the install path: tests point
// apiURL at an httptest.Server, and override goos/goarch to drive the
// asset-name selection without touching runtime.GOOS.
type updateCheckEnv struct {
	version string
	client  *http.Client
	exePath string
	apiURL  string
	goos    string
	goarch  string
}

// updateCheckResult is the structured outcome of a `--check` run. The
// caller decides whether to translate this into an os.Exit, a cobra
// error, or a stdout print — runUpdateCheck itself stays pure.
type updateCheckResult struct {
	code    int    // 0 up-to-date, 1 update available, 2 error
	message string // human-readable line for stdout / stderr
}

// runUpdateCheck performs install-method detection and (if applicable)
// the GitHub Releases lookup. Pure-ish: all side effects (HTTP, exe
// path) come through `env`. Returns (result, nil) on success and
// (result, err) on transport failure — but result.code is always set,
// so callers can rely on it even when err != nil.
func runUpdateCheck(env updateCheckEnv) (updateCheckResult, error) {
	// Install-method check first: a brew/go-install user gets a
	// short-circuit answer without touching the network.
	method := detectInstallMethod(env.exePath)
	if method.viaPackageManager {
		return updateCheckResult{
			code:    0,
			message: fmt.Sprintf("em-dee was installed via a package manager; update with: %s", method.suggestion),
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPIURL, nil)
	if err != nil {
		return updateCheckResult{code: 2, message: fmt.Sprintf("build request: %v", err)}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// Honor GITHUB_TOKEN per spec §12.6's rate-limit failure-mode note.
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := env.client.Do(req)
	if err != nil {
		return updateCheckResult{
			code:    2,
			message: fmt.Sprintf("network unavailable: %v", err),
		}, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return updateCheckResult{
			code:    2,
			message: fmt.Sprintf("read response: %v", readErr),
		}, readErr
	}

	if resp.StatusCode == http.StatusNotFound {
		return updateCheckResult{
			code:    2,
			message: "no release found yet for trackness/em-dee (GitHub API returned 404)",
		}, nil
	}
	if resp.StatusCode == http.StatusForbidden {
		return updateCheckResult{
			code:    2,
			message: "GitHub API rate-limited or forbidden; set GITHUB_TOKEN to raise the limit",
		}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return updateCheckResult{
			code:    2,
			message: fmt.Sprintf("GitHub API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
		}, nil
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return updateCheckResult{
			code:    2,
			message: fmt.Sprintf("parse release JSON: %v", err),
		}, err
	}
	latest := strings.TrimPrefix(payload.TagName, "v")
	if latest == "" {
		return updateCheckResult{
			code:    2,
			message: "release payload has no tag_name",
		}, nil
	}

	current := strings.TrimPrefix(env.version, "v")

	// Dev-build special case (spec §12.7): a binary built locally
	// without ldflags carries version="dev" (and the goreleaser path
	// stamps `dev-<sha>` for nightly-style builds). Comparing either
	// against a release tag always trips the "update available"
	// branch with the misleading "dev → 1.2.3" line. Treat the
	// dev-build case as a distinct success state — exit 0, message
	// names the latest release without claiming an "update is
	// available" — so scripts pinned to exit 0 for "you're up to
	// date" don't churn on developer machines.
	if isDevBuildVersion(current) {
		return updateCheckResult{
			code:    0,
			message: fmt.Sprintf("running a dev build (%s); latest release is %s — use --check after installing a tagged release for upgrade prompts", current, latest),
		}, nil
	}

	if current == latest {
		return updateCheckResult{
			code:    0,
			message: fmt.Sprintf("you are on the latest version (%s)", current),
		}, nil
	}
	return updateCheckResult{
		code:    1,
		message: fmt.Sprintf("update available: %s → %s", current, latest),
	}, nil
}

// isDevBuildVersion reports whether a normalised version string is
// the dev-build sentinel produced by `go build` without ldflags
// (`dev`) or by a nightly-style goreleaser run (`dev-<sha>`). Spec
// §12.7 enumerates these as the only non-release version shapes;
// anything else is treated as a release tag and compared by string
// equality. Semver comparison is deliberately out of scope for v1
// (spec §12.6 only mandates the three-state exit code, not how
// "newer" is decided).
func isDevBuildVersion(v string) bool {
	return v == "dev" || strings.HasPrefix(v, "dev-")
}

// updateInstallTimeout is the upper bound on the download + verify +
// apply pipeline. Larger than updateCheckTimeout because the asset
// download is the long pole — a ~15 MiB archive over a slow link can
// take a couple of minutes. Documented choice per plan Task 6.4.
const updateInstallTimeout = 5 * time.Minute

// defaultUpdater is the production binary-replace function: it wraps
// minio/selfupdate.Apply, which handles atomic replace cross-platform
// (renaming the running executable on unix, and a delayed rename on
// windows where you can't rename an open file). Pulled out as a var
// so tests substitute a no-op without touching the running binary.
//
// Tradeoff (per plan Task 6.4): minio/selfupdate over
// inconshreveable/go-update. Both libraries do the same job; minio's
// fork is the actively-maintained one (last release 2024+), with the
// same Apply signature. inconshreveable's repo has been archived for
// years, so even though the spec mentions either, picking the
// maintained fork is the smaller-risk choice.
var defaultUpdater updaterFunc = func(newBinary []byte) error {
	return selfupdate.Apply(bytes.NewReader(newBinary), selfupdate.Options{})
}

// newUpdateCmd builds `em-dee update`. Both the read-only `--check`
// path (Task 3.6) and the install path (Task 6.4) are wired here.
func newUpdateCmd(opts Options) *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for or install em-dee updates",
		Args:  cobra.NoArgs,
		// SilenceUsage on user-level errors; the result message is the
		// substantive output, usage noise would just clutter it.
		// SilenceErrors suppresses cobra's automatic stderr print for
		// the exitCodeError returned by --check (we print the message
		// ourselves to direct it to stdout for the 1=update-available
		// path, where scripts may want to read it).
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env := updateCheckEnv{
				version: opts.Version,
				client:  opts.updateHTTPClient,
				exePath: opts.updateExePath,
			}
			if env.exePath == "" {
				if p, err := os.Executable(); err == nil {
					env.exePath = p
				}
			}

			// runUpdateCheck (and runUpdateInstall) both guarantee
			// result.code is set even when err is non-nil; the error
			// itself carries diagnostic detail already folded into
			// result.message, so we drop it. If a `-v` / verbose flag
			// lands later, route the error through that path. Stream
			// routing below: stdout for happy paths (0/1) so scripts
			// can read it, stderr for errors (2) so it doesn't
			// pollute pipelines.
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()

			if checkOnly {
				// --check uses the lighter timeout (one API call only).
				if env.client == nil {
					env.client = &http.Client{Timeout: updateCheckTimeout}
				}
				result, _ := runUpdateCheck(env)
				stream := out
				if result.code == 2 {
					stream = errOut
				}
				fmt.Fprintln(stream, result.message)
				if result.code == 0 {
					return nil
				}
				return &exitCodeError{code: result.code, msg: result.message}
			}

			// Real install path (Task 6.4). The pipeline downloads the
			// platform archive + checksums.txt, verifies SHA256, extracts
			// the binary, and atomically replaces the running executable
			// via selfupdate.Apply (spec §12.6).
			if env.client == nil {
				env.client = &http.Client{Timeout: updateInstallTimeout}
			}
			updater := opts.updateApply
			if updater == nil {
				updater = defaultUpdater
			}
			ctx, cancel := context.WithTimeout(context.Background(), updateInstallTimeout)
			defer cancel()
			result, _ := runUpdateInstall(ctx, env, updater)
			stream := out
			if result.code != 0 {
				stream = errOut
			}
			fmt.Fprintln(stream, result.message)
			if result.code == 0 {
				return nil
			}
			return &exitCodeError{code: result.code, msg: result.message}
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check for updates; do not install")
	return cmd
}
