package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/buildinfo"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/diagnosis"
	"github.com/jniltinho/meethub/internal/files"
	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/httpserver/handlers"
	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/internal/meetings"
	"github.com/jniltinho/meethub/internal/notify"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/ratelimit"
	"github.com/jniltinho/meethub/internal/settings"
	"github.com/jniltinho/meethub/internal/summaries"
	"github.com/jniltinho/meethub/internal/tlsutil"
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
	} else if pending, err := migrator.HasPending(ctx); err != nil {
		return err
	} else if pending {
		return errors.New("the database has pending migrations: run meethub migrate up (or start without --no-migrate)")
	}
	// bring installs from before the chat up to date before serving (design D11)
	if rep, err := chat.Backfill(ctx, db); err != nil {
		return fmt.Errorf("backfill: %w", err)
	} else if rep != (chat.BackfillReport{}) {
		logger.Info("chat backfill", "usernames", rep.Usernames, "default_team", rep.DefaultTeam, "memberships", rep.Memberships)
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
	storage, err := files.NewStorage(cfg)
	if err != nil {
		return err
	}

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

	configFile, err := config.FindFile(c.cfgFile)
	if err != nil {
		return err
	}
	bootID := ulid.Make().String() // /ready reports it: the settings applier waits for a process started after its restart

	var tlsConfig *tls.Config
	if cert, _ := cfg.TLSFiles(); cert != "" {
		if tlsConfig, err = serverTLS(cfg, logger); err != nil {
			return err
		}
	}

	srv := httpserver.New(httpserver.Options{ //nolint:contextcheck // handlers use each request's own context
		Addr:            cfg.HTTP.Addr,
		TLS:             tlsConfig,
		SecureHeaders:   cfg.SecureCookies(),
		TrustedProxies:  cfg.TrustedProxyPrefixes(),
		BodyLimitBytes:  cfg.HTTP.BodyLimitBytes,
		BodyLimits:      handlers.BodyLimits(cfg.Files.MaxSizeBytes * int64(cfg.Files.MaxPerPost)),
		ShutdownTimeout: cfg.HTTP.ShutdownTimeout.Std(),
		Logger:          logger,
		Dist:            web.Dist(),
		Themes:          handlers.ThemeResolver(db),
		ErrorMappers:    handlers.Mappers(),
		ConnectSources:  cfg.LiveKit.BrowserOrigins(),
		BootID:          bootID,
		ReadyChecks: []httpserver.Check{
			{Name: "database", Check: database.ReadyCheck(db, migrator)},
			{Name: "redis", Check: runner.Ping},
			{Name: "storage", Check: storage.Check},
		},
	})

	rt := newRealtime(db, runner.Redis(), services.sessions, services.chat, logger)
	services.chat.MaxFilesPerPost = cfg.Files.MaxPerPost
	fileSvc := files.New(db, services.chat, storage, cfg.Files.MaxSizeBytes, cfg.Files.MaxPerPost, logger)
	fileSvc.Limiter = ratelimit.NewWindow(runner.Redis(), logger, "meethub:uploads:", files.UploadsPerMinute, time.Minute)
	runner.Register(fileSvc.CleanupTask(), services.chat.ActivityCleanupTask(), services.chat.SearchBackfillTask())
	services.api.Chat = services.chat
	services.api.Files = fileSvc
	services.api.Presence = rt.tracker
	services.api.Realtime = rt.hub
	services.api.SessionsChanged = rt.bus.SessionsChanged
	if cfg.SMTP.Enabled() {
		n := &notify.Service{
			DB: db.Gorm, Sender: &notify.SMTPSender{Config: cfg.SMTP}, From: cfg.SMTP.From, Presence: rt.tracker,
			PublicURL: cfg.HTTP.PublicURL, Excerpt: cfg.Notifications.EmailExcerpt, Catalog: notify.LoadCatalog(web.Dist()),
			Logger: logger,
		}
		services.chat.EmailNotifications = true
		services.api.Notify = n
		runner.Register(n.Task())
	}
	meet := newMeetings(cfg, db, services.chat, rt.bus, logger)
	services.api.Meetings = meet
	rec, err := newRecordings(ctx, cfg, db, services.chat, meet, storage, runner.Redis(), logger)
	if err != nil {
		return err
	}
	services.api.Recordings = rec
	sum := newSummaries(ctx, cfg, db, services.chat, rec, storage, runner.Redis(), logger)
	services.api.Summaries = sum
	var summaryRunner *jobs.Runner
	if rec.Enabled() {
		// summary tasks have their own queue and workers, never the lifecycle pool (add-meeting-summaries, design D5)
		summaryRunner, err = jobs.New(ctx, jobs.Options{
			Redis: asynq.RedisClientOpt{
				Addr: cfg.Redis.Addr, Username: cfg.Redis.Username, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
			},
			ConnectTimeout: cfg.Redis.ConnectTimeout.Std(),
			Shutdown:       cfg.HTTP.ShutdownTimeout.Std(),
			Queue:          summaries.Queue,
			Concurrency:    2,
			Logger:         logger,
			DB:             db,
		})
		if err != nil {
			return err
		}
		defer func() { _ = summaryRunner.Close() }()
		summaryRunner.Register(sum.Task(summaryTimeout(cfg)))
		sum.Queue = summaryRunner
	}
	var lifecycle *jobs.Runner
	if rec.Enabled() {
		// the sweeper and the retention have their own workers, so a saturated pool never delays them (design D9)
		lifecycle, err = jobs.New(ctx, jobs.Options{
			Redis: asynq.RedisClientOpt{
				Addr: cfg.Redis.Addr, Username: cfg.Redis.Username, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
			},
			ConnectTimeout: cfg.Redis.ConnectTimeout.Std(),
			Shutdown:       cfg.HTTP.ShutdownTimeout.Std(),
			Queue:          LifecycleQueue,
			Concurrency:    2,
			Logger:         logger,
			DB:             db,
		})
		if err != nil {
			return err
		}
		defer func() { _ = lifecycle.Close() }()
		lifecycle.Register(lifecycleTasks(rec, logger)...)
		// even without AI configured now: what earlier summaries derived still has to go when it is due
		lifecycle.Register(sum.LifecycleTasks()...)
	}
	set := settings.New(db.Gorm, cfg, configFile, bootID)
	set.Logger, set.Redis = logger, runner.Redis()
	set.Limit = ratelimit.NewWindow(runner.Redis(), logger, "meethub:settings:", 10, time.Hour)
	// with recordings off nothing finishes a recording under way: it is turned off after the last one is posted
	// and its captures are in recordings.dir: the directory is changed between recordings
	// and a new AI address is never used with summaries on: the key kept for the old service would go to the new
	// one before anyone tested it. Summaries are turned off first, then tested and turned on again
	set.Check = func(ctx context.Context, values map[string]any) error {
		for _, k := range []string{"ai.base_url", "ai.transcription.base_url"} {
			if _, ok := values[k]; !ok {
				continue
			}
			if v, err := sum.Setting(ctx); err != nil {
				return err
			} else if v != org.SummariesOff {
				return &settings.InvalidConfigError{Msg: k + ": turn meeting summaries off before changing the AI service's address, then test the connection and turn them on again"}
			}
		}
		on, off := values["recordings.enabled"].(bool)
		_, dir := values["recordings.dir"]
		if (!off || on) && !dir || !rec.Enabled() {
			return nil
		}
		n, err := rec.Unfinished(ctx)
		if err != nil {
			return err
		}
		switch {
		case n > 0 && dir:
			return &settings.InvalidConfigError{Msg: "recordings.dir: a recording is in progress or still being prepared: change the directory after it is posted"}
		case n > 0:
			return &settings.InvalidConfigError{Msg: "recordings.enabled: a recording is in progress or still being prepared: turn recordings off after it is posted"}
		}
		return nil
	}
	rec.Hold = set.HasPending
	rec.Frozen = func() bool { return set.PendingKey("recordings.dir") }
	services.api.Settings = set
	diag := &diagnosis.Service{Config: cfg, Summaries: sum.Ready, Applier: set.CanApply, Checks: []diagnosis.Check{
		{Name: "database", Check: database.ReadyCheck(db, migrator)},
		{Name: "redis", Check: runner.Ping},
		{Name: "storage", Check: storage.Check},
	}}
	if lk := cfg.LiveKit; lk.Enabled() {
		rooms := meetings.NewLiveKitRooms(lk.ServerURL(), lk.APIKey, lk.APISecret)
		diag.LiveKit = func(ctx context.Context) error { _, err := rooms.ListRooms(ctx); return err }
	}
	services.api.Diagnosis = diag
	services.api.Register(srv.API()) //nolint:contextcheck // handlers use each request's own context

	// Shutdown order: stop accepting (HTTP and upgrades), close the sockets
	// with 1001 within the shutdown timeout, then stop the realtime loops.
	loopCtx, stopLoops := context.WithCancel(context.WithoutCancel(ctx))
	defer stopLoops()
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return srv.Run(gctx, nil) })
	g.Go(func() error { return runner.Run(gctx) })
	g.Go(func() error {
		<-gctx.Done()
		sctx, cancel := context.WithTimeout(loopCtx, cfg.HTTP.ShutdownTimeout.Std())
		defer cancel()
		rt.hub.Shutdown(sctx)
		stopLoops()
		return nil
	})
	g.Go(func() error { return meet.Run(loopCtx) })
	g.Go(func() error { set.Run(gctx); return nil })
	if lifecycle == nil { // recordings off: what earlier summaries derived still goes when it is due
		g.Go(func() error { sum.CleanupLoop(gctx); return nil })
	}
	if lifecycle != nil {
		g.Go(func() error { return lifecycle.Run(gctx) })
		g.Go(func() error { return rec.Run(gctx) }) // on shutdown: captures stop, sync and release their leases
	}
	if summaryRunner != nil {
		g.Go(func() error { return summaryRunner.Run(gctx) })
		g.Go(func() error { return sum.Run(gctx) })
	}
	for _, loop := range rt.loops() {
		g.Go(func() error { return loop(loopCtx) })
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	logger.Info("meethub stopped")
	return nil
}

// serverTLS loads the certificate the server speaks HTTPS with, generating the self-signed one first when that is
// what the install asked for.
func serverTLS(cfg *config.Config, logger *slog.Logger) (*tls.Config, error) {
	cert, key := cfg.TLSFiles()
	selfSigned := cfg.HTTP.CertFile == ""
	if selfSigned {
		created, err := tlsutil.EnsureSelfSigned(cert, key, []string{cfg.PublicHost()}, time.Now())
		if err != nil {
			return nil, fmt.Errorf("self-signed certificate in %s: %w", filepath.Dir(cert), err)
		}
		if created {
			logger.Warn("generated a self-signed certificate: browsers warn until it is trusted on each device",
				"certificate", cert)
		}
	}
	tc, fingerprint, err := tlsutil.Load(cert, key)
	if err != nil {
		return nil, fmt.Errorf("http.cert_file and http.key_file: %w", err)
	}
	logger.Info("serving https", "certificate", cert, "sha256", fingerprint, "self_signed", selfSigned)
	return tc, nil
}
