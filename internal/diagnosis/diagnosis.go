// Package diagnosis reports whether the parts of a MeetHub server are ready, for the setup wizard and for
// Administration (add-setup-wizard, design D2): each part as ok, a note, a warning or a problem. It carries no
// secret and no setting's value beyond the kind of database and of file storage.
package diagnosis

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/jniltinho/meethub/internal/config"
)

// Levels of an item.
const (
	OK      = "ok"
	Note    = "note"    // an optional part that is not set up
	Warning = "warning" // works, but not as a real install should
	Problem = "problem" // MeetHub cannot be used until it is fixed
)

// Item is one part of the server.
type Item struct {
	ID     string `json:"id"`
	Level  string `json:"level"`
	Detail string `json:"detail,omitempty"` // the kind of database or storage; never a value from a secret
}

// Check is a named readiness check (the server's own: database, redis, storage).
type Check struct {
	Name  string
	Check func(ctx context.Context) error
}

// Service runs the diagnosis of one server.
type Service struct {
	Config *config.Config
	Checks []Check
	// LiveKit asks the LiveKit server for its rooms with the configured key; nil: meetings are off.
	LiveKit func(ctx context.Context) error
	// Summaries reports whether meeting summaries can be offered now (a key from either place, both models).
	Summaries func(ctx context.Context) bool
	// Applier reports whether Administration → Settings can apply changes on this host.
	Applier func() bool
}

const stepTimeout = 5 * time.Second

// Run checks every part, in the order the wizard shows them.
func (s *Service) Run(ctx context.Context) []Item {
	cfg := s.Config
	var out []Item
	add := func(id, level, detail string) { out = append(out, Item{ID: id, Level: level, Detail: detail}) }
	level := func(ok bool, otherwise string) string {
		if ok {
			return OK
		}
		return otherwise
	}
	detail := map[string]string{"database": cfg.Database.Driver, "storage": cfg.Files.Driver}
	for _, c := range s.Checks {
		cctx, cancel := context.WithTimeout(ctx, stepTimeout)
		err := c.Check(cctx)
		cancel()
		add(c.Name, level(err == nil, Problem), detail[c.Name])
	}
	add("https", level(cfg.SecureCookies() || localHost(cfg.HTTP.PublicURL), Warning), "")
	add("mode", level(cfg.Production(), Note), "")
	if s.LiveKit == nil {
		add("meetings", Note, "")
	} else {
		cctx, cancel := context.WithTimeout(ctx, stepTimeout)
		err := s.LiveKit(cctx)
		cancel()
		add("meetings", level(err == nil, Warning), "")
	}
	add("recordings", level(cfg.Recordings.Enabled, Note), "") // on means the startup probe passed
	add("email", level(cfg.SMTP.Enabled(), Note), "")
	add("secret_key", level(cfg.Secret.Bytes() != nil, Warning), "")
	add("ai", level(s.Summaries != nil && s.Summaries(ctx), Note), "")
	add("applier", level(s.Applier != nil && s.Applier(), Note), "")
	return out
}

// localHost reports whether the public URL is this machine: plain HTTP is expected there.
func localHost(public string) bool {
	u, err := url.Parse(public)
	if err != nil {
		return false
	}
	h := u.Hostname()
	ip := net.ParseIP(h)
	return h == "localhost" || ip != nil && ip.IsLoopback()
}
