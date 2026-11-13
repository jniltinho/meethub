package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/jniltinho/meethub/internal/database"
)

func newMigrateCmd(c *cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply, revert or list database migrations",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Apply every pending migration",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return c.withMigrator(cmd, func(m *database.Migrator) error {
					n, err := m.Up(cmd.Context())
					if err != nil {
						return err
					}
					if c.asJSON {
						return c.printJSON(map[string]int{"applied": n})
					}
					if n == 0 {
						_, err = fmt.Fprintln(c.out, "nothing pending: the database is up to date")
					} else {
						_, err = fmt.Fprintf(c.out, "applied %d migration(s)\n", n)
					}
					return err
				})
			},
		},
		&cobra.Command{
			Use:   "down",
			Short: "Revert the last applied migration",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return c.withMigrator(cmd, func(m *database.Migrator) error {
					st, err := m.Down(cmd.Context())
					if err != nil {
						return err
					}
					if c.asJSON {
						return c.printJSON(map[string]any{"reverted": st.Name, "version": st.Version})
					}
					_, err = fmt.Fprintf(c.out, "reverted %s\n", st.Name)
					return err
				})
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "List applied and pending migrations",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return c.withMigrator(cmd, func(m *database.Migrator) error {
					st, err := m.Status(cmd.Context())
					if err != nil {
						return err
					}
					if c.asJSON {
						return c.printJSON(st)
					}
					tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
					_, _ = fmt.Fprintln(tw, "VERSION\tNAME\tSTATE\tAPPLIED AT")
					for _, s := range st {
						state, at := "pending", ""
						if s.Applied {
							state, at = "applied", s.AppliedAt.Format(time.RFC3339)
						}
						_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", s.Version, s.Name, state, at)
					}
					return tw.Flush()
				})
			},
		},
	)
	return cmd
}

// withMigrator opens the database, runs fn with a Migrator and closes it.
func (c *cli) withMigrator(cmd *cobra.Command, fn func(*database.Migrator) error) error {
	cfg, db, err := c.openDatabase(cmd.Context())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	m, err := database.NewMigrator(db, database.WithLogger(newLogger(c.errOut, cfg.Log)))
	if err != nil {
		return err
	}
	return fn(m)
}
