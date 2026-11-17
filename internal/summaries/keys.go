package summaries

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/recordings"
	"github.com/jniltinho/meethub/internal/sealed"
)

// The AI keys by the names the settings page shows (edit-ai-and-recording-settings, design D1).
const (
	KeyMain   = "ai.api_key"
	KeySpeech = "ai.transcription.api_key"
	maxKeyLen = 512
)

// keyColumn is where a key entered in Administration is kept, sealed and bound to that column's name.
var keyColumn = map[string]string{KeyMain: "ai_api_key", KeySpeech: "ai_transcription_api_key"}

// Errors of the keys form.
var (
	ErrNoSecretKey = errors.New("a key cannot be stored on this server: " + config.EnvPrefix + "SECRET_KEY is not set")
	ErrBadKey      = errors.New("the key must be one line of at most 512 characters without spaces")
	ErrUnknownKey  = errors.New("unknown key")
)

// KeyLockedError is a key set by the environment, which wins.
type KeyLockedError struct{ Key, Env string }

func (e *KeyLockedError) Error() string {
	return fmt.Sprintf("%s is set by %s: change it there", e.Key, e.Env)
}

// keys are the AI keys in use: the environment's, else the ones entered in Administration.
type keys struct {
	main, speech string
	stored       map[string]string // key name → "stored" or "unreadable", for what the database holds
}

// keyEnv is the variable of a key name.
func keyEnv(name string) string {
	return config.EnvPrefix + strings.ToUpper(strings.NewReplacer(".", "_").Replace(name))
}

// refresh reads the stored keys and returns the keys in use; every entry point calls it, so a key entered on one
// server is used by the others at their next request. A database error keeps what was known.
func (s *Service) refresh(ctx context.Context) *keys {
	var row struct{ AiAPIKey, AiTranscriptionAPIKey string }
	err := s.DB.Gorm.WithContext(ctx).Model(&org.Organization{}).
		Select("ai_api_key AS ai_api_key, ai_transcription_api_key AS ai_transcription_api_key").Limit(1).Scan(&row).Error
	if err != nil {
		if k := s.keys.Load(); k != nil {
			return k
		}
		row.AiAPIKey, row.AiTranscriptionAPIKey = "", ""
	}
	k := &keys{main: s.Config.APIKey, speech: s.Config.Transcription.APIKey, stored: map[string]string{}}
	open := func(name, text string) string {
		if text == "" {
			return ""
		}
		plain, err := sealed.Open(s.SecretKey, keyColumn[name], text)
		if err != nil || len(s.SecretKey) == 0 {
			k.stored[name] = "unreadable" // another MEETHUB_SECRET_KEY: it must be entered again
			return ""
		}
		k.stored[name] = "stored"
		return plain
	}
	if v := open(KeyMain, row.AiAPIKey); k.main == "" {
		k.main = v
	}
	if v := open(KeySpeech, row.AiTranscriptionAPIKey); k.speech == "" {
		k.speech = v
	}
	if s.Config.Transcription.BaseURL == "" {
		k.speech = k.main // one service for both
	}
	s.keys.Store(k)
	return k
}

// current is the keys as last read (none before the first read).
func (s *Service) current() *keys {
	if k := s.keys.Load(); k != nil {
		return k
	}
	return &keys{main: s.Config.APIKey, speech: s.Config.TranscriptionKey()}
}

// Ready re-reads the stored keys and reports whether summaries can be offered now.
func (s *Service) Ready(ctx context.Context) bool {
	s.refresh(ctx)
	return s.Available()
}

// SpeechKey and WriterKey are what the AI clients send, read at every request.
func (s *Service) SpeechKey() string { return s.current().speech }

// WriterKey is the key of the summary model's service.
func (s *Service) WriterKey() string { return s.current().main }

// KeyView is one AI key as Administration shows it: never its value.
type KeyView struct {
	Key        string `json:"key"`
	Set        bool   `json:"set"`
	Source     string `json:"source"`               // environment, stored or ""
	Env        string `json:"env"`                  // the variable that wins when set
	Locked     bool   `json:"locked"`               // set by the environment
	Unreadable bool   `json:"unreadable,omitempty"` // stored under another secret key: enter it again
	Needed     bool   `json:"needed"`               // the transcription key: only with ai.transcription.base_url
}

// KeysView is the keys form.
type KeysView struct {
	Keys     []KeyView `json:"keys"`
	Storable bool      `json:"storable"` // MEETHUB_SECRET_KEY is set
}

