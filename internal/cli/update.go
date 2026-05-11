package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

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
	// forward-slash patterns uniformly. The actual path string the
	// caller passed in is preserved for the suggestion text via
	// runtime.GOOS gating below.
	p := strings.ReplaceAll(exePath, "\\", "/")

	// Unix go-install detection: $GOPATH/bin, $HOME/go/bin, $(go env
	// GOBIN). We can't read GOPATH from inside the running em-dee
	// binary reliably (the user may have a different env than at
	// install time), so check the common locations.
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

	_ = runtime.GOOS // reserved for future windows-specific suggestion text
	return installMethod{}
}

// updateCheckEnv is the dependency-injected context for runUpdateCheck.
// Each field has a production default (filled by newUpdateCmd when not
// otherwise supplied); tests construct the env directly.
type updateCheckEnv struct {
	version string
	client  *http.Client
	exePath string
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

// newUpdateCmd builds `em-dee update`. Phase 3 only implements the
// `--check` path (read-only); the actual self-install lands in Phase 6
// (Task 6.4) after the release pipeline exists.
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
			if !checkOnly {
				return fmt.Errorf("only --check is implemented in this build; full self-update lands in Phase 6")
			}

			env := updateCheckEnv{
				version: opts.Version,
				client:  opts.updateHTTPClient,
				exePath: opts.updateExePath,
			}
			if env.client == nil {
				env.client = &http.Client{Timeout: updateCheckTimeout}
			}
			if env.exePath == "" {
				if p, err := os.Executable(); err == nil {
					env.exePath = p
				}
			}

			result, _ := runUpdateCheck(env)
			// Direct the message to the right stream: stdout for the
			// happy paths (0/1) since scripts may want to read it,
			// stderr for errors (2) so it doesn't pollute pipelines.
			out := cmd.OutOrStdout()
			if result.code == 2 {
				out = cmd.ErrOrStderr()
			}
			fmt.Fprintln(out, result.message)

			if result.code == 0 {
				return nil
			}
			return &exitCodeError{code: result.code, msg: result.message}
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check for updates; do not install")
	return cmd
}
