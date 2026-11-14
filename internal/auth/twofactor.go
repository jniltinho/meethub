package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"rsc.io/qr"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
)

// Errors of two-factor authentication.
var (
	ErrTwoFactorUnavailable = errors.New("two-factor authentication is not available on this server")
	ErrTwoFactorEnabled     = errors.New("two-factor authentication is already on")
	ErrTwoFactorOff         = errors.New("two-factor authentication is off")
	ErrInvalidCode          = errors.New("the code is not valid")
	ErrInvalidTicket        = errors.New("the login expired, log in again")
)

// Second-step limits (design D2).
const (
	ticketTTL      = 5 * time.Minute
	ticketMaxFails = 5
	ticketPrefix   = "meethub:2fa:ticket:"
)

// secondFactor is a user's authenticator: the sealed secret, whether it is confirmed and the last step used.
type secondFactor struct {
	UserID      string `gorm:"primaryKey"`
	Secret      string
	ConfirmedAt int64
	LastStep    int64
}

func (secondFactor) TableName() string { return "user_second_factors" }

type recoveryCode struct {
	ID       string `gorm:"primaryKey"`
	UserID   string
	CodeHash string
}

func (recoveryCode) TableName() string { return "user_recovery_codes" }

func (s *Service) available() error {
	if len(s.SecretKey) == 0 {
		return ErrTwoFactorUnavailable
	}
	return nil
}

// factor returns the user's authenticator, nil when there is none.
func factor(ctx context.Context, db *gorm.DB, userID string) (*secondFactor, error) {
	var f secondFactor
	if err := db.WithContext(ctx).Where("user_id = ?", userID).Take(&f).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("second factor: %w", err)
	}
	return &f, nil
}

// TwoFactorEnabled reports whether the user has a confirmed authenticator.
func (s *Service) TwoFactorEnabled(ctx context.Context, userID string) (bool, error) {
	f, err := factor(ctx, s.DB.Gorm, userID)
	return f != nil && f.ConfirmedAt != 0, err
}

// TwoFactorStatus is what Settings shows.
type TwoFactorStatus struct {
	Enabled       bool  `json:"enabled"`
	RecoveryCodes int64 `json:"recovery_codes_left"`
}

// TwoFactor returns the principal's two-factor status.
func (s *Service) TwoFactor(ctx context.Context, p *Principal) (*TwoFactorStatus, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	on, err := s.TwoFactorEnabled(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	st := &TwoFactorStatus{Enabled: on}
	if on {
		err = s.DB.Gorm.WithContext(ctx).Model(&recoveryCode{}).Where("user_id = ?", p.User.ID).Count(&st.RecoveryCodes).Error
	}
	return st, err
}

// Enrollment is a new authenticator waiting for its first code.
type Enrollment struct {
	Secret string `json:"secret"`  // base32, to type into the app
	URI    string `json:"uri"`     // otpauth:// key URI
	QRCode string `json:"qr_code"` // the URI as a PNG data URL, drawn here: the secret never leaves the server otherwise
}

// BeginTwoFactor creates (or replaces) the principal's pending authenticator.
func (s *Service) BeginTwoFactor(ctx context.Context, p *Principal) (*Enrollment, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := seal(s.SecretKey, p.User.ID, secret)
	if err != nil {
		return nil, err
	}
	issuer := "MeetHub"
	if o, err := org.Get(ctx, s.DB.Gorm); err == nil && o.Name != "" {
		issuer = o.Name
	}
	uri := otpauthURL(issuer, p.User.Email, secret)
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return nil, fmt.Errorf("qr code: %w", err)
	}
	code.Scale = 5
	err = s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.users.LockByID(ctx, tx, s.DB.Dialect, p.User.ID); err != nil {
			return err
		}
		f, err := factor(ctx, tx, p.User.ID)
		if err != nil {
			return err
		}
		if f != nil && f.ConfirmedAt != 0 {
			return ErrTwoFactorEnabled
		}
		if f != nil {
			return tx.Model(f).Updates(map[string]any{"secret": sealed, "last_step": 0}).Error
		}
		return tx.Create(&secondFactor{UserID: p.User.ID, Secret: sealed}).Error
	})
	if err != nil {
		return nil, err
	}
	return &Enrollment{Secret: secret, URI: uri, QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG())}, nil
}

