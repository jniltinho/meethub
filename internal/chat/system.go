package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/database"
)

// Server-made posts (add-meeting-recordings, design D7): attributed to a person, made by the server alone.

// Types of server-made posts.
const (
	PostTypeRecording       = "recording"        // a meeting's recording, attached
	PostTypeRecordingFailed = "recording_failed" // a recording that could not be saved
	// add-meeting-summaries: replies in a recording's thread
	PostTypeSummary          = "meeting_summary"           // the AI summary, the transcript attached
	PostTypeSummaryRequested = "meeting_summary_requested" // someone asked for the summary
	PostTypeSummaryNoSpeech  = "meeting_summary_no_speech" // the recording has no speech to summarize
)

// SystemKeyPrefix starts the keys of server-made posts; clients may not use it for their pending_post_id.
const SystemKeyPrefix = "recording:"

// systemKeyPrefixes are every prefix reserved for server-made posts.
var systemKeyPrefixes = []string{SystemKeyPrefix, "summary:"}

func systemKey(k string) bool {
	for _, p := range systemKeyPrefixes {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// SystemPost is a post the server makes on someone's behalf.
type SystemPost struct {
	ChannelID string
	UserID    string // to whom it is attributed, member of the channel or not
	RootID    string // a reply in this post's thread (it must be a live root of the channel); "": a root
	Message   string // Markdown, rendered like any message; its mentions notify nobody
	Type      string
	Key       string         // deterministic: one post per key and user, ever
	Props     map[string]any // typed metadata set only by the server
	Files     []FileInfo     // stored with the post, in order
}

// CanRecord reports whether s may record a channel's meetings (design D7): a member who may read it and is its
// administrator — a channel administrator, or a team administrator or manage_system holder who is a member —; in
// direct and group conversations, and with anyMember, any member.
func CanRecord(s Subject, a Access, anyMember bool) bool {
	return CanRead(s, a) && (a.direct() || anyMember || CanAdminister(s, a))
}

// RecordingAccess returns the channel sub may read and whether they may record its meetings.
func (s *Service) RecordingAccess(ctx context.Context, sub Subject, channelID string, anyMember bool) (*Channel, bool, error) {
	tx := s.DB.Gorm.WithContext(ctx)
	ch, err := readChannel(ctx, tx, channelID)
	if err != nil {
		return nil, false, err
	}
	a, _, err := accessTx(ctx, tx, sub, ch)
	if err != nil {
		return nil, false, err
	}
	if !CanRead(sub, a) {
		return nil, false, ReadError(sub, a)
	}
	return ch, CanRecord(sub, a, anyMember), nil
}

// MayRecordTx is RecordingAccess's answer read in tx (one holding the channel's lock, as PublishSystemPost's inTx
// does), so a permission revoked meanwhile is seen.
func MayRecordTx(ctx context.Context, tx *gorm.DB, sub Subject, channelID string, anyMember bool) (bool, error) {
	ch, err := readChannel(ctx, tx, channelID)
	if err != nil {
		return false, err
	}
	a, _, err := accessTx(ctx, tx, sub, ch)
	if err != nil {
		return false, err
	}
	return CanRecord(sub, a, anyMember), nil
}

// PublishSystemPost stores a server-made post with its files under the channel's lock and announces it. It does
// not mark the channel read for its author, parse mentions or count against rate limits, and it is allowed in an
// archived channel. inTx runs in the same transaction (a failure rolls everything back). A post of the same key
// already stored is returned unchanged, without inTx or an event; created reports which happened.
func (s *Service) PublishSystemPost(ctx context.Context, in SystemPost, inTx func(tx *gorm.DB, p *PostView) error) (*PostView, bool, error) {
	if in.Key == "" || len(in.Key) > 64 {
		return nil, false, ErrInvalidInput
	}
	var (
		em      emitter
		out     *PostView
		created bool
	)
	err := withChannelLock(ctx, s.DB, in.ChannelID, func(tx *gorm.DB, ch *Channel) error {
		var prev Post
		err := tx.WithContext(ctx).Where("user_id = ? AND pending_post_id = ?", in.UserID, in.Key).Take(&prev).Error
		if err == nil {
			prev.trimIDs()
			if prev.ChannelID != ch.ID || prev.Type == "" {
				return fmt.Errorf("system post key %q is taken by another post", in.Key)
			}
			v, err := viewOf(ctx, tx, &prev)
			out = &v
			return err
		}
		if !database.IsNotFound(err) {
			return fmt.Errorf("read system post: %w", err)
		}
		now := s.now()
		key := in.Key
		p := &Post{ID: newID(), ChannelID: ch.ID, UserID: in.UserID, Type: in.Type, PendingPostID: &key,
			Message: in.Message, CreateAt: now, UpdateAt: now, Revision: ch.Revision}
		if in.RootID != "" {
			root, err := threadRoot(ctx, tx, ch.ID, in.RootID)
			if err != nil {
				return err
			}
			p.RootID = root.ID
		}
		if len(in.Props) > 0 {
			b, err := json.Marshal(in.Props)
			if err != nil {
				return fmt.Errorf("system post props: %w", err)
			}
			props := string(b)
			p.Props = &props
		}
		if err := tx.WithContext(ctx).Create(p).Error; err != nil {
			return fmt.Errorf("create system post: %w", err)
		}
		if err := tx.WithContext(ctx).Exec(`UPDATE channels SET total_msg_count = total_msg_count + 1, last_post_at = ? WHERE id = ?`,
			now, ch.ID).Error; err != nil {
			return fmt.Errorf("count post: %w", err)
		}
		if p.RootID != "" {
			if err := tx.WithContext(ctx).Exec(`UPDATE posts SET reply_count = reply_count + 1, last_reply_at = ?, update_at = ?, revision = ? WHERE id = ?`,
				now, now, ch.Revision, p.RootID).Error; err != nil {
				return fmt.Errorf("count reply: %w", err)
			}
		}
		if p.Message != "" {
			if err := indexPost(ctx, tx, s.DB.Dialect, p.ID, p.Message); err != nil {
				return err
			}
		}
		for i := range in.Files {
			f := in.Files[i]
			f.PostID, f.Position, f.ChannelID = &p.ID, i, ch.ID
			if err := tx.WithContext(ctx).Create(&f).Error; err != nil {
				return fmt.Errorf("store file info: %w", err)
			}
		}
		v, err := viewOf(ctx, tx, p)
		if err != nil {
			return err
		}
		if inTx != nil {
			if err := inTx(tx, &v); err != nil {
				return err
			}
		}
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		if ev := em.add("posted", map[string]any{
			"post": v, "channel_type": ch.Type, "channel_name": ch.Name, "team_id": ch.TeamID, "mentions": []string{},
		}, members); ev != nil {
			ev.ChannelID, ev.TeamID = ch.ID, ch.TeamID
		}
		out, created = &v, true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	s.publishAll(ctx, &em)
	return out, created, nil
}

// UpdateSystemPostProps merges patch into a server-made post's props (and runs inTx in the same transaction),
// announcing the post as edited. A deleted or missing post is left alone.
func (s *Service) UpdateSystemPostProps(ctx context.Context, postID string, patch map[string]any, inTx func(tx *gorm.DB) error) error {
	channelID, err := s.postChannel(ctx, postID)
	if err != nil {
		return err
	}
	var em emitter
	err = withChannelLock(ctx, s.DB, channelID, func(tx *gorm.DB, ch *Channel) error {
		p, err := lockedPost(ctx, tx, ch.ID, postID)
		if err != nil {
			return err
		}
		if inTx != nil {
			if err := inTx(tx); err != nil {
				return err
			}
		}
		if p.DeleteAt != 0 {
			return nil
		}
		props := map[string]any{}
		if p.Props != nil {
			_ = json.Unmarshal([]byte(*p.Props), &props)
		}
		maps.Copy(props, patch)
		b, err := json.Marshal(props)
		if err != nil {
			return err
		}
		now := s.now()
		raw := string(b)
		p.Props, p.UpdateAt, p.Revision = &raw, now, ch.Revision
		if err := tx.WithContext(ctx).Model(&Post{}).Where("id = ?", p.ID).
			Updates(map[string]any{"props": raw, "update_at": now, "revision": ch.Revision}).Error; err != nil {
			return fmt.Errorf("update post props: %w", err)
		}
		members, err := channelMemberIDs(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		v, err := viewOf(ctx, tx, p)
		if err != nil {
			return err
		}
		if ev := em.add("post_edited", map[string]any{"post": v}, members); ev != nil {
			ev.ChannelID, ev.TeamID = ch.ID, ch.TeamID
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publishAll(ctx, &em)
	return nil
}

// propsOf decodes a post's stored props.
func propsOf(raw *string) map[string]any {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(*raw), &m) != nil {
		return nil
	}
	return m
}
