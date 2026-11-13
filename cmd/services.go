package cmd

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/httpserver/handlers"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/ratelimit"
	"github.com/jniltinho/meethub/internal/session"
)

// services are the domain services shared by the server and the CLI.
type services struct {
	auth     *auth.Service
	users    *org.Users
	chat     *chat.Service
	sessions session.Store
	api      *handlers.API
}

// newServices wires the domain services; rdb may be nil (CLI: no rate limits).
func (c *cli) newServices(db *database.DB, logger *slog.Logger, rdb *redis.Client) (*services, error) {
	cfg, err := c.config()
	if err != nil {
		return nil, err
	}
	hasher := identity.NewHasher(identity.DefaultArgon2, 0, 0)
	sessions := session.Store{Idle: cfg.Session.IdleTimeout.Std(), Absolute: cfg.Session.AbsoluteLifetime.Std()}
	authSvc := &auth.Service{
		DB:       db,
		Hasher:   hasher,
		Sessions: sessions,
		Limiter:  ratelimit.NewLogins(rdb, logger, 0),
		DataDir:  cfg.App.DataDir,
		Logger:   logger,
	}
	authSvc.OnInitialize = func(ctx context.Context, tx *gorm.DB, orgName, ownerID string) error {
		return chat.EnsureMemberships(ctx, tx, db.Dialect, orgName, ownerID, org.RoleOwner)
	}
	users := &org.Users{DB: db, Hasher: hasher, OnCreate: func(ctx context.Context, tx *gorm.DB, userID, role string) error {
		o, err := org.Get(ctx, tx)
		if err != nil {
			return err
		}
		return chat.EnsureMemberships(ctx, tx, db.Dialect, o.Name, userID, role)
	}}
	return &services{
		auth:     authSvc,
		users:    users,
		chat:     &chat.Service{DB: db},
		sessions: sessions,
		api: &handlers.API{
			Auth: authSvc, Users: users, DB: db,
			PublicOrigin: cfg.PublicOrigin(), SecureCookies: cfg.SecureCookies(),
		},
	}, nil
}
