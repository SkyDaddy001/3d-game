package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Signing in on another device. An account can be open on several devices: the first one's token
// hash is the account's key, and every other device's token hash points at that key.

const (
	maxDevices   = 5
	anonymousTTL = 24 * time.Hour // unnamed visitors are forgotten after this long
)

var (
	ErrHasAccount  = errors.New("this device already has an account; sign out of it first")
	ErrKeepAccount = errors.New("verify your email first, or signing out would lose this account for good")
)

// keyLocked returns the account key for a device token. The caller must hold s.mu.
func (s *Store) keyLocked(token string) string {
	h := hashToken(token)
	if primary, ok := s.devices[h]; ok {
		return primary
	}
	return h
}

// rekeyLocked moves an account to a new key and repoints everything that refers to it.
func (s *Store) rekeyLocked(a *Account, key string) {
	delete(s.accounts, a.TokenHash)
	a.TokenHash = key
	s.accounts[key] = a
	if a.Name != "" {
		s.names[strings.ToLower(a.Name)] = key
	}
	if a.Verified {
		s.emails[a.EmailHash] = key
	}
	for _, d := range a.Devices {
		s.devices[d] = key
	}
}

// BeginLogin starts signing a new, unnamed device in to the account that owns an email address.
// send is false when no account has that address: the caller must answer the same way in both
// cases, so the form cannot be used to find out who has an account.
func (s *Store) BeginLogin(token, email string) (code, addr string, send bool, err error) {
	addr, err = normalizeEmail(email)
	if err != nil {
		return "", "", false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.keyLocked(token)
	d, ok := s.accounts[h]
	if !ok {
		return "", "", false, ErrUnknown
	}
	if d.Name != "" {
		return "", "", false, ErrHasAccount
	}
	now := time.Now()
	if d.OTPSentAt != 0 && now.Sub(time.Unix(d.OTPSentAt, 0)) < otpCooldown {
		return "", "", false, ErrCooldown
	}
	d.OTPSentAt = now.Unix()
	target, found := s.emails[hashWith("email", addr)]
	if !found {
		d.LoginTarget, d.OTPHash = "", ""
		return "", addr, false, nil
	}
	code, err = newCode()
	if err != nil {
		return "", "", false, err
	}
	d.LoginTarget = target
	d.OTPHash = hashWith(h, code)
	d.OTPExpires = now.Add(otpTTL).Unix()
	d.OTPTries = 0
	return code, addr, true, nil
}

// CompleteLogin checks the code and links this device to the account.
func (s *Store) CompleteLogin(token, code string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.keyLocked(token)
	d, ok := s.accounts[h]
	if !ok {
		return Account{}, ErrUnknown
	}
	if d.Name != "" {
		return Account{}, ErrHasAccount
	}
	if d.LoginTarget == "" || d.OTPHash == "" {
		return Account{}, ErrNoPending
	}
	if time.Now().Unix() > d.OTPExpires {
		return Account{}, ErrOTPExpired
	}
	if d.OTPTries >= otpMaxTries {
		return Account{}, ErrOTPTries
	}
	d.OTPTries++
	if hashWith(h, strings.TrimSpace(code)) != d.OTPHash {
		return Account{}, ErrOTPWrong
	}
	target, ok := s.accounts[d.LoginTarget]
	if !ok {
		return Account{}, ErrNoPending
	}
	delete(s.accounts, h)
	target.Devices = append(target.Devices, h)
	s.devices[h] = target.TokenHash
	// Too many devices: the one that signed in longest ago is signed out.
	for len(target.Devices) > maxDevices-1 {
		delete(s.devices, target.Devices[0])
		target.Devices = target.Devices[1:]
	}
	return copyAccount(target), s.saveLocked()
}

// SignOut removes this device from its account. The account stays, and can be opened again with
// an email code, so an account without a verified email cannot sign out of its only device.
func (s *Store) SignOut(token string) error {
	h := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	if primary, ok := s.devices[h]; ok {
		a := s.accounts[primary]
		a.Devices = without(a.Devices, h)
		delete(s.devices, h)
		return s.saveLocked()
	}
	a, ok := s.accounts[h]
	if !ok {
		return ErrUnknown
	}
	if a.Name == "" {
		delete(s.accounts, h)
		return nil
	}
	if !a.Verified {
		return ErrKeepAccount
	}
	if len(a.Devices) > 0 {
		next := a.Devices[0]
		a.Devices = a.Devices[1:]
		delete(s.devices, next)
		s.rekeyLocked(a, next)
		return s.saveLocked()
	}
	// No device left: park the account under a key no cookie can match.
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	s.rekeyLocked(a, "detached:"+hex.EncodeToString(b))
	return s.saveLocked()
}

// SignOutOthers keeps this device and signs every other device out of the account.
func (s *Store) SignOutOthers(token string) (int, error) {
	h := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok {
		return 0, ErrUnknown
	}
	n := len(a.Devices)
	for _, d := range a.Devices {
		delete(s.devices, d)
	}
	a.Devices = nil
	if a.TokenHash != h { // this device was an extra one: it becomes the account's key
		s.rekeyLocked(a, h)
	}
	return n, s.saveLocked()
}

// PruneAnonymous forgets unnamed devices created before cutoff. It returns how many were dropped.
func (s *Store) PruneAnonymous(cutoff time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for h, a := range s.accounts {
		if a.Name == "" && a.Created < cutoff.Unix() {
			delete(s.accounts, h)
			n++
		}
	}
	return n
}

func without(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
