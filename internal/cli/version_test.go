package cli

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

// TestVersion_Human asserts the human-readable format (plan Task 3.1).
func TestVersion_Human(t *testing.T) {
	root := NewRootCmd(Options{
		Version: "1.2.3",
		Commit:  "abcdef0",
		Date:    "2026-05-11",
	})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := buf.String()
	want := "em-dee 1.2.3 (commit abcdef0, built 2026-05-11)\n"
	if got != want {
		t.Errorf("version output mismatch\n got: %q\nwant: %q", got, want)
	}
}

// TestVersion_JSON asserts the JSON shape:
// {"version","commit","date","platform"}.
func TestVersion_JSON(t *testing.T) {
	root := NewRootCmd(Options{
		Version: "1.2.3",
		Commit:  "abcdef0",
		Date:    "2026-05-11",
	})
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"version", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got struct {
		Version  string `json:"version"`
		Commit   string `json:"commit"`
		Date     string `json:"date"`
		Platform string `json:"platform"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}
	if got.Version != "1.2.3" {
		t.Errorf("version: got %q want 1.2.3", got.Version)
	}
	if got.Commit != "abcdef0" {
		t.Errorf("commit: got %q want abcdef0", got.Commit)
	}
	if got.Date != "2026-05-11" {
		t.Errorf("date: got %q want 2026-05-11", got.Date)
	}
	wantPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if got.Platform != wantPlatform {
		t.Errorf("platform: got %q want %q", got.Platform, wantPlatform)
	}
	// JSON output must end in a newline so shell pipelines stay tidy.
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("expected trailing newline, got %q", buf.String())
	}
}