// GetKeys returns the state of the AI keys to someone with manage_system.
func (s *Service) GetKeys(ctx context.Context, actor org.Actor) (*KeysView, error) {
	if err := actor.Require(org.PermManageSystem); err != nil {
		return nil, err
	}
	k := s.refresh(ctx)
	v := &KeysView{Storable: len(s.SecretKey) > 0}
	for _, name := range []string{KeyMain, KeySpeech} {
		kv := KeyView{Key: name, Env: keyEnv(name), Needed: name == KeyMain || s.Config.Transcription.BaseURL != ""}
		switch env := map[string]string{KeyMain: s.Config.APIKey, KeySpeech: s.Config.Transcription.APIKey}[name]; {
		case env != "":
			kv.Set, kv.Source, kv.Locked = true, "environment", true
		case k.stored[name] == "stored":
			kv.Set, kv.Source = true, "stored"
		case k.stored[name] == "unreadable":
			kv.Unreadable = true
		}
		v.Keys = append(v.Keys, kv)
	}
	return v, nil
}

// SetKey stores an AI key entered in Administration, sealed, or removes it (value ""); the caller has
// re-authenticated the owner. Removing a key that summaries need turns them off first, as SetSetting does.
// Audited without the value.
func (s *Service) SetKey(ctx context.Context, actor org.Actor, name, value string) (*KeysView, error) {
	if err := actor.Require(org.PermManageSystem); err != nil {
		return nil, err
	}
	column, ok := keyColumn[name]
	if !ok {
		return nil, ErrUnknownKey
	}
	if env := map[string]string{KeyMain: s.Config.APIKey, KeySpeech: s.Config.Transcription.APIKey}[name]; env != "" {
		return nil, &KeyLockedError{Key: name, Env: keyEnv(name)}
	}
	value = strings.TrimSpace(value)
	action, text := "removed", ""
	if value != "" {
		if len(s.SecretKey) == 0 {
			return nil, ErrNoSecretKey
		}
		if len(value) > maxKeyLen || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return nil, ErrBadKey
		}
		var err error
		if text, err = sealed.Seal(s.SecretKey, column, value); err != nil {
			return nil, err
		}
		action = "set"
	}
	// removing a key summaries need turns them off in the same transaction, by what the organization stored and
	// not by what this server can read: a key under another MEETHUB_SECRET_KEY reads as off here already
	inUse := value == "" && (name == KeyMain || s.Config.Transcription.BaseURL != "")
	var cancelled []recordings.Recording
	turnedOff := false
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := org.LockOrganization(ctx, tx, s.DB.Dialect); err != nil {
			return err
		}
		if inUse {
			var was []string
			if err := tx.Model(&org.Organization{}).Pluck("meeting_summaries", &was).Error; err != nil {
				return fmt.Errorf("meeting summaries setting: %w", err)
			}
			if len(was) > 0 && was[0] != org.SummariesOff {
				var err error
				if cancelled, err = s.store(ctx, tx, actor, was[0], org.SummariesOff); err != nil {
					return err
				}
				turnedOff = true
			}
		}
		if err := tx.Model(&org.Organization{}).Where("singleton = 1").
			Updates(map[string]any{column: text, "updated_at": time.Now().UTC()}).Error; err != nil {
			return fmt.Errorf("ai key: %w", err)
		}
		return audit.Record(ctx, tx, actorEvent(actor, audit.ActionAIKeyChanged, "organization", "",
			map[string]any{"key": name, "action": action}))
	})
	if err != nil {
		return nil, err
	}
	if turnedOff {
		s.afterOff(ctx, cancelled)
	}
	return s.GetKeys(ctx, actor)
}

// keysPresent reports, in tx under the organization's lock, whether every key summaries need is set: by the
// environment or stored (readable or not: Available says whether this server can use it).
func (s *Service) keysPresent(ctx context.Context, tx *gorm.DB) (bool, error) {
	var row struct{ AiAPIKey, AiTranscriptionAPIKey string }
	if err := tx.WithContext(ctx).Model(&org.Organization{}).
		Select("ai_api_key AS ai_api_key, ai_transcription_api_key AS ai_transcription_api_key").Limit(1).Scan(&row).Error; err != nil {
		return false, fmt.Errorf("ai keys: %w", err)
	}
	main := s.Config.APIKey != "" || row.AiAPIKey != ""
	speech := s.Config.Transcription.BaseURL == "" || s.Config.Transcription.APIKey != "" || row.AiTranscriptionAPIKey != ""
	return main && speech, nil
}
