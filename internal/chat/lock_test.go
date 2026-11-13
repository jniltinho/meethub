package chat

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

var ctx = context.Background()

// seedChannel inserts a user and a public channel with that user as a member.
func seedChannel(t *testing.T, db *database.DB) (channelID, userID string) {
	t.Helper()
	now := time.Now().UTC()
	userID, channelID = "01JUSER00000000000000DIEGO", "01JCHAN000000000000FINANCE"
	exec := func(q string, args ...any) {
		t.Helper()
		if err := db.Gorm.Exec(q, args...).Error; err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, email, email_normalized, display_name, password_hash, status, created_at, updated_at)
		VALUES (?, 'd@x.io', 'd@x.io', 'Diego', 'h', 'active', ?, ?)`, userID, now, now)
	exec(`INSERT INTO channels (id, team_id, type, name, display_name, created_at, updated_at) VALUES (?, 'team1', 'P', 'finance', 'Finance', 1, 1)`, channelID)
	exec(`INSERT INTO channel_members (channel_id, user_id, role, created_at) VALUES (?, ?, 'channel_user', 1)`, channelID, userID)
	return channelID, userID
}

// postIfMember is a minimal "post": under the channel lock, insert only when
// the author is still a member, stamping the new revision.
func postIfMember(db *database.DB, channelID, userID, id string) (stored bool, rev int64, err error) {
	err = withChannelLock(ctx, db, channelID, func(tx *gorm.DB, ch *Channel) error {
		m, err := channelMember(ctx, tx, channelID, userID)
		if err != nil || m == nil {
			return err
		}
		stored, rev = true, ch.Revision
		return tx.Exec(`INSERT INTO posts (id, channel_id, user_id, message, create_at, update_at, revision) VALUES (?, ?, ?, 'x', 1, 1, ?)`,
			id, channelID, userID, ch.Revision).Error
	})
	return stored, rev, err
}

// removeHolding removes the member under the channel lock and keeps the
// transaction open until release is closed.
func removeHolding(db *database.DB, channelID, userID string, locked chan<- int64, release <-chan struct{}) error {
	return withChannelLock(ctx, db, channelID, func(tx *gorm.DB, ch *Channel) error {
		if err := tx.Exec(`DELETE FROM channel_members WHERE channel_id = ? AND user_id = ?`, channelID, userID).Error; err != nil {
			return err
		}
		locked <- ch.Revision
		<-release
		return nil
	})
}

func TestRemovalRacingAPost(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		t.Run("removal first", func(t *testing.T) {
			db := dbtest.Open(t, db.Dialect) // fresh database per sub-case
			channelID, userID := seedChannel(t, db)
			locked, release := make(chan int64), make(chan struct{})
			removed := make(chan error, 1)
			go func() { removed <- removeHolding(db, channelID, userID, locked, release) }()
			removalRev := <-locked // the removal holds the channel lock now
			posted := make(chan bool, 1)
			go func() {
				stored, _, err := postIfMember(db, channelID, userID, "01JPOST0000000000000000001")
				if err != nil {
					t.Error(err)
				}
				posted <- stored
			}()
			time.Sleep(200 * time.Millisecond) // the post is now waiting for the lock
			close(release)
			if err := <-removed; err != nil {
				t.Fatal(err)
			}
			if <-posted {
				t.Fatalf("a post committed after the removal (revision %d) was stored", removalRev)
			}
		})
		t.Run("post first", func(t *testing.T) {
			db := dbtest.Open(t, db.Dialect)
			channelID, userID := seedChannel(t, db)
			stored, postRev, err := postIfMember(db, channelID, userID, "01JPOST0000000000000000002")
			if err != nil || !stored {
				t.Fatalf("post before removal: %v %v", stored, err)
			}
			locked, release := make(chan int64, 1), make(chan struct{})
			close(release)
			if err := removeHolding(db, channelID, userID, locked, release); err != nil {
				t.Fatal(err)
			}
			if removalRev := <-locked; removalRev <= postRev {
				t.Fatalf("revisions out of commit order: post %d, removal %d", postRev, removalRev)
			}
		})
	})
}

func TestLockUnknownChannel(t *testing.T) {
	db := dbtest.Open(t, "sqlite")
	err := withChannelLock(ctx, db, "nope", func(*gorm.DB, *Channel) error { return nil })
	if err != ErrNotFound { //nolint:errorlint // sentinel returned directly
		t.Fatalf("got %v", err)
	}
}

// TestRevealConversation: a message makes a conversation visible to members not
// seeing it yet, once (used by posting).
func TestRevealConversation(t *testing.T) {
	db := dbtest.Open(t, "sqlite")
	channelID, userID := seedChannel(t, db)
	db.Gorm.Exec(`UPDATE channels SET type = 'D', team_id = '' WHERE id = ?`, channelID)
	db.Gorm.Exec(`UPDATE channel_members SET visible = ? WHERE channel_id = ?`, false, channelID)
	err := withChannelLock(ctx, db, channelID, func(tx *gorm.DB, ch *Channel) error {
		got, err := revealConversationTx(ctx, tx, ch)
		if err != nil || len(got) != 1 || got[0] != userID {
			t.Fatalf("first reveal: %v %v", got, err)
		}
		again, err := revealConversationTx(ctx, tx, ch)
		if err != nil || len(again) != 0 {
			t.Fatalf("second reveal: %v %v", again, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
