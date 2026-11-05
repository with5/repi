package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"repi/internal/store"
)

const (
	heatWeeks = 26 // columns of the heatmap
	heatCell  = 14 // side of a day's square
	heatGap   = 3

	maxDailyGoal  = 240 // minutes the goal stepper goes up to
	dailyGoalStep = 5
)

// statsPanel shows how long the user listened each day.
func (m *model) statsPanel(c *ui.Context) {
	t := c.Theme()
	now := time.Now()
	// Columns are weeks starting on Sunday, so the first day shown is a Sunday.
	n := (heatWeeks-1)*7 + int(now.Weekday()) + 1
	days := m.st.Daily(now, n)
	total, active := m.st.TotalListened()

	var peak, week time.Duration
	for i, d := range days {
		peak = max(peak, d.Listened)
		if i >= n-7 {
			week += d.Listened
		}
	}

	ui.Text(c, "学习统计").FontSize(18).Bold()
	ui.Row(c).Gap(12).Children(func() {
		for _, s := range [][2]string{
			{"今天", fmtListen(days[n-1].Listened)},
			{"近 7 天", fmtListen(week)},
			{"连续天数", fmt.Sprintf("%d 天", m.st.Streak(now))},
			{"累计", fmtListen(total)},
		} {
			ui.Column(c.Key(s[0])).Grow(1).Padding(10, 12).Gap(2).Border(1, t.Border).Children(func() {
				ui.Text(c, s[0]).FontSize(12).TextColor(t.TextMuted)
				ui.Text(c, s[1]).FontSize(20).Bold()
			})
		}
	})

	m.goalRow(c, days)

	caption := fmt.Sprintf("每天听了多久（近 %d 周，单日最高 %s）", heatWeeks, fmtListen(peak))
	if m.dailyGoal > 0 {
		caption += " · 最深色为达标"
	}
	ui.Text(c, caption).FontSize(12).TextColor(t.TextMuted)
	m.heatmap(c, days, peak)
	m.materialStats(c)
	if total == 0 {
		ui.Text(c, "还没有收听记录：播放一篇资料，这里就会开始统计。").TextColor(t.TextMuted)
	} else {
		ui.Textf(c, "已累计 %d 天有收听，平均每个学习日 %s", active, fmtListen(total/time.Duration(active))).FontSize(12).TextColor(t.TextMuted)
	}
}

// goal is the daily goal as a duration; 0 means none.
func (m *model) goal() time.Duration { return time.Duration(m.dailyGoal) * time.Minute }

// goalRow sets the daily goal and shows how far today is from it. days ends
// with today and holds at least a week.
func (m *model) goalRow(c *ui.Context, days []store.DayStat) {
	t := c.Theme()
	today := days[len(days)-1].Listened
	goal := m.goal()
	ui.Column(c).Gap(6).Shrink(0).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "每日目标").TextColor(t.TextMuted)
			v := float64(m.dailyGoal)
			if ui.Stepper(c, &v, 0, maxDailyGoal, dailyGoalStep).Label("调整每日目标").Changed() {
				m.dailyGoal = int(v)
				m.saveSettings()
				goal = m.goal()
			}
			if goal == 0 {
				ui.Text(c, "未设定").TextColor(t.TextMuted).Grow(1)
				return
			}
			ui.Text(c, fmtListen(goal)).Bold().Grow(1)
			met := 0
			for _, d := range days[len(days)-7:] {
				if d.Listened >= goal {
					met++
				}
			}
			ui.Textf(c, "近 7 天达标 %d 天", met).FontSize(12).TextColor(t.TextMuted)
			if today >= goal {
				ui.Text(c, "今天已达标").FontSize(12).TextColor(t.Success)
			} else {
				ui.Text(c, "今天还差 "+fmtListen(goal-today)).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if goal > 0 {
			ui.Row(c).Height(6).Background(t.Border).Radius(3).Children(func() {
				ui.Column(c).Height(6).WidthPercent(min(float32(today)/float32(goal), 1) * 100).Background(t.Accent).Radius(3)
			})
		}
	})
}

// fmtListen formats a listening time as "1 小时 05 分", "12 分" or "<1 分".
func fmtListen(d time.Duration) string {
	min := int(d.Round(time.Minute) / time.Minute)
	switch {
	case d <= 0:
		return "0 分"
	case min == 0:
		return "<1 分"
	case min < 60:
		return fmt.Sprintf("%d 分", min)
	}
	return fmt.Sprintf("%d 小时 %02d 分", min/60, min%60)
}

