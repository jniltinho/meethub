package chat

import (
	"context"
	"errors"
	"time"

	"github.com/jniltinho/meethub/internal/database"
)

// More domain errors.
var (
	ErrNameTaken      = errors.New("name already in use")
	ErrDefaultTeam    = errors.New("the default team cannot be archived or left by its last team admin")
	ErrDefaultChannel = errors.New("the default channel cannot be archived or left")
	ErrNotTeamMember  = errors.New("the user is not a member of the channel's team")
	ErrArchived       = errors.New("archived")
	ErrInvalidRole    = errors.New("invalid role")
	ErrUserInactive   = errors.New("the user is not active")
	ErrDirectChannel  = errors.New("direct and group conversations have fixed members")
)

// Event is a real-time event (Mattermost name and data) with its recipients,
// resolved inside the writing transaction (design D5).
type Event struct {
	Name       string         `json:"event"`
	Data       map[string]any `json:"data"`
	Recipients []string       `json:"-"`
	ChannelID  string         `json:"-"` // set for channel-scoped events (queued-event purge on removal)
	TeamID     string         `json:"-"`
}

// Publisher delivers events after the transaction that produced them commits.
type Publisher interface {
	Publish(ctx context.Context, events ...Event)
}

// Service implements the chat operations.
type Service struct {
	DB        *database.DB
	Publisher Publisher // nil: events are dropped (CLI, tests)
	Now       func() time.Time
}

func (s *Service) now() int64 {
	if s.Now != nil {
		return nowMillis(s.Now())
	}
	return nowMillis(time.Now())
}

func (s *Service) publish(ctx context.Context, events []Event) {
	if s.Publisher != nil && len(events) > 0 {
		s.Publisher.Publish(context.WithoutCancel(ctx), events...)
	}
}

// emitter collects the events of one transaction.
type emitter struct{ events []Event }

func (e *emitter) add(name string, data map[string]any, recipients []string) *Event {
	if len(recipients) == 0 {
		return nil
	}
	e.events = append(e.events, Event{Name: name, Data: data, Recipients: recipients})
	return &e.events[len(e.events)-1]
}
