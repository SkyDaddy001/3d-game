package main

import (
	"errors"
	"testing"
)

func TestClanLifecycle(t *testing.T) {
	s, _ := OpenStore("")
	lead := newVerifiedAccount(t, s, "clan_lead", "lead@example.test")
	mate := newVerifiedAccount(t, s, "clan_mate", "mate@example.test")

	if err := s.CreateClan(lead, "Wolves", "WLF"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.CreateClan(mate, "wolves", "MTE"); !errors.Is(err, ErrClanTaken) {
		t.Fatalf("duplicate name: %v, want ErrClanTaken", err)
	}
	if err := s.CreateClan(mate, "Bears", "wlf"); !errors.Is(err, ErrClanTaken) {
		t.Fatalf("duplicate tag: %v, want ErrClanTaken", err)
	}
	if err := s.CreateClan(lead, "Other", "OTH"); !errors.Is(err, ErrInClan) {
		t.Fatalf("second clan: %v, want ErrInClan", err)
	}

	if err := s.InviteToClan(mate, "clan_lead"); !errors.Is(err, ErrNoClan) {
		t.Fatalf("invite from no clan: %v, want ErrNoClan", err)
	}
	if err := s.AcceptClanInvite(mate); !errors.Is(err, ErrNoInvite) {
		t.Fatalf("accept without invite: %v, want ErrNoInvite", err)
	}
	if err := s.InviteToClan(lead, "clan_mate"); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if err := s.AcceptClanInvite(mate); err != nil {
		t.Fatalf("accept: %v", err)
	}

	c, ok := s.ClanOf(mate)
	if !ok || c.Name != "Wolves" || c.Tag != "WLF" || c.Leader != "clan_lead" || len(c.Members) != 2 {
		t.Fatalf("clan view for member: %+v ok=%v", c, ok)
	}
	if c.Leading {
		t.Fatal("member should not be leading")
	}

	if err := s.LeaveClan(mate); err != nil {
		t.Fatalf("member leaves: %v", err)
	}
	if c, _ := s.ClanOf(lead); len(c.Members) != 1 {
		t.Fatalf("after member left: %d members, want 1", len(c.Members))
	}

	if err := s.LeaveClan(lead); err != nil {
		t.Fatalf("leader leaves: %v", err)
	}
	if _, ok := s.ClanOf(lead); ok {
		t.Fatal("leader leaving should disband the clan")
	}
}

func TestOnlyLeaderInvites(t *testing.T) {
	s, _ := OpenStore("")
	lead := newVerifiedAccount(t, s, "boss_one", "boss@example.test")
	mate := newVerifiedAccount(t, s, "pawn_two", "pawn@example.test")
	other := newVerifiedAccount(t, s, "third_three", "third@example.test")
	s.CreateClan(lead, "Crew", "CRW")
	s.InviteToClan(lead, "pawn_two")
	s.AcceptClanInvite(mate)
	if err := s.InviteToClan(mate, "third_three"); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("member invite: %v, want ErrNotLeader", err)
	}
	if err := s.InviteToClan(lead, "third_three"); err != nil {
		t.Fatalf("leader invite: %v", err)
	}
	if err := s.AcceptClanInvite(other); err != nil {
		t.Fatalf("accept: %v", err)
	}
}

func TestClanNeedsVerifiedAccount(t *testing.T) {
	s, _ := OpenStore("")
	token, _, _ := s.NewDevice()
	s.SetName(token, "unverified_1")
	if err := s.CreateClan(token, "Ghosts", "GST"); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("unverified create: %v, want ErrNotVerified", err)
	}
}

func TestClanNameAndTagRules(t *testing.T) {
	s, _ := OpenStore("")
	token := newVerifiedAccount(t, s, "rules_one", "rules@example.test")
	if err := s.CreateClan(token, "ab", "ABC"); !errors.Is(err, ErrClanName) {
		t.Fatalf("short name: %v, want ErrClanName", err)
	}
	if err := s.CreateClan(token, "Fine", "toolong"); !errors.Is(err, ErrClanTag) {
		t.Fatalf("long tag: %v, want ErrClanTag", err)
	}
}

func TestClanTagReachesMatchPlayers(t *testing.T) {
	s, _ := OpenStore("")
	token := newVerifiedAccount(t, s, "tag_holder", "tag@example.test")
	if err := s.CreateClan(token, "Foxes", "FOX"); err != nil {
		t.Fatal(err)
	}
	m := NewMatch("tagtest")
	m.clanTag = s.ClanTagByName
	p, ok := m.Join("tag_holder")
	if !ok {
		t.Fatal("join failed")
	}
	if v := p.view(); v.Clan != "FOX" {
		t.Fatalf("view clan %q, want FOX", v.Clan)
	}
	q, _ := m.Join("nobody_here")
	if q != nil && q.view().Clan != "" {
		t.Fatalf("player without clan has tag %q", q.view().Clan)
	}
}

func TestClanHandOver(t *testing.T) {
	s, _ := OpenStore("")
	lead := newVerifiedAccount(t, s, "old_boss", "oldboss@example.test")
	mate := newVerifiedAccount(t, s, "new_boss", "newboss@example.test")
	out := newVerifiedAccount(t, s, "outsider", "out@example.test")
	s.CreateClan(lead, "Owls", "OWL")
	s.InviteToClan(lead, "new_boss")
	s.AcceptClanInvite(mate)

	if err := s.HandOverClan(lead, "outsider"); !errors.Is(err, ErrNoUser) {
		t.Fatalf("hand over to non-member: %v, want ErrNoUser", err)
	}
	if err := s.HandOverClan(mate, "old_boss"); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("member hands over: %v, want ErrNotLeader", err)
	}
	if err := s.HandOverClan(lead, "new_boss"); err != nil {
		t.Fatalf("hand over: %v", err)
	}
	if c, _ := s.ClanOf(mate); !c.Leading || c.Leader != "new_boss" {
		t.Fatalf("new leader not set: %+v", c)
	}
	if c, _ := s.ClanOf(lead); c.Leading || len(c.Members) != 2 {
		t.Fatalf("old leader should stay as a member: %+v", c)
	}
	_ = out
}
