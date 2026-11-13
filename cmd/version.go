package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/jniltinho/meethub/internal/buildinfo"
)

func newVersionCmd(c *cli) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit and build time",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if c.asJSON {
				return c.printJSON(map[string]string{
					"version":    buildinfo.Version,
					"commit":     buildinfo.Commit,
					"build_time": buildinfo.BuildTime,
					"go":         runtime.Version(),
					"platform":   runtime.GOOS + "/" + runtime.GOARCH,
				})
			}
			_, err := fmt.Fprintf(c.out, "meethub %s (commit %s, built %s, %s %s/%s)\n",
				buildinfo.Version, buildinfo.Commit, buildinfo.BuildTime, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return err
		},
	}
}