// ConfirmTwoFactor turns two-factor on with the first code of the pending authenticator and returns the recovery
// codes, shown once.
func (s *Service) ConfirmTwoFactor(ctx context.Context, p *Principal, code, clientIP string) ([]string, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	codes, hashes, err := s.newRecoveryCodes(ctx)
	if err != nil {
		return nil, err
	}
	err = s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.users.LockByID(ctx, tx, s.DB.Dialect, p.User.ID); err != nil {
			return err
		}
		f, err := factor(ctx, tx, p.User.ID)
		if err != nil {
			return err
		}
		if f == nil {
			return ErrTwoFactorOff
		}
		if f.ConfirmedAt != 0 {
			return ErrTwoFactorEnabled
		}
		secret, err := unseal(s.SecretKey, p.User.ID, f.Secret)
		if err != nil {
			return ErrTwoFactorUnavailable
		}
		step, ok := checkTOTP(secret, strings.TrimSpace(code), 0, s.now())
		if !ok {
			return ErrInvalidCode
		}
		if err := tx.Model(f).Updates(map[string]any{"confirmed_at": s.now().UnixMilli(), "last_step": step}).Error; err != nil {
			return err
		}
		if err := replaceRecoveryCodes(ctx, tx, p.User.ID, hashes); err != nil {
			return err
		}
		return s.audit2FA(ctx, tx, p, audit.ActionTwoFactorEnabled, clientIP)
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// RegenerateRecoveryCodes replaces the principal's recovery codes; it takes a current code of the authenticator.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, p *Principal, code, clientIP string) ([]string, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	err := s.guarded(ctx, p, clientIP, func() error { return s.checkTOTPOf(ctx, p.User.ID, code) })
	if err != nil {
		return nil, err
	}
	codes, hashes, err := s.newRecoveryCodes(ctx)
	if err != nil {
		return nil, err
	}
	err = s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := replaceRecoveryCodes(ctx, tx, p.User.ID, hashes); err != nil {
			return err
		}
		return s.audit2FA(ctx, tx, p, audit.ActionRecoveryCodesNew, clientIP)
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTwoFactor turns two-factor off with the principal's password and a current code (or a recovery code).
func (s *Service) DisableTwoFactor(ctx context.Context, p *Principal, password, code, clientIP string) error {
	if err := s.available(); err != nil {
		return err
	}
	err := s.guarded(ctx, p, clientIP, func() error {
		if err := s.Hasher.Verify(ctx, p.User.PasswordHash, password); err != nil {
			if errors.Is(err, identity.ErrPasswordMismatch) || errors.Is(err, identity.ErrPasswordTooLong) {
				return ErrInvalidCredentials
			}
			return err
		}
		return s.checkSecondFactor(ctx, p.User.ID, code, clientIP)
	})
	if err != nil {
		return err
	}
	return s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := identity.DeleteSecondFactor(ctx, tx, p.User.ID); err != nil {
			return err
		}
		return s.audit2FA(ctx, tx, p, audit.ActionTwoFactorDisabled, clientIP)
	})
}

// guarded runs check under the failed-login limits of the principal's e-mail and address: a failure counts as a
// failed login, a success releases the attempt.
func (s *Service) guarded(ctx context.Context, p *Principal, clientIP string, check func() error) error {
	email := p.User.EmailNormalized
	if err := s.Limiter.Reserve(ctx, clientIP, email); err != nil {
		return err
	}
	if err := check(); err != nil {
		if errors.Is(err, ErrInvalidCode) || errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrTwoFactorOff) {
			s.recordFailure(ctx, p.User.Email, clientIP)
		}
		return err
	}
	s.Limiter.Release(ctx, clientIP, email)
	return nil
}

