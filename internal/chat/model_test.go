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
