package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/buildinfo"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/httpserver/handlers"
	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/web"
)

func newServerCmd(c *cli) *cobra.Command {
	var noMigrate bool
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the MeetHub server (HTTP API, web app and background workers)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return c.runServer(ctx, noMigrate)
		},
	}
	cmd.Flags().BoolVar(&noMigrate, "no-migrate", false, "do not apply pending migrations at startup")
	return cmd
}

// runServer wires the server's components and runs them until ctx ends.
func (c *cli) runServer(ctx context.Context, noMigrate bool) error {
	cfg, err := c.config()
	if err != nil {
		return err
	}
	logger := newLogger(c.errOut, cfg.Log)
	logger.Info("starting meethub", "version", buildinfo.Version, "env", cfg.App.Env, "database", cfg.Database.Driver)

	db, err := database.Open(ctx, database.SettingsFrom(cfg))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	migrator, err := database.NewMigrator(db, database.WithLogger(logger))
	if err != nil {
		return err
	}
	if !noMigrate {
		n, err := migrator.Up(ctx)
		if err != nil {
			return err
		}
		logger.Info("database migrated", "applied", n)
	}

	runner, err := jobs.New(ctx, jobs.Options{
		Redis: asynq.RedisClientOpt{
			Addr: cfg.Redis.Addr, Username: cfg.Redis.Username, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
		},
		ConnectTimeout: cfg.Redis.ConnectTimeout.Std(),
		Shutdown:       cfg.HTTP.ShutdownTimeout.Std(),
		Logger:         logger,
		DB:             db,
	})
	if err != nil {
		return err
	}
	defer func() { _ = runner.Close() }()
	runner.Register(auth.SessionCleanupTask(db, cfg.Session.IdleTimeout.Std(), time.Now))

	services, err := c.newServices(db, logger, runner.Redis())
	if err != nil {
		return err
	}
	if token, generated, err := services.auth.PrepareSetup(ctx, cfg.Setup.Token); err != nil {
		return err
	} else if token != "" {
		msg := "setup required: open the setup page and enter the setup token"
		if generated {
			logger.Warn(msg, "url", strings.TrimRight(cfg.HTTP.PublicURL, "/")+"/setup", "setup_token", token,
				"token_file", filepath.Join(cfg.App.DataDir, auth.SetupTokenFile))
		} else {
			logger.Warn(msg+" (MEETHUB_SETUP_TOKEN)", "url", strings.TrimRight(cfg.HTTP.PublicURL, "/")+"/setup")
		}
	}

	srv := httpserver.New(httpserver.Options{ //nolint:contextcheck // handlers use each request's own context
		Addr:            cfg.HTTP.Addr,
		SecureHeaders:   cfg.SecureCookies(),
		TrustedProxies:  cfg.TrustedProxyPrefixes(),
		BodyLimitBytes:  cfg.HTTP.BodyLimitBytes,
		ShutdownTimeout: cfg.HTTP.ShutdownTimeout.Std(),
		Logger:          logger,
		Dist:            web.Dist(),
		Themes:          handlers.ThemeResolver(db),
		ErrorMappers:    handlers.Mappers(),
		ReadyChecks: []httpserver.Check{
			{Name: "database", Check: database.ReadyCheck(db, migrator)},
			{Name: "redis", Check: runner.Ping},
		},
	})

	services.api.Register(srv.API())

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return srv.Run(gctx, nil) })
	g.Go(func() error { return runner.Run(gctx) })
	if err := g.Wait(); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	logger.Info("meethub stopped")
	return nil
}
