package chat

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gorm.io/gorm"
)

var (
	codeBlockRe = regexp.MustCompile("(?s)```.*?(```|$)|~~~.*?(~~~|$)")
	codeSpanRe  = regexp.MustCompile("`[^`\n]*`")
	// a mention starts at the beginning or after a character that cannot be part of a name or an email
	mentionRe = regexp.MustCompile(`(?:^|[^\pL\pN@._-])@([\pL\pN._-]+)`)
)

// Mentions are the mention keywords and usernames found in a message, with
// code spans and blocks removed.
type Mentions struct {
	Usernames []string // normalized
	Channel   bool     // @channel or @all
	Here      bool
}

// ParseMentions finds @username, @channel, @all and @here in text; trailing
// '.', '-' and '_' are not part of a name (as in Mattermost).
func ParseMentions(text string) Mentions {
	text = codeBlockRe.ReplaceAllString(text, " ")
	text = codeSpanRe.ReplaceAllString(text, " ")
	var m Mentions
	for _, match := range mentionRe.FindAllStringSubmatch(text, -1) {
		name := strings.ToLower(strings.TrimRight(match[1], ".-_"))
		switch name {
		case "":
		case "channel", "all":
			m.Channel = true
		case "here":
			m.Here = true
		default:
			if !slices.Contains(m.Usernames, name) {
				m.Usernames = append(m.Usernames, name)
			}
		}
	}
	return m
}

// applyMentions counts one mention per mentioned member (never the author)
// under the channel lock and returns who was mentioned. In direct and group
// conversations every other member is mentioned.
func (s *Service) applyMentions(ctx context.Context, tx *gorm.DB, sub Subject, ch *Channel, text string) ([]string, error) {
	members, err := channelMemberIDs(ctx, tx, ch.ID)
	if err != nil {
		return nil, err
	}
	others := slices.DeleteFunc(slices.Clone(members), func(id string) bool { return id == sub.UserID })
	var ids []string
	switch ch.Type {
	case ChannelDirect, ChannelGroup:
		ids = others
	default:
		m := ParseMentions(text)
		set := map[string]bool{}
		if m.Channel {
			for _, id := range others {
				set[id] = true
			}
		}
		if m.Here && !m.Channel && s.Presence != nil && len(others) > 0 {
			online, err := s.Presence.Online(ctx, others)
			if err != nil {
				return nil, fmt.Errorf("presence for @here: %w", err)
			}
			for id := range online {
				set[id] = true
			}
		}
		if len(m.Usernames) > 0 {
			var named []string
			if err := tx.WithContext(ctx).Table("users u").
				Joins("JOIN channel_members cm ON cm.user_id = u.id AND cm.channel_id = ?", ch.ID).
				Where("u.username_normalized IN ?", m.Usernames).Pluck("u.id", &named).Error; err != nil {
				return nil, fmt.Errorf("mentioned users: %w", err)
			}
			for _, id := range trimAll(named) {
				set[id] = true
			}
		}
		delete(set, sub.UserID)
		for id := range set {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for chunk := range slices.Chunk(ids, 500) {
		if err := tx.WithContext(ctx).Exec(`UPDATE channel_members SET mention_count = mention_count + 1 WHERE channel_id = ? AND user_id IN ?`,
			ch.ID, chunk).Error; err != nil {
			return nil, fmt.Errorf("count mentions: %w", err)
		}
	}
	return ids, nil
}

// ViewChannel marks a channel or conversation read for the caller: the read
// position only moves forward and the mention count resets, under the channel
// lock so it is ordered with posts. The caller's other connections receive
// channel_viewed.
func (s *Service) ViewChannel(ctx context.Context, sub Subject, channelID string) error {
	var em emitter
	err := withChannelLock(ctx, s.DB, channelID, func(tx *gorm.DB, ch *Channel) error {
		a, _, err := accessTx(ctx, tx, sub, ch)
		if err != nil {
			return err
		}
		if !CanRead(sub, a) {
			return ReadError(sub, a)
		}
		now := s.now()
		if err := tx.WithContext(ctx).Exec(`UPDATE channel_members SET
			msg_count = CASE WHEN msg_count < ? THEN ? ELSE msg_count END,
			mention_count = 0,
			last_viewed_at = CASE WHEN last_viewed_at < ? THEN ? ELSE last_viewed_at END
			WHERE channel_id = ? AND user_id = ?`,
			ch.TotalMsgCount, ch.TotalMsgCount, now, now, ch.ID, sub.UserID).Error; err != nil {
			return fmt.Errorf("view channel: %w", err)
		}
		if ev := em.add("channel_viewed", map[string]any{"channel_id": ch.ID}, []string{sub.UserID}); ev != nil {
			ev.ChannelID, ev.TeamID = ch.ID, ch.TeamID
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, em.events)
	return nil
}
