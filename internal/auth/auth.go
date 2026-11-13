package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/session"
)

// Errors of the authentication flows.
var (
	ErrInvalidCredentials = errors.New("invalid e-mail or password")
	ErrInvalidSetupToken  = errors.New("invalid setup token")
	ErrAlreadyInitialized = errors.New("the install is already initialized")
	ErrFirstUserOwner     = errors.New("the first user must be an owner and needs an organization name")
)

// SetupTokenFile is the file holding a generated setup token, in the data dir.
const SetupTokenFile = "setup-token"

// PasswordHasher hashes and verifies passwords (identity.Hasher implements it).
type PasswordHasher interface {
	Hash(ctx context.Context, pw string) (string, error)
	Verify(ctx context.Context, encoded, pw string) error
	VerifyDummy(ctx context.Context, pw string) error
}

// Service implements setup, login, logout, password changes and request
// authentication.
type Service struct {
	DB       *database.DB
	Hasher   PasswordHasher
	Sessions session.Store
	Limiter  *Limiter
	DataDir  string
	Logger   *slog.Logger
	Now      func() time.Time

	mu         sync.Mutex // guards setupToken
	setupToken string
	users      identity.Store
}

func (s *Service) getSetupToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setupToken
}

func (s *Service) setSetupToken(t string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setupToken = t
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Initialized reports whether the organization exists.
func (s *Service) Initialized(ctx context.Context) (bool, error) {
	_, err := org.Get(ctx, s.DB.Gorm)
	if errors.Is(err, org.ErrNoOrganization) {
		return false, nil
	}
	return err == nil, err
}

// PrepareSetup readies the setup token on an uninitialized install: the given
// one (MEETHUB_SETUP_TOKEN) or a generated one written to <data_dir>/setup-token
// (0600). It returns the token and whether it was generated (to log it once).
func (s *Service) PrepareSetup(ctx context.Context, configured string) (token string, generated bool, err error) {
	ok, err := s.Initialized(ctx)
	if err != nil {
		return "", false, err
	}
	if ok {
		_ = os.Remove(filepath.Join(s.DataDir, SetupTokenFile))
		return "", false, nil
	}
	if configured != "" {
		s.setSetupToken(configured)
		return configured, false, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", false, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	s.setSetupToken(token)
	if err := os.MkdirAll(s.DataDir, 0o750); err != nil {
		return "", false, fmt.Errorf("data dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.DataDir, SetupTokenFile), []byte(token+"\n"), 0o600); err != nil {
		return "", false, fmt.Errorf("write setup token: %w", err)
	}
	return token, true, nil
}

// InitInput creates the organization and its owner.
type InitInput struct {
	OrganizationName string
	DisplayName      string
	Email            string
	Password         string
}

// Initialize creates the organization, the owner, the membership and the
// system.initialized audit event in one transaction; the organization
// singleton guarantees at most one initialization ever succeeds. It is shared
// by the setup page and the CLI.
func (s *Service) Initialize(ctx context.Context, in InitInput, clientIP string) (*org.Organization, *identity.User, error) {
	if strings.TrimSpace(in.OrganizationName) == "" {
		return nil, nil, ErrFirstUserOwner
	}
	hash, err := s.Hasher.Hash(ctx, in.Password)
	if err != nil {
		return nil, nil, err
	}
	var (
		o *org.Organization
		u *identity.User
	)
	err = s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if o, err = org.Create(ctx, tx, in.OrganizationName); err != nil {
			if errors.Is(err, org.ErrAlreadyExists) {
				return ErrAlreadyInitialized
			}
			return err
		}
		u = &identity.User{Email: in.Email, DisplayName: in.DisplayName, PasswordHash: hash}
		if err := s.users.Create(ctx, tx, u); err != nil {
			return err
		}
		if err := org.AddMember(ctx, tx, o.ID, u.ID, org.RoleOwner); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: &o.ID, ActorID: &u.ID, Action: audit.ActionSystemInitialized,
			TargetType: "organization", TargetID: o.ID, ClientIP: clientIP,
			Details: map[string]any{"organization": o.Name},
		})
	})
	if err != nil {
		return nil, nil, err
	}
	s.setSetupToken("")
	_ = os.Remove(filepath.Join(s.DataDir, SetupTokenFile))
	return o, u, nil
}

// Setup is Initialize from the web: it requires the setup token and logs the
// owner in.
func (s *Service) Setup(ctx context.Context, token string, in InitInput, clientIP, userAgent string) (*LoginResult, error) {
	ok, err := s.Initialized(ctx)
	if err != nil {
		return nil, err
	}
	if ok {
		return nil, ErrAlreadyInitialized
	}
	if want := s.getSetupToken(); want == "" || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		return nil, ErrInvalidSetupToken
	}
	_, u, err := s.Initialize(ctx, in, clientIP)
	if err != nil {
		return nil, err
	}
	return s.startSession(ctx, u, clientIP, userAgent)
}

// LoginResult is a new session for a user.
type LoginResult struct {
	Token   string
	Session *session.Session
	User    *identity.User
}

