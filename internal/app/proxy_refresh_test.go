package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hrthas-king/GeoPulse/internal/ipinfo"
	"github.com/hrthas-king/GeoPulse/internal/monitor"
)

func TestManualRefreshUpdatesChangedEgressPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var changed atomic.Bool
	engine := New(Dependencies{
		ReadCounters: func(string) (monitor.Counters, error) { return monitor.Counters{At: time.Now()}, nil },
		Ping:         func(string) monitor.Probe { return monitor.Probe{Success: true, At: time.Now()} },
		Fetch: func(context.Context) (ipinfo.Info, error) {
			if changed.Load() {
				return ipinfo.Info{IP: "8.8.8.8", Country: "JP"}, nil
			}
			return ipinfo.Info{IP: "1.1.1.1", Country: "TH"}, nil
		},
	})
	snapshots := make(chan Snapshot, 32)
	done := make(chan struct{})
	go func() { engine.Run(ctx, 1, func(s Snapshot) { snapshots <- s }); close(done) }()
	defer func() { cancel(); <-done }()
	first := time.After(2 * time.Second)
	for {
		select {
		case s := <-snapshots:
			if s.Geo.Country == "TH" {
				goto ready
			}
		case <-first:
			t.Fatal("initial location did not arrive")
		}
	}
ready:
	changed.Store(true)
	engine.Refresh <- struct{}{}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case s := <-snapshots:
			if s.Geo.Country == "JP" {
				return
			}
		case <-deadline:
			t.Fatal("manual refresh kept the old country after egress changed")
		}
	}
}

func TestTunnelSwitchWithoutNICEventRefreshesLocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var changed atomic.Bool
	events := make(chan monitor.EgressEvent, 1)
	engine := New(Dependencies{
		ReadCounters: func(string) (monitor.Counters, error) { return monitor.Counters{At: time.Now()}, nil },
		Ping:         func(string) monitor.Probe { return monitor.Probe{Success: true, At: time.Now()} },
		Fetch: func(context.Context) (ipinfo.Info, error) {
			if changed.Load() {
				return ipinfo.Info{IP: "8.8.8.8", Country: "JP"}, nil
			}
			return ipinfo.Info{IP: "1.1.1.1", Country: "TH"}, nil
		}, EgressEvents: events,
	})
	// 只缩短时钟，仍执行真实 TUN 调度分支；不伪造任何 NIC 变化事件。
	engine.tunnelPoll = 30 * time.Millisecond
	engine.geoCooldown = 0
	snapshots := make(chan Snapshot, 32)
	done := make(chan struct{})
	go func() { engine.Run(ctx, 1, func(s Snapshot) { snapshots <- s }); close(done) }()
	defer func() { cancel(); <-done }()
	first := time.After(time.Second)
	for {
		select {
		case s := <-snapshots:
			if s.Geo.Country == "TH" {
				goto ready
			}
		case <-first:
			t.Fatal("initial location did not arrive")
		}
	}
ready:
	events <- monitor.EgressEvent{Active: true}
	changed.Store(true)
	deadline := time.After(time.Second)
	for {
		select {
		case s := <-snapshots:
			if s.Geo.Country == "JP" {
				return
			}
		case <-deadline:
			t.Fatal("TUN switched region with unchanged NIC; location never refreshed")
		}
	}
}

func TestOrdinaryProxyActivityDoesNotEraseLocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan monitor.EgressEvent, 1)
	engine := New(Dependencies{
		ReadCounters: func(string) (monitor.Counters, error) { return monitor.Counters{At: time.Now()}, nil },
		Ping:         func(string) monitor.Probe { return monitor.Probe{Success: true, At: time.Now()} },
		Fetch:        func(context.Context) (ipinfo.Info, error) { return ipinfo.Info{IP: "1.1.1.1", Country: "TH"}, nil },
		EgressEvents: events,
	})
	snapshots := make(chan Snapshot, 32)
	done := make(chan struct{})
	go func() { engine.Run(ctx, 1, func(s Snapshot) { snapshots <- s }); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.After(time.Second)
	for {
		select {
		case s := <-snapshots:
			if s.Geo.Country == "TH" {
				goto ready
			}
		case <-deadline:
			t.Fatal("initial country did not arrive")
		}
	}
ready:
	// 代理普通建连同样会改变端点集合，不能把它当作已确认的出口切换。
	events <- monitor.EgressEvent{Active: true, Changed: true}
	for {
		select {
		case s := <-snapshots:
			if s.EgressActive {
				if s.Geo.Country != "TH" {
					t.Fatal("ordinary proxy connection erased the current country")
				}
				return
			}
		case <-deadline:
			t.Fatal("proxy event not consumed")
		}
	}
}

func TestConfirmedNetworkChangeRefreshesPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var changed atomic.Bool
	events := make(chan struct{}, 1)
	engine := New(Dependencies{
		ReadCounters: func(string) (monitor.Counters, error) { return monitor.Counters{At: time.Now()}, nil },
		Ping:         func(string) monitor.Probe { return monitor.Probe{Success: true, At: time.Now()} },
		Fetch: func(context.Context) (ipinfo.Info, error) {
			if changed.Load() {
				return ipinfo.Info{IP: "8.8.8.8", Country: "JP"}, nil
			}
			return ipinfo.Info{IP: "1.1.1.1", Country: "TH"}, nil
		},
		NetworkEvents: events,
	})
	snapshots := make(chan Snapshot, 32)
	done := make(chan struct{})
	go func() { engine.Run(ctx, 1, func(s Snapshot) { snapshots <- s }); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.After(time.Second)
	for {
		select {
		case s := <-snapshots:
			if s.Geo.Country == "TH" {
				goto ready
			}
		case <-deadline:
			t.Fatal("initial country missing")
		}
	}
ready:
	changed.Store(true)
	events <- struct{}{}
	deadline = time.After(3 * time.Second)
	for {
		select {
		case s := <-snapshots:
			if s.Geo.Country == "JP" {
				return
			}
		case <-deadline:
			t.Fatal("confirmed network change waited for 30s cooldown")
		}
	}
}
