package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hrthas-king/GeoPulse/internal/ipinfo"
	"github.com/hrthas-king/GeoPulse/internal/monitor"
)

func TestRefreshGate(t *testing.T) {
	var g gate
	now := time.Unix(100, 0)
	if g.wait(now) != 0 {
		t.Fatal("first attempt delayed")
	}
	g.last = now
	if g.wait(now.Add(time.Second)) != 29*time.Second {
		t.Fatal("request storm not limited")
	}
	if g.wait(now.Add(GeoCooldown)) != 0 {
		t.Fatal("queued attempt not allowed at cooldown")
	}
}

func TestDisplayIntervalDoesNotSlowHeartbeat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	beats := make(chan time.Time, 8)
	var reads atomic.Int32
	engine := New(Dependencies{
		ReadCounters: func(string) (monitor.Counters, error) { reads.Add(1); return monitor.Counters{At: time.Now()}, nil },
		Ping:         func(string) monitor.Probe { beats <- time.Now(); return monitor.Probe{Success: true, At: time.Now()} },
		Fetch:        func(context.Context) (ipinfo.Info, error) { return ipinfo.Info{IP: "1.1.1.1", Country: "US"}, nil },
	})
	done := make(chan struct{})
	go func() { engine.Run(ctx, 5, func(Snapshot) {}); close(done) }()
	deadline := time.After(4 * time.Second)
	var first time.Time
	for i := 0; i < 3; i++ {
		select {
		case at := <-beats:
			if i == 0 {
				first = at
			}
			if i == 2 && at.Sub(first) > 3*time.Second {
				t.Fatal("5s display interval slowed heartbeat")
			}
		case <-deadline:
			t.Fatal("heartbeat did not keep 1s cadence")
		}
	}
	cancel()
	<-done
	if reads.Load() < 2 {
		t.Fatal("NIC sampling slowed with display interval")
	}
}

func TestNetworkChangeDiscardsInflightLocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan struct{}, 1)
	started, release := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	engine := New(Dependencies{
		ReadCounters: func(string) (monitor.Counters, error) { return monitor.Counters{At: time.Now()}, nil },
		Ping:         func(string) monitor.Probe { return monitor.Probe{Success: true, At: time.Now()} },
		Fetch: func(context.Context) (ipinfo.Info, error) {
			if count.Add(1) == 1 {
				close(started)
			}
			<-release
			return ipinfo.Info{IP: "1.1.1.1", Country: "US"}, nil
		}, NetworkEvents: events,
	})
	snapshots := make(chan Snapshot, 32)
	done := make(chan struct{})
	go func() { engine.Run(ctx, 5, func(s Snapshot) { snapshots <- s }); close(done) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("initial lookup did not start")
	}
	// 明确等到事件已被处理，再让旧请求返回。
	events <- struct{}{}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case s := <-snapshots:
			if s.Status == monitor.Unknown && s.GeoRefreshing && s.Probe.Uplink == monitor.UplinkUnknown {
				goto eventProcessed
			}
		case <-deadline:
			t.Fatal("network change not processed")
		}
	}
eventProcessed:
	close(release)
	for {
		select {
		case s := <-snapshots:
			if s.Geo.IP != "" {
				t.Fatal("stale network location was published")
			}
			if !s.GeoRefreshing {
				if count.Load() != 1 {
					t.Fatal("cooldown allowed an extra request")
				}
				cancel()
				<-done
				return
			}
		case <-deadline:
			t.Fatal("old lookup did not finish")
		}
	}
}
