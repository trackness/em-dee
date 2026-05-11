package cli

import "fmt"

// Execute is the single entrypoint from `cmd/em-dee/main.go`. Phase 3
// replaces this stub with a full cobra command tree; the signature is
// frozen now per spec §12.7 so the version-embedding shape is locked
// in before later wiring.
func Execute(version, commit, date string) {
	_ = commit
	_ = date
	fmt.Printf("em-dee %s\n", version)
}
