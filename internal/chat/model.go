// Package chat implements MeetHub's team chat on the Mattermost model
// (baseline v12.0.2): teams, channels, direct and group conversations, posts,
// reactions and read state, with authorization and per-channel serialization.
package chat

// Portions adapted from Mattermost (Apache-2.0): server/public/model/channel.go
// (GetDMNameFromIds, GetGroupNameFromUserIds, channel limits and name rules).
// Copyright (c) 2015-present Mattermost, Inc. See NOTICE.

import (
	"crypto/sha1" //nolint:gosec // a name, not a security digest (Mattermost compatibility)
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Channel types, as in Mattermost.
const (
	ChannelOpen    = "O"
	ChannelPrivate = "P"
	ChannelDirect  = "D"
	ChannelGroup   = "G"
)

// Limits, as in Mattermost.
const (
	ChannelNameMaxLength       = 64
	ChannelDisplayNameMaxRunes = 64
	ChannelPurposeMaxRunes     = 250
	ChannelHeaderMaxRunes      = 1024
	TeamNameMaxLength          = 64
	TeamDisplayNameMaxRunes    = 64
	TeamDescriptionMaxRunes    = 255
	PostMessageMaxRunes        = 16383
	GroupMinMembers            = 3
	GroupMaxMembers            = 8
)

// Validation errors.
var (
	ErrInvalidName        = errors.New("name must be 2 to 64 lowercase letters, digits, '-' or '_'")
	ErrInvalidDisplayName = errors.New("display name must be 1 to 64 characters")
	ErrTooLong            = errors.New("text is too long")
)

var (
	channelNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)
	teamNamePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
)

// GetDMNameFromIDs (Mattermost: GetDMNameFromIds) is the name of the direct channel between two users: the
// sorted ids joined by "__" (a self conversation repeats the id).
func GetDMNameFromIDs(userID1, userID2 string) string {
	if userID1 > userID2 {
		return userID2 + "__" + userID1
	}
	return userID1 + "__" + userID2
}

// GetGroupNameFromUserIDs (Mattermost: GetGroupNameFromUserIds) is the name of the group channel of a set of users:
// the hex SHA-1 of the sorted ids (order-independent).
func GetGroupNameFromUserIDs(userIDs []string) string {
	ids := slices.Clone(userIDs)
	slices.Sort(ids)
	h := sha1.New() //nolint:gosec // see import
	for _, id := range ids {
		_, _ = h.Write([]byte(id))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NormalizeChannelName lower-cases and checks a channel URL name.
func NormalizeChannelName(s string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(s))
	if !channelNamePattern.MatchString(n) {
		return "", ErrInvalidName
	}
	return n, nil
}

// NormalizeTeamName lower-cases and checks a team URL name.
func NormalizeTeamName(s string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(s))
	if !teamNamePattern.MatchString(n) {
		return "", ErrInvalidName
	}
	return n, nil
}

// CheckDisplayName validates a team or channel display name.
func CheckDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n == 0 || n > ChannelDisplayNameMaxRunes {
		return "", ErrInvalidDisplayName
	}
	return s, nil
}

// CheckText validates free text (purpose, header, description) against a rune limit.
func CheckText(s string, maxRunes int) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxRunes {
		return "", ErrTooLong
	}
	return s, nil
}

// SlugFromDisplayName derives a URL name from a display name ("Dev Ops!" → "dev-ops").
func SlugFromDisplayName(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > ChannelNameMaxLength {
		out = strings.TrimRight(out[:ChannelNameMaxLength], "-")
	}
	if len(out) < 2 {
		out = "channel-" + out
	}
	return out
}
