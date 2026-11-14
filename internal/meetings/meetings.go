// Package meetings connects channels to LiveKit (design of add-livekit-meetings): one persistent room per channel
// and conversation, short-lived access tokens for the members who may read the channel, who is in each room (from
// LiveKit's webhooks, corrected by reconciliation) and the eviction of people who lost access.
package meetings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/livekit/protocol/auth"
	"gorm.io/gorm/clause"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/database"
)

// TokenTTL is how long an access token can be used to connect; LiveKit refreshes the tokens of connected
// participants itself.
const TokenTTL = 2 * time.Minute

// ErrDisabled answers the meetings routes when LiveKit is not configured.
var ErrDisabled = errors.New("meetings are not configured")

// Room is a channel's persistent meeting room; Name is its random LiveKit room name.
type Room struct {
	ChannelID string `gorm:"primaryKey;size:26"`
	Name      string
	CreatedAt int64 `gorm:"autoCreateTime:false"`
}

// TableName maps Room to meeting_rooms.
func (Room) TableName() string { return "meeting_rooms" }

// Participant is a user in a room, by LiveKit participant session (SID).
type Participant struct {
	ChannelID string `gorm:"primaryKey;size:26"`
	UserID    string `gorm:"primaryKey;size:26"`
	SID       string `gorm:"column:sid"`
	JoinedAt  int64
}

// TableName maps Participant to meeting_participants.
func (Participant) TableName() string { return "meeting_participants" }

// Chat is what meetings need from the chat: its read rule and a channel's members.
type Chat interface {
	ReadableChannel(ctx context.Context, sub chat.Subject, channelID string) (*chat.Channel, error)
	MemberIDs(ctx context.Context, channelID string) ([]string, error)
}

// Service issues tokens and keeps room presence. URL, APIKey and APISecret empty: meetings are disabled.
type Service struct {
	DB        *database.DB
	Chat      Chat
	Publisher chat.Publisher // nil: events are dropped
	Rooms     RoomService    // LiveKit's room API; nil: reconciliation only works on the database
	URL       string         // the URL browsers connect to
	APIKey    string
	APISecret string
	Logger    *slog.Logger
	Now       func() time.Time
}

// Enabled reports whether LiveKit is configured.
func (s *Service) Enabled() bool {
	return s != nil && s.URL != "" && s.APIKey != "" && s.APISecret != ""
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Token is what a browser needs to connect to a room.
type Token struct {
	URL       string `json:"url"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"` // Unix milliseconds
}

// IssueToken gives sub a token for the room of a channel they may read (not archived), named name in the room.
func (s *Service) IssueToken(ctx context.Context, sub chat.Subject, name, channelID string) (*Token, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	ch, err := s.Chat.ReadableChannel(ctx, sub, channelID)
	if err != nil {
		return nil, err
	}
	if ch.ArchivedAt != 0 {
		return nil, chat.ErrArchived
	}
	room, err := s.ensureRoom(ctx, ch.ID)
	if err != nil {
		return nil, err
	}
	grant := &auth.VideoGrant{RoomJoin: true, Room: room}
	grant.SetCanPublish(true)
	grant.SetCanSubscribe(true)
	grant.SetCanPublishData(true)
	grant.SetCanUpdateOwnMetadata(true)
	at := auth.NewAccessToken(s.APIKey, s.APISecret).
		SetVideoGrant(grant).SetIdentity(sub.UserID).SetName(name).SetValidFor(TokenTTL)
	jwt, err := at.ToJWT()
	if err != nil {
		return nil, fmt.Errorf("sign meeting token: %w", err)
	}
	return &Token{URL: s.URL, Token: jwt, ExpiresAt: s.now().Add(TokenTTL).UnixMilli()}, nil
}

// ensureRoom returns the channel's room name, creating the room with a random name on first use; concurrent first
// uses agree on one row.
func (s *Service) ensureRoom(ctx context.Context, channelID string) (string, error) {
	tx := s.DB.Gorm.WithContext(ctx)
	var r Room
	err := tx.Where("channel_id = ?", channelID).Take(&r).Error
	if err == nil {
		return r.Name, nil
	}
	if !database.IsNotFound(err) {
		return "", fmt.Errorf("read meeting room: %w", err)
	}
	name, err := randomName()
	if err != nil {
		return "", err
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&Room{ChannelID: channelID, Name: name, CreatedAt: s.now().UnixMilli()}).Error; err != nil {
		return "", fmt.Errorf("create meeting room: %w", err)
	}
	if err := tx.Where("channel_id = ?", channelID).Take(&r).Error; err != nil {
		return "", fmt.Errorf("read meeting room: %w", err)
	}
	return r.Name, nil
}

// randomName is "mh_" and 128 random bits: nothing about the channel can be derived from it, nor it from the channel.
func randomName() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("room name: %w", err)
	}
	return "mh_" + hex.EncodeToString(b), nil
}

// roomByName finds the channel of a LiveKit room ("" when it is not ours).
func (s *Service) roomByName(ctx context.Context, name string) (string, error) {
	var ids []string
	if err := s.DB.Gorm.WithContext(ctx).Model(&Room{}).Where("name = ?", name).Limit(1).
		Pluck("channel_id", &ids).Error; err != nil {
		return "", fmt.Errorf("find meeting room: %w", err)
	}
	if len(ids) == 0 {
		return "", nil
	}
	return strings.TrimSpace(ids[0]), nil
}
