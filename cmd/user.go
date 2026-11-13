package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
)

func newUserCmd(c *cli) *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Create users, reset passwords and list users", Args: cobra.NoArgs}
	var passwordStdin bool
	cmd.PersistentFlags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from standard input instead of a prompt")

	var email, name, role, orgName string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a user; on an uninitialized install, the first owner and the organization (--org-name)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return c.withServices(cmd.Context(), func(ctx context.Context, s *services) error {
				initialized, err := s.auth.Initialized(ctx)
				if err != nil {
					return err
				}
				if !initialized && (role != org.RoleOwner || strings.TrimSpace(orgName) == "") {
					return auth.ErrFirstUserOwner
				}
				pw, err := c.readPassword(passwordStdin, true)
				if err != nil {
					return err
				}
				var u *identity.User
				if !initialized {
					var o *org.Organization
					if o, u, err = s.auth.Initialize(ctx, auth.InitInput{OrganizationName: orgName, DisplayName: name, Email: email, Password: pw}, "cli"); err != nil {
						return err
					}
					_, _ = fmt.Fprintf(c.errOut, "initialized organization %q (slug %s)\n", o.Name, o.Slug)
				} else {
					m, err := s.users.Create(ctx, org.SystemActor, org.NewUser{Email: email, DisplayName: name, Role: role, Password: pw})
					if err != nil {
						return err
					}
					u = &m.User
				}
				if c.asJSON {
					return c.printJSON(org.Member{User: *u, Role: role})
				}
				_, err = fmt.Fprintf(c.out, "created %s <%s> as %s (id %s)\n", u.DisplayName, u.Email, role, u.ID)
				return err
			})
		},
	}
	create.Flags().StringVar(&email, "email", "", "e-mail address (required)")
	create.Flags().StringVar(&name, "name", "", "display name (required)")
	create.Flags().StringVar(&role, "role", org.RoleMember, "owner, admin, member or guest")
	create.Flags().StringVar(&orgName, "org-name", "", "organization name (required for the first user)")
	_ = create.MarkFlagRequired("email")
	_ = create.MarkFlagRequired("name")

	var resetEmail string
	setPassword := &cobra.Command{
		Use:   "set-password",
		Short: "Set a user's password and end all of their sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return c.withServices(cmd.Context(), func(ctx context.Context, s *services) error {
				u, err := (identity.Store{}).ByEmail(ctx, s.auth.DB.Gorm, resetEmail)
				if err != nil {
					return err
				}
				pw, err := c.readPassword(passwordStdin, true)
				if err != nil {
					return err
				}
				if err := s.users.ResetPassword(ctx, org.SystemActor, u.ID, pw); err != nil {
					return err
				}
				_, err = fmt.Fprintf(c.out, "password changed for %s; all of their sessions were ended\n", u.Email)
				return err
			})
		},
	}
	setPassword.Flags().StringVar(&resetEmail, "email", "", "e-mail address (required)")
	_ = setPassword.MarkFlagRequired("email")

	list := &cobra.Command{
		Use:   "list",
		Short: "List users",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return c.withServices(cmd.Context(), func(ctx context.Context, s *services) error {
				var all []org.Member
				cursor := ""
				for {
					page, err := s.users.List(ctx, org.SystemActor, org.ListFilter{Limit: 200, Cursor: cursor})
					if err != nil {
						return err
					}
					all = append(all, page.Users...)
					if cursor = page.NextCursor; cursor == "" {
						break
					}
				}
				if c.asJSON {
					if all == nil {
						all = []org.Member{}
					}
					return c.printJSON(all)
				}
				tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
				_, _ = fmt.Fprintln(tw, "ID\tNAME\tE-MAIL\tROLE\tSTATUS")
				for _, m := range all {
					_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", m.ID, m.DisplayName, m.Email, m.Role, m.Status)
				}
				return tw.Flush()
			})
		},
	}
	cmd.AddCommand(create, setPassword, list)
	return cmd
}

// withServices opens the database (migrations must be applied) and runs fn.
func (c *cli) withServices(ctx context.Context, fn func(context.Context, *services) error) error {
	cfg, db, err := c.openDatabase(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	m, err := database.NewMigrator(db)
	if err != nil {
		return err
	}
	if pending, err := m.HasPending(ctx); err != nil {
		return err
	} else if pending {
		return errors.New("the database has pending migrations: run meethub migrate up first")
	}
	if _, err := chat.Backfill(ctx, db); err != nil {
		return fmt.Errorf("backfill: %w", err)
	}
	s, err := c.newServices(db, newLogger(c.errOut, cfg.Log), nil)
	if err != nil {
		return err
	}
	return fn(ctx, s)
}

// readPassword reads a password from stdin (--password-stdin) or from a
// terminal prompt without echo; it never takes one from the arguments.
func (c *cli) readPassword(fromStdin, confirm bool) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(c.in).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	f, ok := c.in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) { //nolint:gosec // file descriptors fit in int
		return "", errors.New("no terminal to prompt for the password: use --password-stdin")
	}
	read := func(prompt string) (string, error) {
		_, _ = fmt.Fprint(c.errOut, prompt)
		b, err := term.ReadPassword(int(f.Fd())) //nolint:gosec // file descriptors fit in int
		_, _ = fmt.Fprintln(c.errOut)
		return string(b), err
	}
	pw, err := read("Password: ")
	if err != nil {
		return "", err
	}
	if confirm {
		again, err := read("Repeat password: ")
		if err != nil {
			return "", err
		}
		if again != pw {
			return "", errors.New("the passwords do not match")
		}
	}
	return pw, nil
}
