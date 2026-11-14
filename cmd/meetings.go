package cmd

import (
	"log/slog"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/meetings"
)

// newMeetings wires the meetings service; without LiveKit settings it is disabled (its routes answer 503).
func newMeetings(cfg *config.Config, db *database.DB, chatSvc *chat.Service, pub chat.Publisher, logger *slog.Logger) *meetings.Service {
	lk := cfg.LiveKit
	svc := &meetings.Service{DB: db, Chat: chatSvc, Publisher: pub, Logger: logger}
	if lk.Enabled() {
		svc.URL, svc.APIKey, svc.APISecret = lk.URL, lk.APIKey, lk.APISecret
		svc.Rooms = meetings.NewLiveKitRooms(lk.ServerURL(), lk.APIKey, lk.APISecret)
	}
	return svc
}
