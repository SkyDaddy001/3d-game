package main

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---- Signing in on another device ----

func TestSecondDeviceSignsInWithEmailCode(t *testing.T) {
	s, _ := OpenStore("")
	first := newVerifiedAccount(t, s, "traveller", "trav@example.test")
	s.RecordMatch("traveller", MatchResult{XP: 400})

	second, _, _ := s.NewDevice()
	code, addr, send, err := s.BeginLogin(second, "Trav@Example.test")
	if err != nil || !send || addr != "trav@example.test" {
		t.Fatalf("begin login: send %v addr %q err %v", send, addr, err)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if _, err := s.CompleteLogin(second, wrong); !errors.Is(err, ErrOTPWrong) {
		t.Fatalf("wrong code: %v", err)
	}
	acc, err := s.CompleteLogin(second, code)
	if err != nil || acc.Name != "traveller" || acc.XP != 400 {
		t.Fatalf("complete login: %+v err %v", acc, err)
	}
	// Both devices now open the same account, and changes from one show on the other.
	if err := s.SetLook(second, Look{Hair: 3}); err != nil {
		t.Fatal(err)
	}
	a1, _ := s.ByToken(first)
	a2, _ := s.ByToken(second)
	if a1.Name != "traveller" || a2.Name != "traveller" || a1.Look.Hair != 3 {
		t.Fatalf("devices disagree: %+v / %+v", a1, a2)
	}
}

// An unknown email gets the same outcome as a known one, but no code is made.
func TestLoginDoesNotRevealWhoHasAnAccount(t *testing.T) {
	s, _ := OpenStore("")
	dev, _, _ := s.NewDevice()
	code, _, send, err := s.BeginLogin(dev, "nobody@example.test")
	if err != nil || send || code != "" {
		t.Fatalf("unknown email: code %q send %v err %v", code, send, err)
	}
	if _, err := s.CompleteLogin(dev, "123456"); !errors.Is(err, ErrNoPending) {
		t.Fatalf("code with nothing pending: %v", err)
	}
	if _, _, _, err := s.BeginLogin(dev, "nobody@example.test"); !errors.Is(err, ErrCooldown) {
		t.Fatalf("second request at once: %v, want ErrCooldown", err)
	}
}

// A device that already has an account cannot be signed in to a different one.
func TestNamedDeviceCannotLoginElsewhere(t *testing.T) {
	s, _ := OpenStore("")
	newVerifiedAccount(t, s, "owner_one", "one@example.test")
	other := newVerifiedAccount(t, s, "owner_two", "two@example.test")
	if _, _, _, err := s.BeginLogin(other, "one@example.test"); !errors.Is(err, ErrHasAccount) {
		t.Fatalf("named device login: %v, want ErrHasAccount", err)
	}
}

func TestSignOutAndBackIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acc.json")
	s, _ := OpenStore(path)
	first := newVerifiedAccount(t, s, "comeback", "cb@example.test")
	if err := s.SignOut(first); err != nil {
		t.Fatal(err)
	}
	if acc, ok := s.ByToken(first); ok && acc.Name != "" {
		t.Fatal("the old cookie still opens the account after signing out")
	}
	// The account survives, including across a restart, and a new device can sign in to it.
	s, _ = OpenStore(path)
	dev, _, _ := s.NewDevice()
	code, _, send, err := s.BeginLogin(dev, "cb@example.test")
	if err != nil || !send {
		t.Fatalf("login after sign-out: send %v err %v", send, err)
	}
	if acc, err := s.CompleteLogin(dev, code); err != nil || acc.Name != "comeback" {
		t.Fatalf("sign back in: %+v %v", acc, err)
	}
}

func TestUnverifiedAccountCannotSignOut(t *testing.T) {
	s, _ := OpenStore("")
	tok, _, _ := s.NewDevice()
	s.SetName(tok, "fresh_one")
	if err := s.SignOut(tok); !errors.Is(err, ErrKeepAccount) {
		t.Fatalf("sign out without a verified email: %v, want ErrKeepAccount", err)
	}
}

func TestSignOutOtherDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acc.json")
	s, _ := OpenStore(path)
	first := newVerifiedAccount(t, s, "multi_dev", "md@example.test")
	second, _, _ := s.NewDevice()
	code, _, _, _ := s.BeginLogin(second, "md@example.test")
	if _, err := s.CompleteLogin(second, code); err != nil {
		t.Fatal(err)
	}
	// Signing out the others from the second device leaves only the second.
	if n, err := s.SignOutOthers(second); err != nil || n != 1 {
		t.Fatalf("sign out others: n %d err %v", n, err)
	}
	if acc, ok := s.ByToken(first); ok && acc.Name != "" {
		t.Fatal("the first device is still signed in")
	}
	s, _ = OpenStore(path) // and it stays that way after a restart
	if acc, ok := s.ByToken(second); !ok || acc.Name != "multi_dev" {
		t.Fatalf("second device after restart: ok %v %+v", ok, acc)
	}
	if err := s.RecordMatch("multi_dev", MatchResult{XP: 10}); err != nil {
		t.Fatalf("the account lost its usertag index: %v", err)
	}
}

// Visitors who never pick a usertag are not written to disk and are forgotten after a day.
func TestAnonymousDevicesStayOffDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acc.json")
	s, _ := OpenStore(path)
	for i := 0; i < 20; i++ {
		s.NewDevice()
	}
	newVerifiedAccount(t, s, "real_player", "real@example.test")
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), `"token_hash"`); n != 1 {
		t.Fatalf("%d accounts on disk, want only the named one", n)
	}
	if dropped := s.PruneAnonymous(time.Now().Add(time.Hour)); dropped != 20 {
		t.Fatalf("pruned %d anonymous devices, want 20", dropped)
	}
	if _, err := s.Top(5), error(nil); err != nil || len(s.Top(5)) != 1 {
		t.Fatal("pruning removed a real account")
	}
}

// ---- Rate limits ----

