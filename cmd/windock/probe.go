package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BjarkeM/windock-go/internal/monitor"
	"github.com/BjarkeM/windock-go/internal/win"
	"github.com/BjarkeM/windock-go/internal/zones"
)

// cmdProbe follows the cursor and reports which trigger it is inside.
// "nothing happens" may mean the rule is wrong or that the
// pointer never reached it. The extremes line shows how far it travelled.
func cmdProbe(path string) error {
	if !win.SetProcessDpiAwarenessContextV2() {
		fmt.Println("note: per-monitor DPI awareness unavailable; coordinates may be virtualised")
	}
	cfg, _, err := loadConfig(path)
	if err != nil {
		return err
	}
	mons := monitor.Enumerate()
	layout := zones.Resolve(cfg.ActiveRules(), mons, cfg.GlobalSettings)
	rules := cfg.ActiveRules()

	fmt.Printf("Profile: %s  (%d active triggers)\n", profileName(cfg), layout.Len())
	for _, m := range mons.All() {
		fmt.Printf("Monitor %d: bounds %s\n", m.Index, rectStr(m.Bounds))
	}
	fmt.Println()
	fmt.Println("Move the pointer around, and push it hard into a screen edge.")
	fmt.Println("The extremes line shows how far it actually got, which tells you")
	fmt.Println("whether a trigger is unreachable or merely being missed.")
	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	const big = int32(1) << 30
	var (
		last       = -1
		minX, minY = big, big
		maxX, maxY = -big, -big
		hits       = map[int]int{}
		prev       win.POINT
	)

	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println()
			fmt.Println()
			fmt.Printf("Pointer reached x %d..%d, y %d..%d\n", minX, maxX, minY, maxY)
			fmt.Println("Triggers entered during this run:")
			if len(hits) == 0 {
				fmt.Println("  (none - the pointer never entered a trigger region)")
			}
			for i, r := range rules {
				if n := hits[i]; n > 0 {
					fmt.Printf("  rule [%2d] %-44s entered %d time(s)\n", i, describeRule(r), n)
				}
			}
			return nil

		case <-ticker.C:
			p := win.GetCursorPos()
			if p == prev {
				continue
			}
			prev = p
			minX, maxX = minInt32(minX, p.X), maxInt32(maxX, p.X)
			minY, maxY = minInt32(minY, p.Y), maxInt32(maxY, p.Y)

			idx := layout.MatchIndex(p)
			if idx != last {
				last = idx
				fmt.Println()
				if idx < 0 {
					fmt.Printf("  (%d,%d) left the trigger\n", p.X, p.Y)
				} else {
					z := layout.Zones()[idx]
					hits[z.RuleIndex]++
					fmt.Printf("  (%d,%d) INSIDE rule [%d] %s\n",
						p.X, p.Y, z.RuleIndex, describeRule(rules[z.RuleIndex]))
					fmt.Printf("           trigger %s\n", rectStr(z.Hit))
					fmt.Printf("           would dock to %s\n", rectStr(z.Dock))
				}
			}

			state := "no trigger"
			if idx >= 0 {
				state = fmt.Sprintf("rule [%d]", layout.Zones()[idx].RuleIndex)
			}
			fmt.Printf("\rcursor (%5d,%5d)  %-12s  reached x %d..%d  y %d..%d   ",
				p.X, p.Y, state, minX, maxX, minY, maxY)
		}
	}
}

func minInt32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
