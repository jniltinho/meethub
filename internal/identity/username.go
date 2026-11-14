package identity

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/database"
)

// Username rules follow Mattermost (server/public/model/user.go): 3–22
// characters, lowercase letters, digits, '.', '-' and '_', starting with a letter.
const (
	MinUsernameLen = 3
	MaxUsernameLen = 22
)

// Username errors.
var (
	ErrInvalidUsername = errors.New("username must be 3 to 22 lowercase letters, digits, '.', '-' or '_', starting with a letter, and not a reserved word")
	ErrUsernameTaken   = errors.New("username already in use")
)

var (
	usernamePattern  = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,21}$`)
	reservedNames    = []string{"all", "channel", "here", "system"}
	invalidNameChars = regexp.MustCompile(`[^a-z0-9._-]+`)
)

// NormalizeUsername lower-cases and trims a username and checks the rules.
func NormalizeUsername(s string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "@")))
	if !usernamePattern.MatchString(n) || slices.Contains(reservedNames, n) {
		return "", ErrInvalidUsername
	}
	return n, nil
}

// usernameBase derives a valid username stem from an e-mail's local part.
func usernameBase(email string) string {
	local, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	b := strings.Trim(invalidNameChars.ReplaceAllString(local, "."), ".-_")
	if b == "" || b[0] < 'a' || b[0] > 'z' {
		b = "user" + b
	}
	if len(b) < MinUsernameLen {
		b += strings.Repeat("0", MinUsernameLen-len(b))
	}
	if len(b) > MaxUsernameLen {
		b = strings.TrimRight(b[:MaxUsernameLen], ".-_")
	}
	if slices.Contains(reservedNames, b) {
		b = "user." + b
	}
	return b
}

// candidate returns the n-th candidate for base (n = 1: base itself, then base2, base3…).
func candidate(base string, n int) string {
	if n == 1 {
		return base
	}
	suffix := strconv.Itoa(n)
	if len(base)+len(suffix) > MaxUsernameLen {
		base = strings.TrimRight(base[:MaxUsernameLen-len(suffix)], ".-_")
	}
	return base + suffix
}

// AssignUsername gives the user a unique username derived from their e-mail
// (ana.silva, ana.silva2, …). It retries on a concurrent collision.
func (Store) AssignUsername(ctx context.Context, tx *gorm.DB, id, email string) (string, error) {
	base := usernameBase(email)
	var taken []string
	if err := tx.WithContext(ctx).Model(&User{}).Where("username_normalized LIKE ?", likePrefix(base)+"%").
		Pluck("username_normalized", &taken).Error; err != nil {
		return "", fmt.Errorf("assign username: %w", err)
	}
	for n := 1; n < 10_000; n++ {
		name := candidate(base, n)
		if slices.Contains(taken, name) {
			continue
		}
		err := database.Translate(tx.WithContext(ctx).Model(&User{}).Where("id = ?", id).
			Updates(map[string]any{"username": name, "username_normalized": name}).Error)
		if errors.Is(err, database.ErrDuplicate) { // taken concurrently: try the next one
			taken = append(taken, name)
			continue
		}
		if err != nil {
			return "", fmt.Errorf("assign username: %w", err)
		}
		return name, nil
	}
	return "", ErrUsernameTaken
}

// SetUsername changes a user's username.
func (Store) SetUsername(ctx context.Context, tx *gorm.DB, id, username string) (string, error) {
	n, err := NormalizeUsername(username)
	if err != nil {
		return "", err
	}
	err = database.Translate(tx.WithContext(ctx).Model(&User{}).Where("id = ?", id).
		Updates(map[string]any{"username": n, "username_normalized": n}).Error)
	if errors.Is(err, database.ErrDuplicate) {
		return "", ErrUsernameTaken
	}
	if err != nil {
		return "", fmt.Errorf("set username: %w", err)
	}
	return n, nil
}

func likePrefix(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

// PublicUser is how other users see a user outside administration: no e-mail,
// login times or organization role (user-directory spec).
type PublicUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Active      bool   `json:"active"`
	// LastPictureUpdate versions the profile picture (ms); 0: no picture
	LastPictureUpdate int64 `json:"last_picture_update"`
}

// Public returns the public profile of u.
func (u *User) Public() PublicUser {
	name := ""
	if u.Username != nil {
		name = *u.Username
	}
	return PublicUser{
		ID: strings.TrimSpace(u.ID), Username: name, DisplayName: u.DisplayName, Active: u.Status == StatusActive,
		LastPictureUpdate: u.LastPictureUpdate,
	}
}