// Login checks the credentials (with rate limits and uniform failures) and
// starts a new session.
func (s *Service) Login(ctx context.Context, email, password, clientIP, userAgent string) (*LoginResult, error) {
	if len(password) > identity.MaxPasswordBytes {
		return nil, identity.ErrPasswordTooLong
	}
	normalized, nerr := identity.NormalizeEmail(email)
	if nerr != nil {
		normalized = strings.ToLower(strings.TrimSpace(email))
	}
	if err := s.Limiter.Check(ctx, clientIP, normalized); err != nil {
		return nil, err
	}
	u, err := s.users.ByEmail(ctx, s.DB.Gorm, normalized)
	switch {
	case errors.Is(err, identity.ErrUserNotFound):
		err = s.Hasher.VerifyDummy(ctx, password)
	case err == nil:
		err = s.Hasher.Verify(ctx, u.PasswordHash, password)
		if err == nil && u.Status != identity.StatusActive {
			err = ErrInvalidCredentials
		}
	}
	if err != nil {
		if errors.Is(err, identity.ErrBusy) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		if !errors.Is(err, identity.ErrPasswordMismatch) && !errors.Is(err, ErrInvalidCredentials) &&
			!errors.Is(err, identity.ErrMalformedHash) {
			return nil, err
		}
		s.Limiter.Fail(ctx, clientIP, normalized)
		s.recordFailure(ctx, normalized, clientIP)
		return nil, ErrInvalidCredentials
	}
	s.Limiter.Succeed(ctx, normalized)
	return s.startSession(ctx, u, clientIP, userAgent)
}

func (s *Service) recordFailure(ctx context.Context, email, clientIP string) {
	err := audit.Record(ctx, s.DB.Gorm, audit.Event{
		Action: audit.ActionLoginFailed, TargetType: "email", TargetID: truncate(email, 64), ClientIP: clientIP,
	})
	if err != nil && s.Logger != nil {
		s.Logger.ErrorContext(ctx, "audit login failure", "error", err.Error())
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// startSession creates a session and records the successful login.
func (s *Service) startSession(ctx context.Context, u *identity.User, clientIP, userAgent string) (*LoginResult, error) {
	var res LoginResult
	err := s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		token, sess, err := s.Sessions.Create(ctx, tx, u.ID, clientIP, userAgent, s.now())
		if err != nil {
			return err
		}
		if err := s.users.TouchLogin(ctx, tx, u.ID); err != nil {
			return err
		}
		res = LoginResult{Token: token, Session: sess, User: u}
		return audit.Record(ctx, tx, audit.Event{
			ActorID: &u.ID, Action: audit.ActionLoginSucceeded, TargetType: "user", TargetID: u.ID, ClientIP: clientIP,
		})
	})
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Principal is the authenticated caller of a request.
type Principal struct {
	User       *identity.User
	Membership *org.Membership
	Session    *session.Session
	Token      string
}

// Actor returns the principal as an org.Actor.
func (p *Principal) Actor(clientIP string) org.Actor {
	return org.Actor{UserID: p.User.ID, Role: p.Membership.Role, ClientIP: clientIP}
}

// CSRFToken is the token the SPA must echo in X-CSRF-Token.
func (p *Principal) CSRFToken() string { return session.CSRFToken(p.Token) }

// Authenticate resolves a session token to its live principal and records activity.
func (s *Service) Authenticate(ctx context.Context, token string) (*Principal, error) {
	sess, err := s.Sessions.Lookup(ctx, s.DB.Gorm, token, s.now())
	if err != nil {
		return nil, err
	}
	u, err := s.users.ByID(ctx, s.DB.Gorm, sess.UserID)
	if err != nil || u.Status != identity.StatusActive {
		return nil, session.ErrInvalid
	}
	m, err := org.MembershipOf(ctx, s.DB.Gorm, u.ID)
	if err != nil {
		return nil, session.ErrInvalid
	}
	if err := s.Sessions.Touch(ctx, s.DB.Gorm, sess, s.now()); err != nil {
		return nil, err
	}
	return &Principal{User: u, Membership: m, Session: sess, Token: token}, nil
}

// Logout revokes the principal's session.
func (s *Service) Logout(ctx context.Context, p *Principal, clientIP string) error {
	return s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := session.Revoke(ctx, tx, p.Session.ID, s.now()); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: &p.Membership.OrgID, ActorID: &p.User.ID, Action: audit.ActionLogout,
			TargetType: "session", TargetID: p.Session.ID, ClientIP: clientIP,
		})
	})
}

// ChangePassword changes the principal's own password: all their sessions
// end and the current browser continues with a new session token.
func (s *Service) ChangePassword(ctx context.Context, p *Principal, current, next, clientIP, userAgent string) (*LoginResult, error) {
	if err := s.Hasher.Verify(ctx, p.User.PasswordHash, current); err != nil {
		if errors.Is(err, identity.ErrPasswordMismatch) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	hash, err := s.Hasher.Hash(ctx, next)
	if err != nil {
		return nil, err
	}
	var res LoginResult
	err = s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.users.SetPassword(ctx, tx, p.User.ID, hash); err != nil {
			return err
		}
		if err := session.RevokeAllForUser(ctx, tx, p.User.ID, "", s.now()); err != nil {
			return err
		}
		token, sess, err := s.Sessions.Create(ctx, tx, p.User.ID, clientIP, userAgent, s.now())
		if err != nil {
			return err
		}
		res = LoginResult{Token: token, Session: sess, User: p.User}
		return audit.Record(ctx, tx, audit.Event{
			OrgID: &p.Membership.OrgID, ActorID: &p.User.ID, Action: audit.ActionPasswordChanged,
			TargetType: "user", TargetID: p.User.ID, ClientIP: clientIP,
		})
	})
	if err != nil {
		return nil, err
	}
	return &res, nil
}
