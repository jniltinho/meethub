// Package cmd defines the meethub command tree (Cobra): server, migrate, user,
// healthcheck and version. It is the composition root: it loads the
// configuration and wires the internal packages together.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/jniltinho/meethub/internal/config"
)

// cli carries the global flags and I/O shared by every command.
type cli struct {
	cfgFile string
	asJSON  bool
	in      io.Reader
	out     io.Writer
	errOut  io.Writer
	cfg     *config.Config
}

// Execute runs the CLI with the process arguments and exits with its status.
func Execute() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes the command line and returns the exit status (0 ok, 1 error).
func run(args []string, in io.Reader, out, errOut io.Writer) int {
	root := newRootCmd(&cli{in: in, out: out, errOut: errOut})
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(errOut, "error:", err)
		return 1
	}
	return 0
}

func newRootCmd(c *cli) *cobra.Command {
	root := &cobra.Command{
		Use:           "meethub",
		Short:         "MeetHub — team chat with LiveKit meetings",
		Long:          "MeetHub is a self-hosted team chat with LiveKit meetings: server, database migrations and administration.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&c.cfgFile, "config", "", "config file (default ./config.toml, then /etc/meethub/config.toml)")
	root.PersistentFlags().BoolVar(&c.asJSON, "json", false, "print JSON output")
	root.AddCommand(
		newVersionCmd(c),
		newHealthcheckCmd(c),
		newMigrateCmd(c),
	)
	return root
}

// config loads and validates the configuration once, on first use, so
// commands that do not need it (version) never touch it.
func (c *cli) config() (*config.Config, error) {
	if c.cfg != nil {
		return c.cfg, nil
	}
	cfg, err := config.Load(c.cfgFile)
	if err != nil {
		return nil, err
	}
	c.cfg = cfg
	return cfg, nil
}

// printJSON writes v as indented JSON.
func (c *cli) printJSON(v any) error {
	enc := json.NewEncoder(c.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
