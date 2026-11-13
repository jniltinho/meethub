// Package org owns the organization, memberships and the role → permission
// rules (modelled on Mattermost's system roles) that the server enforces for
// every action, including user management and the last-owner guard.
package org

import (
	"errors"
	"slices"
)

// Roles, highest first.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleGuest  = "guest"
)

// Permission names, Mattermost style.
type Permission string

// Permissions granted by roles.
const (
	PermManageSystem   Permission = "manage_system" // organization settings
	PermManageOwners   Permission = "manage_owners" // grant/remove owner, edit owners
	PermManageUsers    Permission = "manage_users"  // create, disable, enable, set role ≤ admin, reset passwords
	PermReadAudits     Permission = "read_audits"   // read the audit log
	PermCreateTeam     Permission = "create_team"   // create chat teams
	PermEditOwnProfile Permission = "edit_own_profile"
)

// Errors of the authorization rules.
var (
	ErrForbidden   = errors.New("forbidden")
	ErrLastOwner   = errors.New("the organization must keep at least one active owner")
	ErrInvalidRole = errors.New("role must be owner, admin, member or guest")
)

var rolePermissions = map[string][]Permission{
	RoleOwner:  {PermManageSystem, PermManageOwners, PermManageUsers, PermReadAudits, PermCreateTeam, PermEditOwnProfile},
	RoleAdmin:  {PermManageUsers, PermReadAudits, PermCreateTeam, PermEditOwnProfile},
	RoleMember: {PermEditOwnProfile},
	RoleGuest:  {PermEditOwnProfile},
}

// ValidRole reports whether r is a known role.
func ValidRole(r string) bool { _, ok := rolePermissions[r]; return ok }

// Can reports whether role grants p.
func Can(role string, p Permission) bool { return slices.Contains(rolePermissions[role], p) }

// Permissions lists what role grants (for /me).
func Permissions(role string) []Permission { return slices.Clone(rolePermissions[role]) }

// Actor is who performs an action. System is the operator at the CLI: it may
// do anything and is recorded without an actor id.
type Actor struct {
	UserID   string
	Role     string
	ClientIP string
	System   bool
}

// SystemActor is the CLI operator.
var SystemActor = Actor{System: true, Role: RoleOwner}

// Require returns ErrForbidden unless the actor has p.
func (a Actor) Require(p Permission) error {
	if a.System || Can(a.Role, p) {
		return nil
	}
	return ErrForbidden
}

// CanManage checks that the actor may change a user whose role is targetRole
// (and, when newRole is not empty, give them newRole).
func (a Actor) CanManage(targetRole, newRole string) error {
	if err := a.Require(PermManageUsers); err != nil {
		return err
	}
	if newRole != "" && !ValidRole(newRole) {
		return ErrInvalidRole
	}
	if targetRole == RoleOwner || newRole == RoleOwner {
		return a.Require(PermManageOwners)
	}
	return nil
}
