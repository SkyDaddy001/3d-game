package main

import "testing"

func TestLevelCurve(t *testing.T) {
	cases := []struct{ xp, level int }{
		{0, 1}, {99, 1}, {100, 2}, {399, 2}, {400, 3}, {900, 4},
	}
	for _, c := range cases {
		if got := LevelFor(c.xp); got != c.level {
			t.Errorf("LevelFor(%d) = %d, want %d", c.xp, got, c.level)
		}
	}
}

func TestLevelProgressWithinLevel(t *testing.T) {
	level, into, need := LevelProgress(250)
	// Level 2 starts at 100 XP and level 3 at 400, so 250 is 150 into a 300 XP level.
	if level != 2 || into != 150 || need != 300 {
		t.Fatalf("LevelProgress(250) = %d, %d, %d; want 2, 150, 300", level, into, need)
	}
}

func TestComputeXPRewardsWinning(t *testing.T) {
	if win := ComputeXP(0, 1, 0); win != 500 {
		t.Fatalf("win with no kills = %d, want 500", win)
	}
	if loss := ComputeXP(0, 9, 0); loss >= ComputeXP(0, 1, 0) {
		t.Fatal("placing 9th should earn less than winning")
	}
	if ComputeXP(3, 5, 0) <= ComputeXP(1, 5, 0) {
		t.Fatal("more kills should earn more XP")
	}
}
