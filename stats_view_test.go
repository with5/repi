package main

import (
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"repi/internal/store"
)

// The sidebar button opens the panel; the stepper sets a goal that survives a
// restart, and listening enough reaches it.
func TestDailyGoal(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := newModel(silentOut{})
	defer m.pl.Close()
	tt := ui.NewTester(m.view, 1000, 720)

	if tt.HasText("每日目标") {
		t.Fatal("panel shown before the click")
	}
	if err := tt.Click("学习统计"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("每日目标") || !tt.HasText("未设定") {
		t.Fatalf("panel not shown: %q", tt.Texts())
	}

	// The upper arrow of the stepper, twice.
	r, ok := tt.Find("调整每日目标")
	if !ok {
		t.Fatal("no stepper")
	}
	for range 2 {
		tt.ClickAt(r.X+r.W/2, r.Y+r.H/4)
		tt.Advance(time.Second)
		tt.Frame()
	}
	if m.dailyGoal != 2*dailyGoalStep {
		t.Fatalf("goal = %d", m.dailyGoal)
	}
	if !tt.HasText("今天还差 10 分") {
		t.Errorf("no remaining time: %q", tt.Texts())
	}

	again, err := store.Open(store.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Settings().DailyGoal; got != 10 {
		t.Errorf("goal on disk = %d", got)
	}

	a := m.st.Add(store.Material{Title: "The long one", Text: "x"})
	b := m.st.Add(store.Material{Title: "The short one", Text: "y"})
	m.st.AddListen(time.Now(), a.ID, 3*time.Minute)
	m.st.AddListen(time.Now(), b.ID, time.Minute)
	m.st.AddListen(time.Now().AddDate(0, 0, -2), 0, 5*time.Minute)
	tt.Frame()
	if !tt.HasText("今天还差 6 分") {
		t.Errorf("remaining not updated: %q", tt.Texts())
	}

	// With REPI_SHOT=path the panel is saved there to be looked at.
	if p := os.Getenv("REPI_SHOT"); p != "" {
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, tt.Image())
	}

	m.st.AddListen(time.Now(), 0, 6*time.Minute)
	tt.Frame()
	if !tt.HasText("今天已达标") || !tt.HasText("近 7 天达标 1 天") {
		t.Errorf("goal not reached: %q", tt.Texts())
	}
}

func TestHeatLevel(t *testing.T) {
	const full = 40 * time.Minute
	for _, c := range []struct {
		d    time.Duration
		want int
	}{
		{0, 0},
		{time.Second, 1},
		{10 * time.Minute, 1},
		{11 * time.Minute, 2},
		{39 * time.Minute, 4},
		{40 * time.Minute, 4},
		{90 * time.Minute, 4},
	} {
		if got := heatLevel(c.d, full); got != c.want {
			t.Errorf("heatLevel(%v) = %d, want %d", c.d, got, c.want)
		}
	}
	if heatLevel(time.Minute, 0) != 0 {
		t.Error("no scale should give the empty level")
	}
}