func TestLimiterWindow(t *testing.T) {
	l := newLimiter()
	for i := 0; i < 3; i++ {
		if !l.allow("k", 3, 50*time.Millisecond) {
			t.Fatalf("request %d refused inside the limit", i+1)
		}
	}
	if l.allow("k", 3, 50*time.Millisecond) {
		t.Fatal("the fourth request should be refused")
	}
	if !l.allow("other", 3, 50*time.Millisecond) {
		t.Fatal("another key should not be affected")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.allow("k", 3, 50*time.Millisecond) {
		t.Fatal("the limit should reset after its window")
	}
}

func TestRecordingBotMatchesIsRateLimited(t *testing.T) {
	srv, s := newTestServer(t)
	pl := newPlayer(t, srv.URL, "spammer_1")
	s.limits = newLimiter() // on from here, after the player has signed up
	codes := map[int]int{}
	for i := 0; i < 6; i++ {
		codes[postForm(t, pl, srv.URL+"/replays/bots", url.Values{"bots": {"2"}}).StatusCode]++
	}
	if codes[http.StatusOK] != 4 || codes[http.StatusTooManyRequests] != 2 {
		t.Fatalf("status counts %v, want 4 allowed and 2 refused", codes)
	}
}

// ---- Backups ----

func TestBackupsAreWrittenAndPruned(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(filepath.Join(dir, "acc.json"))
	newVerifiedAccount(t, s, "saved_one", "saved@example.test")
	base := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	var last string
	for i := 0; i < 5; i++ {
		p, err := s.Backup(filepath.Join(dir, "backups"), 3, base.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		last = p
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "backups"))
	if len(entries) != 3 {
		t.Fatalf("%d backups kept, want 3", len(entries))
	}
	// A backup is a complete accounts file: opening it as one restores the player.
	restored, err := OpenStore(last)
	if err != nil {
		t.Fatal(err)
	}
	if top := restored.Top(5); len(top) != 1 || top[0].Name != "saved_one" {
		t.Fatalf("restored accounts: %+v", top)
	}
}

// ---- Usertags, blocking, reports ----

func TestUsertagRules(t *testing.T) {
	for _, bad := range []string{"Admin_1", "xX_sh1t_Xx", "Bot_Luna", "M0derator", "F_u_c_k_you"} {
		if !nameBlocked(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
	for _, ok := range []string{"SkyDaddy", "tarak_99", "Robotnik", "classy_one", "abbot_x"} {
		if nameBlocked(ok) {
			t.Errorf("%q should be allowed", ok)
		}
	}
	s, _ := OpenStore("")
	tok, _, _ := s.NewDevice()
	if _, err := s.SetName(tok, "the_admin"); !errors.Is(err, ErrNameBlocked) {
		t.Fatalf("reserved name: %v, want ErrNameBlocked", err)
	}
}

func TestBlockEndsFriendshipAndHidesPlayers(t *testing.T) {
	s, _ := OpenStore("")
	a := newVerifiedAccount(t, s, "blocker_a", "a@example.test")
	b := newVerifiedAccount(t, s, "blocked_b", "b@example.test")
	s.AddFriend(a, "blocked_b")
	if err := s.Block(a, "blocked_b"); err != nil {
		t.Fatal(err)
	}
	accA, _ := s.ByToken(a)
	accB, _ := s.ByToken(b)
	if len(accA.Friends) != 0 || len(accB.Friends) != 0 {
		t.Fatalf("friendship survived a block: %v / %v", accA.Friends, accB.Friends)
	}
	if err := s.AddFriend(b, "blocker_a"); !errors.Is(err, ErrNoUser) {
		t.Fatalf("blocked player adding the blocker: %v, want ErrNoUser", err)
	}
	if !s.HasBlocked("blocker_a", "blocked_b") || s.HasBlocked("blocked_b", "blocker_a") {
		t.Fatal("HasBlocked is wrong")
	}
	if err := s.Unblock(a, "blocked_b"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFriend(b, "blocker_a"); err != nil {
		t.Fatalf("after unblocking: %v", err)
	}
	if err := s.RemoveFriend(a, "blocked_b"); err != nil {
		t.Fatal(err)
	}
	if accB, _ = s.ByToken(b); len(accB.Friends) != 0 {
		t.Fatal("removing a friend should remove it on both sides")
	}
}

func TestReportsAreSaved(t *testing.T) {
	srv, s := newTestServer(t)
	s.reports.path = filepath.Join(t.TempDir(), "reports.jsonl")
	a := newPlayer(t, srv.URL, "reporter_1")
	newPlayer(t, srv.URL, "rude_one")
	resp := postForm(t, a, srv.URL+"/reports", url.Values{"usertag": {"RUDE_ONE"}, "reason": {"cheating"}, "note": {"aimed through walls"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("report: status %d", resp.StatusCode)
	}
	data, _ := os.ReadFile(s.reports.path)
	for _, want := range []string{`"reporter":"reporter_1"`, `"target":"rude_one"`, `"reason":"cheating"`, "aimed through walls"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("report file is missing %s: %s", want, data)
		}
	}
	if r := postForm(t, a, srv.URL+"/reports", url.Values{"usertag": {"nobody_x"}, "reason": {"cheating"}}); r.StatusCode != http.StatusNotFound {
		t.Fatalf("report of an unknown player: status %d", r.StatusCode)
	}
	if r := postForm(t, a, srv.URL+"/reports", url.Values{"usertag": {"rude_one"}, "reason": {"because"}}); r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("report with a made-up reason: status %d", r.StatusCode)
	}
}

// The whole second-device flow through the web routes.
func TestLoginRoutes(t *testing.T) {
	srv, _ := newTestServer(t)
	newPlayer(t, srv.URL, "web_owner")
	dev := newPlayerNoName(t, srv.URL)
	if page := getBody(t, dev, srv.URL+"/"); !strings.Contains(page, "Already have an account?") {
		t.Fatal("a new device should be offered sign-in")
	}
	if r := postForm(t, dev, srv.URL+"/auth/login/email", url.Values{"email": {"web_owner@example.test"}}); r.StatusCode != http.StatusOK {
		t.Fatalf("login email: status %d", r.StatusCode)
	}
	if r := postForm(t, dev, srv.URL+"/auth/login/otp", url.Values{"code": {testMail.code(t)}}); r.StatusCode != http.StatusOK {
		t.Fatalf("login code: status %d", r.StatusCode)
	}
	page := getBody(t, dev, srv.URL+"/")
	if !strings.Contains(page, "web_owner") || !strings.Contains(page, "Signed in on 2 devices") {
		t.Fatal("the second device should now show the account, on 2 devices")
	}
	// Signing out here returns this browser to a fresh visitor.
	if r := postForm(t, dev, srv.URL+"/auth/signout", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("sign out: status %d", r.StatusCode)
	}
	if page := getBody(t, dev, srv.URL+"/"); strings.Contains(page, "web_owner") {
		t.Fatal("still signed in after signing out")
	}
}
