package cli

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// versionPayload is the JSON shape mandated by spec §12.6:
// {"version","commit","date","platform"}. Tooling consumers (notably
// `em-dee update`) parse this, so the field names are a contract — do
// not change without bumping a contract version.
type versionPayload struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Date     string `json:"date"`
	Platform string `json:"platform"`
}

// newVersionCmd builds `em-dee version`. Human form by default; pass
// `--json` for the structured payload.
func newVersionCmd(opts Options) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the em-dee version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if asJSON {
				payload := versionPayload{
					Version:  opts.Version,
					Commit:   opts.Commit,
					Date:     opts.Date,
					Platform: runtime.GOOS + "/" + runtime.GOARCH,
				}
				enc := json.NewEncoder(out)
				// No HTML escaping (em-dee is a CLI, not a browser
				// consumer); compact one-line output so `jq`-style
				// pipelines stay tidy. Encoder always appends "\n".
				enc.SetEscapeHTML(false)
				return enc.Encode(payload)
			}
			fmt.Fprintf(out, "em-dee %s (commit %s, built %s)\n",
				opts.Version, opts.Commit, opts.Date)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}
