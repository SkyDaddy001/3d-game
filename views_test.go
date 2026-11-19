package main

import "testing"

// Capture and bomb state appear in the snapshot only in their modes.
func TestCaptureAndBombViews(t *testing.T) {
	m := newArena()
	m.Mode = "capture"
	m.zones = newCaptureZones()
	m.captureScores = [2]int{12, 7}
	if v := m.captureView(); v == nil || v.Scores != [2]int{12, 7} || len(v.Zones) != 3 {
		t.Fatalf("capture view wrong: %+v", v)
	}
	if m.bombView() != nil {
		t.Fatal("a capture room should have no bomb view")
	}

	b := newArena()
	b.Mode = "bomb"
	b.bomb = newBomb()
	b.bomb.planted, b.bomb.site, b.bomb.fuse = true, 2, 30
	v := b.bombView()
	if v == nil || !v.Planted || v.Site != 2 || v.Sites[1].Name != "B" || b.captureView() != nil {
		t.Fatalf("bomb view wrong: %+v", v)
	}
}
