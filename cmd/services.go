package cmd

import (
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/httpserver/handlers"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/ratelimit"
	"github.com/jniltinho/meethub/internal/session"
)

// services are the domain services shared by the server and the CLI.
type services struct {
	auth  *auth.Service
	users *org.Users
	api   *handlers.API
}

// newServices wires the domain services; rdb may be nil (CLI: no rate limits).
func (c *cli) newServices(db *database.DB, logger *slog.Logger, rdb *redis.Client) (*services, error) {
	cfg, err := c.config()
	if err != nil {
		return nil, err
	}
	hasher := identity.NewHasher(identity.DefaultArgon2, 0, 0)
	authSvc := &auth.Service{
		DB:       db,
		Hasher:   hasher,
		Sessions: session.Store{Idle: cfg.Session.IdleTimeout.Std(), Absolute: cfg.Session.AbsoluteLifetime.Std()},
		Limiter:  ratelimit.NewLogins(rdb, logger, 0),
		DataDir:  cfg.App.DataDir,
		Logger:   logger,
	}
	users := &org.Users{DB: db, Hasher: hasher}
	return &services{
		auth:  authSvc,
		users: users,
		api: &handlers.API{
			Auth: authSvc, Users: users, DB: db,
			PublicOrigin: cfg.PublicOrigin(), SecureCookies: cfg.SecureCookies(),
		},
	}, nil
}