func (s *Service) audit2FA(ctx context.Context, tx *gorm.DB, p *Principal, action, clientIP string) error {
	return audit.Record(ctx, tx, audit.Event{
		OrgID: &p.Membership.OrgID, ActorID: &p.User.ID, Action: action,
		TargetType: "user", TargetID: p.User.ID, ClientIP: clientIP,
	})
}

// newRecoveryCodes returns ten codes and their Argon2id hashes.
func (s *Service) newRecoveryCodes(ctx context.Context) ([]string, []string, error) {
	codes := make([]string, recoveryCodes)
	hashes := make([]string, recoveryCodes)
	for i := range codes {
		c, err := newRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		h, err := s.Hasher.Hash(ctx, recoveryPrefix+normalizeRecovery(c))
		if err != nil {
			return nil, nil, err
		}
		codes[i], hashes[i] = c, h
	}
	return codes, hashes, nil
}

func replaceRecoveryCodes(ctx context.Context, tx *gorm.DB, userID string, hashes []string) error {
	if err := tx.WithContext(ctx).Where("user_id = ?", userID).Delete(&recoveryCode{}).Error; err != nil {
		return fmt.Errorf("delete recovery codes: %w", err)
	}
	rows := make([]recoveryCode, len(hashes))
	for i, h := range hashes {
		rows[i] = recoveryCode{ID: identity.NewID(), UserID: userID, CodeHash: h}
	}
	if err := tx.WithContext(ctx).Create(&rows).Error; err != nil {
		return fmt.Errorf("store recovery codes: %w", err)
	}
	return nil
}

// checkTOTPOf accepts a current code of the user's confirmed authenticator, once.
func (s *Service) checkTOTPOf(ctx context.Context, userID, code string) error {
	f, err := factor(ctx, s.DB.Gorm, userID)
	if err != nil {
		return err
	}
	if f == nil || f.ConfirmedAt == 0 {
		return ErrTwoFactorOff
	}
	secret, err := unseal(s.SecretKey, userID, f.Secret)
	if err != nil {
		return ErrTwoFactorUnavailable // another key, or tampered data: fail closed
	}
	step, ok := checkTOTP(secret, code, f.LastStep, s.now())
	if !ok {
		return ErrInvalidCode
	}
	// single use: only the first request to move last_step past this step wins
	res := s.DB.Gorm.WithContext(ctx).Model(&secondFactor{}).
		Where("user_id = ? AND last_step < ?", userID, step).Update("last_step", step)
	if res.Error != nil {
		return fmt.Errorf("second factor: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrInvalidCode
	}
	return nil
}

// checkSecondFactor accepts a current authenticator code (6 digits) or an unused recovery code, once.
func (s *Service) checkSecondFactor(ctx context.Context, userID, code, clientIP string) error {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) == totpDigits && strings.Trim(code, "0123456789") == "" {
		return s.checkTOTPOf(ctx, userID, code)
	}
	typed := normalizeRecovery(code)
	if len(typed) != recoveryLen {
		return ErrInvalidCode
	}
	if on, err := s.TwoFactorEnabled(ctx, userID); err != nil || !on {
		if err != nil {
			return err
		}
		return ErrTwoFactorOff
	}
	var rows []recoveryCode
	if err := s.DB.Gorm.WithContext(ctx).Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return fmt.Errorf("recovery codes: %w", err)
	}
	for _, r := range rows {
		err := s.Hasher.Verify(ctx, r.CodeHash, recoveryPrefix+typed)
		if errors.Is(err, identity.ErrPasswordMismatch) || errors.Is(err, identity.ErrMalformedHash) {
			continue
		}
		if err != nil {
			return err
		}
		return s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			res := tx.Where("id = ?", r.ID).Delete(&recoveryCode{}) // single use: the first delete wins
			if res.Error != nil {
				return fmt.Errorf("use recovery code: %w", res.Error)
			}
			if res.RowsAffected == 0 {
				return ErrInvalidCode
			}
			return audit.Record(ctx, tx, audit.Event{
				ActorID: &userID, Action: audit.ActionRecoveryCodeUsed, TargetType: "user", TargetID: userID, ClientIP: clientIP,
			})
		})
	}
	return ErrInvalidCode
}

