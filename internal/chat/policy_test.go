package chat_test

import (
	"errors"
	"testing"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/org"
)

// TestAccessMatrix checks every cell of design D4's access matrix.
func TestAccessMatrix(t *testing.T) {
	t.Parallel()
	member := chat.Subject{UserID: "m", OrgRole: org.RoleMember}
	guest := chat.Subject{UserID: "g", OrgRole: org.RoleGuest}
	owner := chat.Subject{UserID: "o", OrgRole: org.RoleOwner}
	admin := chat.Subject{UserID: "a", OrgRole: org.RoleAdmin} // org admin: user management, not chat administration

	type want struct{ discover, read, join, post, administer bool }
	tests := []struct {
		name string
		s    chat.Subject
		a    chat.Access
		want want
	}{
		// public channel
		{"public, team member not joined", member, chat.Access{Type: "O", TeamRole: chat.TeamUser}, want{true, false, true, false, false}},
		{"public, channel member", member, chat.Access{Type: "O", TeamRole: chat.TeamUser, ChannelRole: chat.ChannelUser}, want{true, true, false, true, false}},
		{"public, channel admin", member, chat.Access{Type: "O", TeamRole: chat.TeamUser, ChannelRole: chat.ChannelAdmin}, want{true, true, false, true, true}},
		{"public, team admin not joined", member, chat.Access{Type: "O", TeamRole: chat.TeamAdmin}, want{true, false, true, false, true}},
		{"public, outside the team", member, chat.Access{Type: "O"}, want{false, false, false, false, false}},
		{"public, guest not added", guest, chat.Access{Type: "O", TeamRole: chat.TeamUser}, want{false, false, false, false, false}},
		{"public, guest added", guest, chat.Access{Type: "O", TeamRole: chat.TeamUser, ChannelRole: chat.ChannelUser}, want{true, true, false, true, false}},
		{"public, org admin not joined", admin, chat.Access{Type: "O", TeamRole: chat.TeamUser}, want{true, false, true, false, false}},
		{"public, owner outside the team", owner, chat.Access{Type: "O"}, want{false, false, false, false, true}},
		// private channel
		{"private, team member not added", member, chat.Access{Type: "P", TeamRole: chat.TeamUser}, want{false, false, false, false, false}},
		{"private, channel member", member, chat.Access{Type: "P", TeamRole: chat.TeamUser, ChannelRole: chat.ChannelUser}, want{true, true, false, true, false}},
		{"private, channel member removed from the team", member, chat.Access{Type: "P", ChannelRole: chat.ChannelUser}, want{false, false, false, false, false}},
		{"private, team admin not added", member, chat.Access{Type: "P", TeamRole: chat.TeamAdmin}, want{false, false, false, false, true}},
		{"private, guest added", guest, chat.Access{Type: "P", TeamRole: chat.TeamUser, ChannelRole: chat.ChannelUser}, want{true, true, false, true, false}},
		// direct and group conversations: members only, no administrators
		{"DM member", member, chat.Access{Type: "D", ChannelRole: chat.ChannelUser}, want{true, true, false, true, false}},
		{"DM outsider", member, chat.Access{Type: "D"}, want{false, false, false, false, false}},
		{"DM, owner outsider", owner, chat.Access{Type: "D"}, want{false, false, false, false, false}},
		{"GM member", guest, chat.Access{Type: "G", ChannelRole: chat.ChannelUser}, want{true, true, false, true, false}},
		{"GM, owner member", owner, chat.Access{Type: "G", ChannelRole: chat.ChannelUser}, want{true, true, false, true, false}},
	}
	for _, tt := range tests {
		got := want{
			chat.CanDiscover(tt.s, tt.a), chat.CanRead(tt.s, tt.a), chat.CanJoin(tt.s, tt.a),
			chat.CanPost(tt.s, tt.a), chat.CanAdminister(tt.s, tt.a),
		}
		if got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestReadErrorHidesUndiscoverableChannels(t *testing.T) {
	t.Parallel()
	member := chat.Subject{OrgRole: org.RoleMember}
	if err := chat.ReadError(member, chat.Access{Type: "P", TeamRole: chat.TeamUser}); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("private non-member: %v", err)
	}
	if err := chat.ReadError(member, chat.Access{Type: "O", TeamRole: chat.TeamUser}); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("public not joined: %v", err)
	}
}

func TestTeamRules(t *testing.T) {
	t.Parallel()
	for _, role := range []string{org.RoleOwner, org.RoleAdmin, org.RoleMember, org.RoleGuest} {
		s := chat.Subject{OrgRole: role}
		wantCreate := role == org.RoleOwner || role == org.RoleAdmin
		if chat.CanCreateTeam(s) != wantCreate {
			t.Errorf("%s create team: %v", role, !wantCreate)
		}
		wantJoin := role != org.RoleGuest
		if chat.CanJoinTeam(s, chat.TeamOpen, "") != wantJoin {
			t.Errorf("%s join open team", role)
		}
		if chat.CanJoinTeam(s, chat.TeamInvite, "") || chat.CanJoinTeam(s, chat.TeamOpen, chat.TeamUser) {
			t.Errorf("%s joined an invite team or a team they are in", role)
		}
		if got := chat.CanCreateChannel(s, chat.TeamUser); got != (role != org.RoleGuest) {
			t.Errorf("%s create channel: %v", role, got)
		}
		if got := chat.CanAdministerTeam(s, chat.TeamUser); got != (role == org.RoleOwner) {
			t.Errorf("%s administer team as team_user: %v", role, got)
		}
		if !chat.CanAdministerTeam(s, chat.TeamAdmin) {
			t.Errorf("%s as team_admin must administer", role)
		}
	}
	if chat.CanCreateChannel(chat.Subject{OrgRole: org.RoleMember}, "") {
		t.Error("outside the team")
	}
}
