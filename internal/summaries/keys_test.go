package summaries_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/jniltinho/meethub/internal/ai"
	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/recordings"
	"github.com/jniltinho/meethub/internal/summaries"
)

// keyed is a server without MEETHUB_AI_API_KEY: its key is the one entered in Administration.
func keyed(t *testing.T, w *world, secret []byte) *summaries.Service {
	t.Helper()
	s := w.service(t)
	s.Config.APIKey, s.SecretKey = "", secret
	s.Speech = &ai.Client{BaseURL: w.fake.URL(), KeyFunc: s.SpeechKey}
	s.Writer = &ai.Client{BaseURL: w.fake.URL(), KeyFunc: s.WriterKey}
	return s
}

func TestKeysFromAdministration(t *testing.T) {
	const entered = "sk-entered-in-the-page"
	secret := bytes.Repeat([]byte{0x51}, 32) // test-only key
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		owner := org.Actor{UserID: w.owner.UserID, Role: org.RoleOwner}
		a, b := keyed(t, w, secret), keyed(t, w, secret) // two servers on one database

		v, err := a.GetKeys(bg, owner)
		if err != nil || !v.Storable || len(v.Keys) != 2 || v.Keys[0].Set || v.Keys[0].Env != "MEETHUB_AI_API_KEY" ||
			v.Keys[1].Env != "MEETHUB_AI_TRANSCRIPTION_API_KEY" || v.Keys[1].Needed {
			t.Fatalf("empty: %+v %v", v, err)
		}
		if a.Ready(bg) {
			t.Fatal("available without a key")
		}
		for name, tt := range map[string]struct {
			s     *summaries.Service
			actor org.Actor
			key   string
			value string
			want  error
		}{
			"an admin":       {a, org.Actor{UserID: w.admin.UserID, Role: org.RoleAdmin}, summaries.KeyMain, entered, org.ErrForbidden},
			"unknown key":    {a, owner, "smtp.password", entered, summaries.ErrUnknownKey},
			"with a space":   {a, owner, summaries.KeyMain, "sk a", summaries.ErrBadKey},
			"too long":       {a, owner, summaries.KeyMain, strings.Repeat("k", 513), summaries.ErrBadKey},
			"no secret key":  {keyed(t, w, nil), owner, summaries.KeyMain, entered, summaries.ErrNoSecretKey},
			"an environment": {w.service(t), owner, summaries.KeyMain, entered, nil},
		} {
			_, err := tt.s.SetKey(bg, tt.actor, tt.key, tt.value)
			var locked *summaries.KeyLockedError
			if name == "an environment" {
				if !errors.As(err, &locked) || locked.Env != "MEETHUB_AI_API_KEY" {
					t.Fatalf("%s: %v", name, err)
				}
				continue
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("%s: %v", name, err)
			}
		}

		if v, err = a.SetKey(bg, owner, summaries.KeyMain, "  "+entered+"\n"); err != nil || !v.Keys[0].Set || v.Keys[0].Source != "stored" || v.Keys[0].Locked {
			t.Fatalf("set: %+v %v", v, err)
		}
		// the other server uses it at its next request, and what it sends is the key
		if !b.Ready(bg) {
			t.Fatal("the other server does not see the key")
		}
		if _, err := b.Writer.Chat(bg, ai.ChatRequest{Model: "llm", System: "s", User: "u", MaxTokens: 100}); err != nil {
			t.Fatal(err)
		}
		calls := w.fake.Calls()
		if got := calls[len(calls)-1].Auth; got != "Bearer "+entered {
			t.Fatalf("sent %q", got)
		}
		// sealed in the database, absent from the audit log
		var column string
		if err := db.Gorm.Raw(`SELECT ai_api_key FROM organizations`).Scan(&column).Error; err != nil || column == "" || strings.Contains(column, entered) {
			t.Fatalf("stored %q %v", column, err)
		}
		var events []audit.Event
		if err := db.Gorm.Where("action = ?", audit.ActionAIKeyChanged).Find(&events).Error; err != nil || len(events) != 1 {
			t.Fatalf("audit: %d %v", len(events), err)
		}
		if d := events[0].RawDetails; strings.Contains(d, entered) || !strings.Contains(d, `"set"`) || !strings.Contains(d, summaries.KeyMain) {
			t.Fatalf("audit details %s", d)
		}
		// another MEETHUB_SECRET_KEY: not set, to be entered again, nothing deleted
		other := keyed(t, w, bytes.Repeat([]byte{0x52}, 32))
		if v, err := other.GetKeys(bg, owner); err != nil || v.Keys[0].Set || !v.Keys[0].Unreadable || other.Ready(bg) {
			t.Fatalf("another secret key: %+v %v", v, err)
		}
		// a server that cannot read the key leaves a queued summary to the others: it never cancels shared work,
		// and removing the key it cannot read still turns the organization's setting off
		w.setting(t, org.SummariesAutomatic)
		rec := w.recording(t, org.SummariesAutomatic)
		if err := db.Gorm.Model(rec).Update("summary_status", recordings.SummaryQueued).Error; err != nil {
			t.Fatal(err)
		}
		if err := other.Attempt(bg, rec.ID, 1, false); !errors.Is(err, summaries.ErrNotConfigured) {
			t.Fatalf("attempt without a readable key: %v", err)
		}
		if got := w.row(t, rec.ID).SummaryStatus; got != recordings.SummaryQueued {
			t.Fatalf("the summary is %q after a server without the key tried it", got)
		}
		if _, err := other.SetKey(bg, owner, summaries.KeyMain, ""); err != nil {
			t.Fatal(err)
		}
		var persisted string
		_ = db.Gorm.Raw(`SELECT meeting_summaries FROM organizations`).Scan(&persisted).Error
		if persisted != org.SummariesOff || w.row(t, rec.ID).SummaryStatus != recordings.SummaryCancelled {
			t.Fatalf("removing an unreadable key: setting %q, summary %q", persisted, w.row(t, rec.ID).SummaryStatus)
		}
		// turning summaries on needs the key again, checked under the organization's lock
		if _, err := a.SetSetting(bg, owner, org.SummariesManual); !errors.Is(err, summaries.ErrNotConfigured) {
			t.Fatalf("on without a key: %v", err)
		}
		if _, err := a.SetKey(bg, owner, summaries.KeyMain, entered); err != nil {
			t.Fatal(err)
		}
		// removing the key summaries need turns them off, then removes it
		w.setting(t, org.SummariesManual)
		if got, _ := b.Setting(bg); got != org.SummariesManual {
			t.Fatalf("setting %q", got)
		}
		if v, err = a.SetKey(bg, owner, summaries.KeyMain, ""); err != nil || v.Keys[0].Set {
			t.Fatalf("remove: %+v %v", v, err)
		}
		var stored string
		_ = db.Gorm.Raw(`SELECT meeting_summaries FROM organizations`).Scan(&stored).Error
		if stored != org.SummariesOff || b.Ready(bg) {
			t.Fatalf("after removal: setting %q, available %v", stored, b.Ready(bg))
		}
		if err := db.Gorm.Raw(`SELECT ai_api_key FROM organizations`).Scan(&column).Error; err != nil || column != "" {
			t.Fatalf("still stored: %q %v", column, err)
		}
	})
}

// With ai.transcription.base_url set the speech service has its own key: both are needed.
func TestTranscriptionKeyFromAdministration(t *testing.T) {
	secret := bytes.Repeat([]byte{0x51}, 32) // test-only key
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		owner := org.Actor{UserID: w.owner.UserID, Role: org.RoleOwner}
		s := keyed(t, w, secret)
		s.Config.Transcription.BaseURL = w.fake.URL()
		if _, err := s.SetKey(bg, owner, summaries.KeyMain, "main-key"); err != nil {
			t.Fatal(err)
		}
		if v, _ := s.GetKeys(bg, owner); s.Ready(bg) || !v.Keys[1].Needed {
			t.Fatalf("available without the transcription key: %+v", v)
		}
		if _, err := s.SetKey(bg, owner, summaries.KeySpeech, "speech-key"); err != nil {
			t.Fatal(err)
		}
		if !s.Ready(bg) || s.SpeechKey() != "speech-key" || s.WriterKey() != "main-key" {
			t.Fatalf("keys %q %q", s.SpeechKey(), s.WriterKey())
		}
	})
}
