package cmd

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
)

// newLogger builds the structured logger from log.level and log.format.
func newLogger(w io.Writer, c config.Log) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(strings.ToUpper(c.Level)))
	opts := &slog.HandlerOptions{Level: level}
	if c.Format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// openDatabase loads the configuration and connects to the database.
func (c *cli) openDatabase(ctx context.Context) (*config.Config, *database.DB, error) {
	cfg, err := c.config()
	if err != nil {
		return nil, nil, err
	}
	db, err := database.Open(ctx, database.SettingsFrom(cfg))
	if err != nil {
		return nil, nil, err
	}
	return cfg, db, nil
}
