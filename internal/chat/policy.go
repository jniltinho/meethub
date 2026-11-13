package chat

import (
	"errors"

	"github.com/jniltinho/meethub/internal/org"
)

// Team and channel roles, as in Mattermost.
const (
	TeamAdmin    = "team_admin"
	TeamUser     = "team_user"
	ChannelAdmin = "channel_admin"
	ChannelUser  = "channel_user"
)

// Authorization errors. ErrNotFound hides channels the caller may not discover.
var (
	ErrForbidden = errors.New("forbidden")
	ErrNotFound  = errors.New("not found")
)

// Subject is the caller as the chat policy sees them.
type Subject struct {
	UserID  string
	OrgRole string // owner, admin, member, guest
}

// Guest reports whether the subject is a guest.
func (s Subject) Guest() bool { return s.OrgRole == org.RoleGuest }

// System reports whether the subject administers everything (manage_system).
func (s Subject) System() bool { return org.Can(s.OrgRole, org.PermManageSystem) }

// Access is what the policy needs about one channel and the subject's place in it.
type Access struct {
	Type        string // O, P, D, G
	TeamRole    string // the subject's role in the channel's team; "" when not a member (D/G: unused)
	ChannelRole string // the subject's role in the channel; "" when not a member
}

func (a Access) direct() bool   { return a.Type == ChannelDirect || a.Type == ChannelGroup }
func (a Access) inTeam() bool   { return a.direct() || a.TeamRole != "" }
func (a Access) isMember() bool { return a.ChannelRole != "" }

// CanDiscover: name, purpose and member count (D4 matrix, first row).
func CanDiscover(s Subject, a Access) bool {
	switch {
	case a.direct(), a.Type == ChannelPrivate:
		return a.isMember() && a.inTeam()
	default: // public
		return a.inTeam() && (a.isMember() || !s.Guest())
	}
}

// CanRead: history, threads, reactions, members — members only.
func CanRead(_ Subject, a Access) bool { return a.isMember() && a.inTeam() }

// CanJoin: public channels, team members except guests, not already a member.
func CanJoin(s Subject, a Access) bool {
	return a.Type == ChannelOpen && a.TeamRole != "" && !s.Guest() && !a.isMember()
}

// CanPost: post, react and type — members only.
func CanPost(s Subject, a Access) bool { return CanRead(s, a) }

// CanAdminister: edit, archive, members and roles of a team channel — channel
// admins, team admins and manage_system; direct and group conversations have no
// administrators (their membership is fixed).
func CanAdminister(s Subject, a Access) bool {
	if a.direct() {
		return false
	}
	return s.System() || (a.TeamRole == TeamAdmin) || (a.inTeam() && a.ChannelRole == ChannelAdmin)
}

// ReadError is the error for a refused read: 404 when the channel may not even be
// discovered (its existence is not revealed), 403 otherwise.
func ReadError(s Subject, a Access) error {
	if !CanDiscover(s, a) {
		return ErrNotFound
	}
	return ErrForbidden
}

// CanCreateChannel: team members except guests.
func CanCreateChannel(s Subject, teamRole string) bool { return teamRole != "" && !s.Guest() }

// CanCreateTeam: the create_team permission.
func CanCreateTeam(s Subject) bool { return org.Can(s.OrgRole, org.PermCreateTeam) }

// CanAdministerTeam: team admins and manage_system.
func CanAdministerTeam(s Subject, teamRole string) bool { return s.System() || teamRole == TeamAdmin }

// CanJoinTeam: non-guests join open teams they are not in.
func CanJoinTeam(s Subject, teamType, teamRole string) bool {
	return teamType == TeamOpen && teamRole == "" && !s.Guest()
}

// Team types.
const (
	TeamOpen   = "open"
	TeamInvite = "invite"
)
