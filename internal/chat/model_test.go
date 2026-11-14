package chat_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jniltinho/meethub/internal/chat"
)

// The expected values reproduce Mattermost's own behavior for the same inputs.
func TestDMAndGroupNames(t *testing.T) {
	t.Parallel()
	a, b := "aaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbb"
	if got := chat.GetDMNameFromIDs(b, a); got != a+"__"+b {
		t.Fatalf("DM name %q", got)
	}
	if chat.GetDMNameFromIDs(a, b) != chat.GetDMNameFromIDs(b, a) || len(chat.GetDMNameFromIDs(a, b)) > chat.ChannelNameMaxLength {
		t.Fatal("DM name not symmetric or too long")
	}
	if got := chat.GetDMNameFromIDs(a, a); got != a+"__"+a {
		t.Fatalf("self DM %q", got)
	}
	ids := []string{"c", "a", "b"}
	g1, g2 := chat.GetGroupNameFromUserIDs(ids), chat.GetGroupNameFromUserIDs([]string{"b", "c", "a"})
	// sha1("abc") = a9993e364706816aba3e25717850c26c9cd0d89d
	if g1 != g2 || g1 != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Fatalf("group names %q %q", g1, g2)
	}
	if ids[0] != "c" {
		t.Fatal("caller's slice was sorted in place")
	}
}

func TestNameRules(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"DevOps": "devops", "town-square": "town-square", "q_a": "q_a", "42": "42"} {
		if got, err := chat.NormalizeChannelName(in); err != nil || got != want {
			t.Errorf("channel %q → %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"a", "-dev", "dev ops", strings.Repeat("a", 65), "déjà"} {
		if _, err := chat.NormalizeChannelName(bad); !errors.Is(err, chat.ErrInvalidName) {
			t.Errorf("channel %q accepted", bad)
		}
	}
	if _, err := chat.NormalizeTeamName("eng_team"); err == nil {
		t.Error("team names do not allow '_'")
	}
	if got := chat.SlugFromDisplayName("Dev Ops & Infra!"); got != "dev-ops-infra" {
		t.Errorf("slug %q", got)
	}
	if _, err := chat.CheckText(strings.Repeat("x", 251), chat.ChannelPurposeMaxRunes); !errors.Is(err, chat.ErrTooLong) {
		t.Error("purpose limit")
	}
}

// TestFixturesAreJSON keeps the payload fixtures (Mattermost-shaped events and responses) valid.
func TestFixturesAreJSON(t *testing.T) {
	t.Parallel()
	files, _ := filepath.Glob("testdata/events/*.json")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Event string `json:"event"`
			Seq   *int64 `json:"seq"`
		}
		if err := json.Unmarshal(b, &v); err != nil || v.Event == "" || v.Seq == nil {
			t.Errorf("%s: not an event envelope: %v", f, err)
		}
	}
}

// eventCatalog is the spec's list of server events (realtime-events).
var eventCatalog = []string{
	"hello", "posted", "post_edited", "post_deleted", "reaction_added", "reaction_removed", "typing", "status_change",
	"channel_viewed", "channel_created", "channel_updated", "channel_deleted", "channel_restored",
	"channel_member_updated", "direct_added", "group_added", "user_added", "user_removed", "added_to_team",
	"leave_team", "update_team", "delete_team", "restore_team", "user_role_updated", "user_updated",
	"preferences_changed", "resync",
}

// TestEventCatalogDocumented: every event has a payload fixture and is
// documented in docs/api.md with it.
func TestEventCatalogDocumented(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile("../../docs/api.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range eventCatalog {
		b, err := os.ReadFile(filepath.Join("testdata/events", name+".json"))
		if err != nil {
			t.Errorf("%s: no fixture", name)
			continue
		}
		var v struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(b, &v) != nil || v.Event != name {
			t.Errorf("%s: fixture names %q", name, v.Event)
		}
		if !strings.Contains(string(doc), "| `"+name+"` |") {
			t.Errorf("%s: not in the event catalog of docs/api.md", name)
		}
	}
}
