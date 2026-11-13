package main

import (
	"errors"
	"path/filepath"
	"testing"
)

// newVerifiedAccount makes a device with a usertag and a verified email, returning its token.
func newVerifiedAccount(t *testing.T, s *Store, name, email string) string {
	t.Helper()
	token, _, err := s.NewDevice()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetName(token, name); err != nil {
		t.Fatal(err)
	}
	code, _, err := s.BeginVerify(token, email)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckOTP(token, code); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestEmailCodeVerifiesAccount(t *testing.T) {
	s, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	token, _, _ := s.NewDevice()
	if _, _, err := s.BeginVerify(token, "a@example.test"); !errors.Is(err, ErrNoName) {
		t.Fatalf("email before usertag: %v, want ErrNoName", err)
	}
	if _, err := s.SetName(token, "vera"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BeginVerify(token, "not-an-email"); !errors.Is(err, ErrEmailInvalid) {
		t.Fatalf("bad email: %v, want ErrEmailInvalid", err)
	}
	code, addr, err := s.BeginVerify(token, "A@Example.test")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "a@example.test" {
		t.Fatalf("address not normalised: %q", addr)
	}
	if _, _, err := s.BeginVerify(token, "a@example.test"); !errors.Is(err, ErrCooldown) {
		t.Fatalf("second request: %v, want ErrCooldown", err)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if err := s.CheckOTP(token, wrong); !errors.Is(err, ErrOTPWrong) {
		t.Fatalf("wrong code: %v, want ErrOTPWrong", err)
	}
	if err := s.CheckOTP(token, code); err != nil {
		t.Fatalf("right code: %v", err)
	}
	acc, _ := s.ByToken(token)
	if !acc.Verified || acc.OTPHash != "" {
		t.Fatalf("after verify: verified=%v otp=%q", acc.Verified, acc.OTPHash)
	}
}

func TestEmailCannotBeSharedBetweenAccounts(t *testing.T) {
	s, _ := OpenStore("")
	newVerifiedAccount(t, s, "first_one", "shared@example.test")

	token, _, _ := s.NewDevice()
	if _, err := s.SetName(token, "second_one"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BeginVerify(token, "shared@example.test"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("same email on second account: %v, want ErrEmailTaken", err)
	}
}

func TestFriendsByUsertag(t *testing.T) {
	s, _ := OpenStore("")
	aliceTok := newVerifiedAccount(t, s, "alice_1", "alice@example.test")
	bobTok := newVerifiedAccount(t, s, "bob_22", "bob@example.test")

	if err := s.AddFriend(aliceTok, "BOB_22"); err != nil {
		t.Fatalf("add friend (case-insensitive): %v", err)
	}
	alice, _ := s.ByToken(aliceTok)
	bob, _ := s.ByToken(bobTok)
	if len(alice.Friends) != 1 || alice.Friends[0] != "bob_22" {
		t.Fatalf("alice friends = %v", alice.Friends)
	}
	if len(bob.Friends) != 1 || bob.Friends[0] != "alice_1" {
		t.Fatalf("friendship should be mutual, bob friends = %v", bob.Friends)
	}
	if err := s.AddFriend(aliceTok, "bob_22"); !errors.Is(err, ErrAlreadyFriend) {
		t.Fatalf("repeat add: %v, want ErrAlreadyFriend", err)
	}
	if err := s.AddFriend(aliceTok, "alice_1"); !errors.Is(err, ErrSelf) {
		t.Fatalf("add self: %v, want ErrSelf", err)
	}
	if err := s.AddFriend(aliceTok, "ghost_99"); !errors.Is(err, ErrNoUser) {
		t.Fatalf("unknown usertag: %v, want ErrNoUser", err)
	}
}

func TestUnverifiedCannotAddFriends(t *testing.T) {
	s, _ := OpenStore("")
	token, _, _ := s.NewDevice()
	s.SetName(token, "newbie_1")
	newVerifiedAccount(t, s, "pro_player", "pro@example.test")
	if err := s.AddFriend(token, "pro_player"); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("unverified add: %v, want ErrNotVerified", err)
	}
}

func TestVerifiedEmailSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	s, _ := OpenStore(path)
	newVerifiedAccount(t, s, "kept_1", "kept@example.test")

	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	token, _, _ := reopened.NewDevice()
	reopened.SetName(token, "other_2")
	if _, _, err := reopened.BeginVerify(token, "kept@example.test"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("email after restart: %v, want ErrEmailTaken", err)
	}
}
