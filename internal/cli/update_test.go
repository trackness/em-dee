package cli

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

// stubTransport injects a canned HTTP response into the update path so
// the test runs without network. resp may be nil to simulate a
// transport-level error.
type stubTransport struct {
	resp *http.Response
	err  error
}

func (s *stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestUpdateCheck_UpToDate(t *testing.T) {
	body := `{"tag_name":"v1.2.3"}`
	got, _ := runUpdateCheck(updateCheckEnv{
		version: "1.2.3",
		client:  &http.Client{Transport: &stubTransport{resp: jsonResp(200, body)}},
		exePath: "/usr/local/bin/em-dee",
	})
	if got.code != 0 {
		t.Errorf("up-to-date: code = %d, want 0; msg=%q", got.code, got.message)
	}
	if !strings.Contains(got.message, "1.2.3") {
		t.Errorf("up-to-date message should mention current version; got %q", got.message)
	}
}

func TestUpdateCheck_UpdateAvailable(t *testing.T) {
	body := `{"tag_name":"v1.3.0"}`
	got, _ := runUpdateCheck(updateCheckEnv{
		version: "1.2.3",
		client:  &http.Client{Transport: &stubTransport{resp: jsonResp(200, body)}},
		exePath: "/usr/local/bin/em-dee",
	})
	if got.code != 1 {
		t.Errorf("update-available: code = %d, want 1; msg=%q", got.code, got.message)
	}
	if !strings.Contains(got.message, "1.3.0") {
		t.Errorf("update-available message should mention new version; got %q", got.message)
	}
}

func TestUpdateCheck_NetworkError(t *testing.T) {
	got, _ := runUpdateCheck(updateCheckEnv{
		version: "1.2.3",
		client:  &http.Client{Transport: &stubTransport{err: errFakeNetwork}},
		exePath: "/usr/local/bin/em-dee",
	})
	if got.code != 2 {
		t.Errorf("network-error: code = %d, want 2; msg=%q", got.code, got.message)
	}
}

func TestUpdateCheck_404NoReleaseYet(t *testing.T) {
	body := `{"message":"Not Found"}`
	got, _ := runUpdateCheck(updateCheckEnv{
		version: "0.1.0-dev",
		client:  &http.Client{Transport: &stubTransport{resp: jsonResp(404, body)}},
		exePath: "/usr/local/bin/em-dee",
	})
	if got.code != 2 {
		t.Errorf("404-no-release: code = %d, want 2; msg=%q", got.code, got.message)
	}
	if !strings.Contains(strings.ToLower(got.message), "no release") &&
		!strings.Contains(got.message, "404") {
		t.Errorf("404 message should mention no-release or 404; got %q", got.message)
	}
}

func TestUpdateCheck_RateLimited(t *testing.T) {
	body := `{"message":"API rate limit exceeded"}`
	got, _ := runUpdateCheck(updateCheckEnv{
		version: "1.2.3",
		client:  &http.Client{Transport: &stubTransport{resp: jsonResp(403, body)}},
		exePath: "/usr/local/bin/em-dee",
	})
	if got.code != 2 {
		t.Errorf("rate-limit: code = %d, want 2", got.code)
	}
}

// Install-method detection: each input path should resolve to the
// documented action (Code + suggested-command if applicable).
func TestDetectInstallMethod(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		wantKey string // a substring that must appear in the suggestion ("" = none / proceed)
	}{
		{"unix /usr/local/bin", "/usr/local/bin/em-dee", ""},
		{"go-install GOPATH/bin", "/Users/james/go/bin/em-dee", "go install"},
		{"homebrew apple silicon", "/opt/homebrew/bin/em-dee", "brew upgrade"},
		{"homebrew cellar", "/usr/local/Cellar/em-dee/1.2.3/bin/em-dee", "brew upgrade"},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/bin/em-dee", "brew upgrade"},
		{"windows go bin", `C:\Users\me\go\bin\em-dee.exe`, "go install"},
		{"windows other", `C:\Program Files\em-dee\em-dee.exe`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method := detectInstallMethod(tc.path)
			if tc.wantKey == "" {
				if method.viaPackageManager {
					t.Errorf("expected proceed (no package manager); got %+v", method)
				}
				return
			}
			if !method.viaPackageManager {
				t.Fatalf("expected package-manager install method; got %+v", method)
			}
			if !strings.Contains(method.suggestion, tc.wantKey) {
				t.Errorf("suggestion %q missing %q", method.suggestion, tc.wantKey)
			}
		})
	}
}

func TestUpdateCheck_RefusesForPackageManager(t *testing.T) {
	got, _ := runUpdateCheck(updateCheckEnv{
		version: "1.2.3",
		// No HTTP should be hit; pass a transport that errors so we
		// catch any accidental call.
		client:  &http.Client{Transport: &stubTransport{err: errFakeNetwork}},
		exePath: "/Users/james/go/bin/em-dee",
	})
	if got.code != 0 {
		t.Errorf("package-manager: code = %d, want 0; msg=%q", got.code, got.message)
	}
	if !strings.Contains(got.message, "go install") {
		t.Errorf("expected go install suggestion; got %q", got.message)
	}
}

// TestUpdateCheck_LiveCommand asserts the `update --check` cobra path
// wires correctly and reflects exit code via error. Use a fake
// transport returning a 200 with a newer version → exit code 1.
func TestUpdateCheck_LiveCommandUpdateAvailable(t *testing.T) {
	body := `{"tag_name":"v9.9.9"}`
	root := NewRootCmd(Options{
		Version:          "1.0.0",
		updateHTTPClient: &http.Client{Transport: &stubTransport{resp: jsonResp(200, body)}},
		updateExePath:    "/usr/local/bin/em-dee",
	})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"update", "--check"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected non-nil error so caller can map to exit code 1")
	}
	if uerr, ok := err.(*exitCodeError); !ok || uerr.code != 1 {
		t.Errorf("expected exitCodeError{code=1}; got %T %v", err, err)
	}
}

// errFakeNetwork is a sentinel for stubTransport's error path.
var errFakeNetwork = fakeErr("simulated network failure")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }
