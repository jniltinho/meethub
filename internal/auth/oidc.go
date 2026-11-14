package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
)

// Errors of single sign-on.
var (
	// ErrSSOFailed is every refused single sign-on: the person sees only that it could not be completed.
	ErrSSOFailed = errors.New("the single sign-on could not be completed")
	// ErrPasswordLoginDisabled is a password login while only single sign-on is offered.
	ErrPasswordLoginDisabled = errors.New("password login is turned off: use single sign-on")
)

const (
	oidcStateTTL    = 10 * time.Minute
	oidcStatePrefix = "meethub:oidc:state:"
)

// OIDCConfig is the provider of single sign-on (config.OIDC with the redirect URI).
type OIDCConfig struct {
	Issuer, ClientID, ClientSecret string
	RedirectURL                    string // <public_url>/api/v1/auth/oidc/callback
	Scopes                         []string
	AutoCreate                     bool
	AllowedDomains                 []string
	DefaultRole                    string // member or guest
}

// OIDC signs people in through an OpenID Connect provider (design D1, D2): the authorization code flow with PKCE,
// a single-use state and a nonce in Redis, the ID token verified with go-oidc, accounts matched by verified e-mail.
type OIDC struct {
	Config OIDCConfig
	Auth   *Service
	Users  *org.Users // creates accounts (auto_create)
	// HTTPClient reaches the provider; nil: http.DefaultClient (tests: the test provider's TLS client).
	HTTPClient *http.Client

	mu       sync.Mutex // guards provider: discovered on first use, so a provider down at startup does not stop it
	provider *oidc.Provider
}

// ctx carries the client that reaches the provider: a timeout, and redirects only to https:// (the client secret,
// the code and the PKCE verifier never travel in clear).
func (o *OIDC) ctx(ctx context.Context) context.Context {
	var transport http.RoundTripper
	if o.HTTPClient != nil {
		transport = o.HTTPClient.Transport
	}
	return oidc.ClientContext(ctx, &http.Client{Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 5 {
				return errors.New("redirect refused")
			}
			return nil
		}})
}

func (o *OIDC) discover(ctx context.Context) (*oidc.Provider, *oauth2.Config, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider == nil {
		p, err := oidc.NewProvider(o.ctx(ctx), o.Config.Issuer)
		if err != nil {
			return nil, nil, fmt.Errorf("oidc discovery: %w", err)
		}
		var keys struct {
			JWKS string `json:"jwks_uri"`
		}
		if err := p.Claims(&keys); err != nil {
			return nil, nil, fmt.Errorf("oidc discovery: %w", err)
		}
		for _, u := range []string{p.Endpoint().AuthURL, p.Endpoint().TokenURL, keys.JWKS} {
			if !strings.HasPrefix(u, "https://") {
				return nil, nil, fmt.Errorf("oidc discovery: endpoint %q is not https://", u)
			}
		}
		o.provider = p
	}
	scopes := o.Config.Scopes
	if !slices.Contains(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	return o.provider, &oauth2.Config{
		ClientID: o.Config.ClientID, ClientSecret: o.Config.ClientSecret, RedirectURL: o.Config.RedirectURL,
		Endpoint: o.provider.Endpoint(), Scopes: scopes,
	}, nil
}

type oidcState struct {
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"` // PKCE
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Start returns the provider's authorization URL and the state, which the caller also binds to the browser (a
// cookie) so a callback started in another browser fails.
func (o *OIDC) Start(ctx context.Context) (authURL, state string, err error) {
	if o.Auth.Redis == nil {
		return "", "", ErrSSOFailed
	}
	_, cfg, err := o.discover(ctx)
	if err != nil {
		return "", "", err
	}
	if state, err = randomToken(); err != nil {
		return "", "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", "", err
	}
	st := oidcState{Nonce: nonce, Verifier: oauth2.GenerateVerifier()}
	b, err := json.Marshal(st)
	if err != nil {
		return "", "", err
	}
	if err := o.Auth.Redis.Set(ctx, ticketKeyWith(oidcStatePrefix, state), b, oidcStateTTL).Err(); err != nil {
		return "", "", fmt.Errorf("store oidc state: %w", err)
	}
	return cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(st.Verifier)), state, nil
}

// oidcClaims are the ID token claims MeetHub uses.
type oidcClaims struct {
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"` // true, or "true" from some providers
	Name          string `json:"name"`
}

func (c oidcClaims) verified() bool { return c.EmailVerified == true || c.EmailVerified == "true" }

// Callback completes a single sign-on: browserState is the state the browser's cookie carries. Every refusal is
// ErrSSOFailed (the reason goes to the audit log and the server log, never to the person).
func (o *OIDC) Callback(ctx context.Context, state, browserState, code, clientIP, userAgent string) (*LoginResult, error) {
	email := ""
	refuse := func(reason string, err error) (*LoginResult, error) {
		o.refused(ctx, email, clientIP, reason, err)
		return nil, ErrSSOFailed
	}
	if state == "" || len(state) > 64 || subtle.ConstantTimeCompare([]byte(state), []byte(browserState)) != 1 {
		return refuse("state does not match the browser", nil)
	}
	if o.Auth.Redis == nil {
		return refuse("no redis", nil)
	}
	raw, err := o.Auth.Redis.GetDel(ctx, ticketKeyWith(oidcStatePrefix, state)).Bytes() // single use
	if err != nil {
		return refuse("unknown or used state", nil)
	}
	var st oidcState
	if err := json.Unmarshal(raw, &st); err != nil {
		return refuse("bad state", err)
	}
	provider, cfg, err := o.discover(ctx)
	if err != nil {
		return refuse("discovery", err)
	}
	tok, err := cfg.Exchange(o.ctx(ctx), code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return refuse("code exchange", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return refuse("no id_token", nil)
	}
	// issuer, audience, signature and expiry
	idt, err := provider.Verifier(&oidc.Config{ClientID: o.Config.ClientID}).Verify(o.ctx(ctx), rawID)
	if err != nil {
		return refuse("id token", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(st.Nonce)) != 1 {
		return refuse("nonce", nil)
	}
	var claims oidcClaims
	if err := idt.Claims(&claims); err != nil {
		return refuse("claims", err)
	}
	email = truncateRunes(strings.TrimSpace(claims.Email), 320)
	if !claims.verified() {
		return refuse("e-mail not verified", nil)
	}
	userID, err := o.match(ctx, idt.Subject, claims, clientIP)
	if err != nil {
		return refuse(err.Error(), nil)
	}
	// design D5: the provider is responsible for the second factor
	return o.Auth.issueSession(ctx, userID, clientIP, userAgent, func(*gorm.DB, *identity.User) error { return nil },
		audit.Event{Action: audit.ActionOIDCLogin, Details: map[string]any{"email": email}})
}

// match finds the account of a verified e-mail (creating it when allowed) and checks or stores the subject.
func (o *OIDC) match(ctx context.Context, subject string, c oidcClaims, clientIP string) (string, error) {
	if subject == "" || len(subject) > 255 {
		return "", errors.New("bad subject")
	}
	norm, err := identity.NormalizeEmail(c.Email)
	if err != nil {
		return "", errors.New("bad e-mail")
	}
	db := o.Auth.DB.Gorm
	u, err := o.Auth.users.ByEmail(ctx, db, norm)
	if errors.Is(err, identity.ErrUserNotFound) {
		if u, err = o.create(ctx, norm, subject, c, clientIP); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if u.Status != identity.StatusActive {
		return "", errors.New("account disabled")
	}
	if u.OIDCSubject != nil {
		if subtle.ConstantTimeCompare([]byte(*u.OIDCSubject), []byte(subject)) != 1 {
			return "", errors.New("subject changed") // a recycled e-mail at the provider cannot enter this account
		}
		return u.ID, nil
	}
	res := db.WithContext(ctx).Exec(`UPDATE users SET oidc_subject = ? WHERE id = ? AND oidc_subject IS NULL`, subject, u.ID)
	if err := database.Translate(res.Error); err != nil {
		return "", errors.New("subject already belongs to another account")
	}
	if res.RowsAffected == 0 { // a concurrent first sign-on stored one: it must be this subject
		again, err := o.Auth.users.ByID(ctx, db, u.ID)
		if err != nil || again.OIDCSubject == nil || *again.OIDCSubject != subject {
			return "", errors.New("subject changed")
		}
	}
	return u.ID, nil
}

// create makes the account with the subject in one transaction: a subject that belongs to another account rolls
// it back.
func (o *OIDC) create(ctx context.Context, email, subject string, c oidcClaims, clientIP string) (*identity.User, error) {
	_, domain, _ := strings.Cut(email, "@")
	if !o.Config.AutoCreate {
		return nil, errors.New("unknown e-mail and auto_create off")
	}
	if len(o.Config.AllowedDomains) > 0 && !slices.ContainsFunc(o.Config.AllowedDomains, func(d string) bool {
		return strings.EqualFold(d, domain)
	}) {
		return nil, errors.New("e-mail domain not allowed")
	}
	name := truncateRunes(strings.TrimSpace(c.Name), 200)
	if _, err := identity.CheckDisplayName(name); err != nil {
		name, _, _ = strings.Cut(email, "@")
	}
	pw, err := randomToken() // nobody knows it: the account signs in through the provider
	if err != nil {
		return nil, err
	}
	m, err := o.Users.Create(ctx, org.Actor{System: true, ClientIP: clientIP},
		org.NewUser{Email: email, DisplayName: name, Role: o.Config.DefaultRole, Password: pw, Via: "oidc", OIDCSubject: subject})
	if err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	return &m.User, nil
}

func (o *OIDC) refused(ctx context.Context, email, clientIP, reason string, cause error) {
	err := audit.Record(ctx, o.Auth.DB.Gorm, audit.Event{
		Action: audit.ActionOIDCRefused, TargetType: "email", ClientIP: clientIP,
		Details: map[string]any{"email": email, "reason": reason},
	})
	if o.Auth.Logger != nil {
		args := []any{"reason", reason}
		if cause != nil {
			args = append(args, "error", safeCause(cause))
		}
		if err != nil {
			args = append(args, "audit_error", err.Error())
		}
		o.Auth.Logger.WarnContext(ctx, "single sign-on refused", args...)
	}
}

// Logf logs an error of the single sign-on flow that is not a refusal (those are audited and logged by Callback).
func (o *OIDC) Logf(ctx context.Context, msg string, err error) {
	if o.Auth.Logger != nil {
		o.Auth.Logger.ErrorContext(ctx, msg, "error", safeCause(err))
	}
}

// safeCause describes a provider failure without the provider's response body (it may carry tokens).
func safeCause(err error) string {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		status := 0
		if re.Response != nil {
			status = re.Response.StatusCode
		}
		return fmt.Sprintf("token endpoint answered %d %s", status, re.ErrorCode)
	}
	// discovery and key-set errors quote the provider's answer: keep the start (status, cause), never a whole body
	return truncateRunes(err.Error(), 200)
}