// materialStats ranks the materials by the time put into each.
func (m *model) materialStats(c *ui.Context) {
	t := c.Theme()
	rows := m.st.ByMaterial()
	if len(rows) == 0 {
		return
	}
	ui.Text(c, "每篇资料投入的时间").FontSize(12).TextColor(t.TextMuted).PaddingY(6)
	ui.Scroll(c).Grow(1).Gap(6).Children(func() {
		for _, r := range rows {
			ui.Column(c.Key(r.ID)).Gap(3).Children(func() {
				ui.Row(c).Gap(8).Children(func() {
					ui.Text(c, r.Title).MaxLines(1).Grow(1).MinWidth(0)
					ui.Text(c, fmtListen(r.Listened)).FontSize(12).TextColor(t.TextMuted)
				})
				ui.Row(c).Height(6).Background(t.Border).Radius(3).Children(func() {
					ui.Column(c).Height(6).WidthPercent(float32(r.Listened) / float32(rows[0].Listened) * 100).Background(t.Accent).Radius(3)
				})
			})
		}
	})
}

// heatLevel is 0 for an empty day, else 1..4 by share of full, the time that
// earns the darkest color: the daily goal, or the busiest day without one.
func heatLevel(d, full time.Duration) int {
	if d <= 0 || full <= 0 {
		return 0
	}
	return min(4, 1+int(float64(d)/float64(full)*4-1e-9)) // quarters of full
}

func heatColor(t *ui.Theme, level int) ui.Color {
	if level == 0 {
		return t.Text.Alpha(0.07)
	}
	return t.Accent.Alpha([]float32{0, 0.3, 0.5, 0.75, 1}[level])
}

// heatmap draws days (oldest first, the first a Sunday) as GitHub does: a
// column per week, a row per weekday, darker for longer listening.
func (m *model) heatmap(c *ui.Context, days []store.DayStat, peak time.Duration) {
	t := c.Theme()
	cols := (len(days) + 6) / 7
	step := float32(heatCell + heatGap)
	goal := m.goal()
	full, most := peak, "多"
	if goal > 0 {
		full, most = goal, "达标"
	}

	ui.Row(c).Gap(6).Children(func() {
		// weekday labels
		ui.Column(c).Gap(heatGap).Children(func() {
			ui.Row(c.Key("pad")).Height(16 - heatGap) // level with the grid, under the month labels
			for r, name := range []string{"", "一", "", "三", "", "五", ""} {
				ui.Row(c.Key(r)).Height(heatCell).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(10).TextColor(t.TextMuted)
				})
			}
		})
		ui.Column(c).Gap(4).Children(func() {
			// month labels: one segment per run of weeks in the same month
			ui.Row(c).Height(12).Children(func() {
				for k := 0; k < cols; {
					mo := days[k*7].Day.Month()
					j := k
					for j < cols && days[j*7].Day.Month() == mo {
						j++
					}
					label := ""
					if j-k >= 2 {
						label = fmt.Sprintf("%d月", int(mo))
					}
					ui.Row(c.Key(k)).Width(float32(j-k) * step).Children(func() {
						ui.Text(c, label).FontSize(10).TextColor(t.TextMuted)
					})
					k = j
				}
			})
			ui.Row(c).Gap(heatGap).Children(func() {
				for k := 0; k < cols; k++ {
					ui.Column(c.Key(k)).Gap(heatGap).Children(func() {
						for r := 0; r < 7; r++ {
							i := k*7 + r
							if i >= len(days) { // days after today
								ui.Column(c.Key(r)).Width(heatCell).Height(heatCell)
								continue
							}
							d := days[i]
							tip := d.Day.Format("1月2日") + "  " + fmtListen(d.Listened)
							if goal > 0 && d.Listened >= goal {
								tip += " · 达标"
							}
							ui.Column(c.Key(r)).Width(heatCell).Height(heatCell).Radius(2).
								Background(heatColor(t, heatLevel(d.Listened, full))).Tooltip(tip)
						}
					})
				}
			})
		})
	})
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "少").FontSize(10).TextColor(t.TextMuted)
		for l := 0; l <= 4; l++ {
			ui.Column(c.Key(l)).Width(10).Height(10).Radius(2).Background(heatColor(t, l))
		}
		ui.Text(c, most).FontSize(10).TextColor(t.TextMuted)
	})
}
