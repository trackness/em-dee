// Package main is the em-dee CLI entrypoint. The build-time variables
// below are populated by `goreleaser` (via ldflags) or by local
// `task build`.
package main

import "github.com/trackness/em-dee/internal/cli"

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cli.Execute(version, commit, date)
}
