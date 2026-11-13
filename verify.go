package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"
)

// Email verification and friends. Codes are six digits, valid for ten minutes, and locked out
// after five wrong tries. A code can be requested again only after thirty seconds.
const (
	otpTTL      = 10 * time.Minute
	otpMaxTries = 5
	otpCooldown = 30 * time.Second
)

var (
	ErrEmailInvalid  = errors.New("enter a valid email address")
	ErrEmailTaken    = errors.New("that email is already linked to another account")
	ErrNoName        = errors.New("choose your usertag first")
	ErrNotVerified   = errors.New("verify your email first")
	ErrCooldown      = errors.New("wait a moment before requesting another code")
	ErrOTPExpired    = errors.New("that code has expired, request a new one")
	ErrOTPTries      = errors.New("too many wrong codes, request a new one")
	ErrOTPWrong      = errors.New("wrong code")
	ErrNoPending     = errors.New("request a code first")
	ErrSelf          = errors.New("that is your own usertag")
	ErrNoUser        = errors.New("no player with that usertag")
	ErrAlreadyFriend = errors.New("already friends")
)

// hashWith returns a salted SHA-256 of a value, hex-encoded.
func hashWith(salt, v string) string {
	sum := sha256.Sum256([]byte(salt + "|" + v))
	return hex.EncodeToString(sum[:])
}

// normalizeEmail checks the address and lowercases it, so one mailbox maps to one account.
func normalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return "", ErrEmailInvalid
	}
	return strings.ToLower(a.Address), nil
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// BeginVerify starts email verification. It returns the code and the normalized address for the
// mailer. The address itself is kept only as a hash, and only until the code is confirmed.
func (s *Store) BeginVerify(token, email string) (code, addr string, err error) {
	addr, err = normalizeEmail(email)
	if err != nil {
		return "", "", err
	}
	h := hashToken(token)
	emailHash := hashWith("email", addr)

	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[h]
	if !ok {
		return "", "", ErrUnknown
	}
	if a.Name == "" {
		return "", "", ErrNoName
	}
	if owner, taken := s.emails[emailHash]; taken && owner != h {
		return "", "", ErrEmailTaken
	}
	now := time.Now()
	if a.OTPSentAt != 0 && now.Sub(time.Unix(a.OTPSentAt, 0)) < otpCooldown {
		return "", "", ErrCooldown
	}
	code, err = newCode()
	if err != nil {
		return "", "", err
	}
	a.PendingEmail = emailHash
	a.OTPHash = hashWith(h, code)
	a.OTPSentAt = now.Unix()
	a.OTPExpires = now.Add(otpTTL).Unix()
	a.OTPTries = 0
	if err := s.saveLocked(); err != nil {
		return "", "", err
	}
	return code, addr, nil
}

// CheckOTP confirms the code. On success the account is verified and the email is locked to it.
func (s *Store) CheckOTP(token, code string) error {
	h := hashToken(token)

	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[h]
	if !ok {
		return ErrUnknown
	}
	if a.PendingEmail == "" || a.OTPHash == "" {
		return ErrNoPending
	}
	if time.Now().Unix() > a.OTPExpires {
		return ErrOTPExpired
	}
	if a.OTPTries >= otpMaxTries {
		return ErrOTPTries
	}
	a.OTPTries++
	if hashWith(h, strings.TrimSpace(code)) != a.OTPHash {
		if err := s.saveLocked(); err != nil {
			return err
		}
		return ErrOTPWrong
	}
	if owner, taken := s.emails[a.PendingEmail]; taken && owner != h {
		return ErrEmailTaken
	}
	a.EmailHash = a.PendingEmail
	a.Verified = true
	s.emails[a.EmailHash] = h
	a.PendingEmail, a.OTPHash, a.OTPSentAt, a.OTPExpires, a.OTPTries = "", "", 0, 0, 0
	return s.saveLocked()
}

// AddFriend adds another player by usertag. Friendships are mutual and added at once.
func (s *Store) AddFriend(token, usertag string) error {
	h := hashToken(token)

	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[h]
	if !ok {
		return ErrUnknown
	}
	if !a.Verified {
		return ErrNotVerified
	}
	otherHash, ok := s.names[strings.ToLower(strings.TrimSpace(usertag))]
	if !ok {
		return ErrNoUser
	}
	if otherHash == h {
		return ErrSelf
	}
	other := s.accounts[otherHash]
	if contains(a.Friends, other.Name) {
		return ErrAlreadyFriend
	}
	a.Friends = append(a.Friends, other.Name)
	if !contains(other.Friends, a.Name) {
		other.Friends = append(other.Friends, a.Name)
	}
	return s.saveLocked()
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
