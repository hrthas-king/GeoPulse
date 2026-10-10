package ui

import (
	"testing"
	"unicode/utf16"

	"github.com/hrthas-king/GeoPulse/internal/app"
	"github.com/hrthas-king/GeoPulse/internal/ipinfo"
	"github.com/hrthas-king/GeoPulse/internal/monitor"
)

func TestOfflineCountryAndTooltipBudget(t *testing.T) {
	s := app.Snapshot{Status: monitor.Offline, Geo: ipinfo.Info{IP: "2001:4860:4860:abcd:1234:1234:1234:1234", Country: "US"},
		Rates: monitor.Rates{Up: 123456789, Down: 123456789, Valid: true}}
	if Country(s) != "--" {
		t.Fatal("offline must hide country")
	}
	if n := len(utf16.Encode([]rune(Tooltip(s)))); n > 127 {
		t.Fatalf("tooltip exceeds Win32 limit: %d", n)
	}
	if len(Pixels("CN", monitor.Online, true, true, true)) != 4096 {
		t.Fatal("wrong icon size")
	}
}