// ticket is the state between the password and the code (design D2).
type ticket struct {
	UserID string `json:"user_id"`
	IP     string `json:"ip"`
	Hash   string `json:"hash"`  // the password hash that was verified (startSession re-checks it)
	Email  string `json:"email"` // as typed, for the audit of failures
	Norm   string `json:"norm"`  // normalized, for the limiter
}

func ticketKey(t string) string {
	sum := sha256.Sum256([]byte(t))
	return ticketPrefix + hex.EncodeToString(sum[:])
}

func (s *Service) newTicket(ctx context.Context, t ticket) (string, error) {
	if s.Redis == nil {
		return "", ErrTwoFactorUnavailable
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	b, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	if err := s.Redis.Set(ctx, ticketKey(token), b, ticketTTL).Err(); err != nil {
		return "", fmt.Errorf("store two-factor ticket: %w", err)
	}
	return token, nil
}

// LoginTwoFactor is the second step of a login: the ticket of the first step and a current code (or a recovery
// code) start the session. Five wrong codes void the ticket and count as one failed login.
func (s *Service) LoginTwoFactor(ctx context.Context, token, code, clientIP, userAgent string) (*LoginResult, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if s.Redis == nil {
		return nil, ErrTwoFactorUnavailable
	}
	if token == "" || len(token) > 64 || len(code) > 64 {
		return nil, ErrInvalidTicket
	}
	key := ticketKey(token)
	raw, err := s.Redis.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrInvalidTicket
	}
	if err != nil {
		return nil, fmt.Errorf("two-factor ticket: %w", err)
	}
	var t ticket
	if err := json.Unmarshal(raw, &t); err != nil || t.IP != clientIP { // bound to the address of the first step
		return nil, ErrInvalidTicket
	}
	// the attempt is counted before the code is checked, so parallel guesses cannot exceed the limit
	pipe := s.Redis.TxPipeline()
	fails := pipe.Incr(ctx, key+":fails")
	pipe.Expire(ctx, key+":fails", ticketTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("two-factor ticket: %w", err)
	}
	if fails.Val() > ticketMaxFails {
		s.Redis.Del(ctx, key)
		return nil, ErrInvalidTicket
	}
	if err := s.checkSecondFactor(ctx, t.UserID, code, clientIP); err != nil {
		if !errors.Is(err, ErrInvalidCode) && !errors.Is(err, ErrTwoFactorOff) {
			return nil, err
		}
		s.recordTwoFactorFailure(ctx, t, audit.ActionTwoFactorFailed)
		if fails.Val() >= ticketMaxFails {
			s.Redis.Del(ctx, key)
			s.recordTwoFactorFailure(ctx, t, audit.ActionTwoFactorVoided)
			s.recordFailure(ctx, t.Email, clientIP) // the voided ticket is one failed login; its reservation stays
			return nil, ErrInvalidTicket
		}
		return nil, ErrInvalidCode
	}
	if n, err := s.Redis.Del(ctx, key).Result(); err != nil || n == 0 {
		return nil, ErrInvalidTicket // used or voided by a concurrent request
	}
	s.Redis.Del(ctx, key+":fails")
	res, err := s.startSession(ctx, t.UserID, t.Hash, clientIP, userAgent)
	if err != nil {
		return nil, err
	}
	s.Limiter.Release(ctx, clientIP, t.Norm)
	return res, nil
}

func (s *Service) recordTwoFactorFailure(ctx context.Context, t ticket, action string) {
	err := audit.Record(ctx, s.DB.Gorm, audit.Event{
		ActorID: &t.UserID, Action: action, TargetType: "user", TargetID: t.UserID, ClientIP: t.IP,
	})
	if err != nil && s.Logger != nil {
		s.Logger.ErrorContext(ctx, "audit two-factor failure", "error", err.Error())
	}
}
