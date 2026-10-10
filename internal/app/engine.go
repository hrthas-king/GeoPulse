// Package app 调度三条互不阻塞的工作线：网卡采样、心跳、事件驱动定位。
package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrthas-king/GeoPulse/internal/ipinfo"
	"github.com/hrthas-king/GeoPulse/internal/monitor"
)

const (
	Heartbeat     = time.Second
	GeoPoll       = 15 * time.Minute
	GeoCooldown   = 30 * time.Second
	EventDebounce = 750 * time.Millisecond
)

type Snapshot struct {
	Status          monitor.Status `json:"status"`
	Failures        int            `json:"consecutive_failures"`
	Rates           monitor.Rates  `json:"rates"`
	Probe           monitor.Probe  `json:"probe"`
	Geo             ipinfo.Info    `json:"geo"`
	GeoRefreshing   bool           `json:"geo_refreshing"`
	GeoError        string         `json:"geo_error,omitempty"`
	NIC             string         `json:"nic"`
	NICError        string         `json:"nic_error,omitempty"`
	WatchError      string         `json:"watch_error,omitempty"`
	IntervalSeconds int            `json:"display_interval_seconds"`
	At              time.Time      `json:"at"`
}

type Dependencies struct {
	ReadCounters  func(string) (monitor.Counters, error)
	Ping          func(string) monitor.Probe
	Fetch         func(context.Context) (ipinfo.Info, error)
	NetworkEvents <-chan struct{}
	WatchError    string
}

type Engine struct {
	deps     Dependencies
	Refresh  chan struct{}
	Interval chan int
}

func New(deps Dependencies) *Engine {
	return &Engine{deps: deps, Refresh: make(chan struct{}, 1), Interval: make(chan int, 1)}
}

func ValidInterval(seconds int) bool { return seconds == 1 || seconds == 2 || seconds == 5 }

// gate 限制所有触发（包括菜单刷新）的请求起始间隔，并保留被限流的请求。
type gate struct{ last time.Time }

func (g *gate) wait(now time.Time) time.Duration {
	if g.last.IsZero() {
		return 0
	}
	if remaining := g.last.Add(GeoCooldown).Sub(now); remaining > 0 {
		return remaining
	}
	return 0
}

type counterResult struct {
	value monitor.Counters
	err   error
	epoch uint64
}
type probeResult struct {
	value monitor.Probe
	epoch uint64
}
type geoResult struct {
	value ipinfo.Info
	err   error
	epoch uint64
}

func (e *Engine) Run(ctx context.Context, seconds int, publish func(Snapshot)) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	if !ValidInterval(seconds) {
		seconds = 1
	}
	counters := make(chan counterResult, 1)
	probes := make(chan probeResult, 1)
	geos := make(chan geoResult, 1)
	var generation atomic.Uint64
	var target atomic.Value
	target.Store("1.1.1.1")
	// 系统 API 调用和定位请求都不在 UI 线程或状态聚合线程执行。
	workers.Add(2)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(Heartbeat)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			gen := generation.Load()
			c, err := e.deps.ReadCounters(target.Load().(string))
			select {
			case counters <- counterResult{c, err, gen}:
			case <-ctx.Done():
				return
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(Heartbeat)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			gen := generation.Load()
			address := target.Load().(string)
			p := e.deps.Ping(address)
			select {
			case probes <- probeResult{p, gen}:
			case <-ctx.Done():
				return
			}
			// 下一秒才尝试备用目标，保证不在同一周期发两包。
			if !p.Success {
				if address == "1.1.1.1" {
					target.Store("8.8.8.8")
				} else {
					target.Store("1.1.1.1")
				}
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()

	var tracker monitor.Tracker
	tracker.Reset()
	var sampler monitor.Sampler
	snapshot := Snapshot{Status: monitor.Unknown, IntervalSeconds: seconds, WatchError: e.deps.WatchError}
	display := time.NewTicker(time.Duration(seconds) * time.Second)
	defer display.Stop()
	poll := time.NewTicker(GeoPoll)
	defer poll.Stop()
	var limiter gate
	var pending, busy bool
	var refreshTimer *time.Timer
	var refreshC <-chan time.Time
	var debounceUntil time.Time
	var fetchCancel context.CancelFunc
	defer func() {
		if refreshTimer != nil {
			refreshTimer.Stop()
		}
		if fetchCancel != nil {
			fetchCancel()
		}
	}()
	emit := func() {
		snapshot.At = time.Now()
		snapshot.Status = tracker.Status
		snapshot.Failures = tracker.Failures
		publish(snapshot)
	}
	// 所有定时器由聚合循环独占。事件洪峰合并成一次请求，离线时等心跳恢复。
	arm := func() {
		if refreshTimer != nil {
			refreshTimer.Stop()
		}
		refreshC = nil
		if !pending || busy || tracker.Status == monitor.Offline {
			return
		}
		now := time.Now()
		wait := limiter.wait(now)
		if debounce := debounceUntil.Sub(now); debounce > wait {
			wait = debounce
		}
		if refreshTimer == nil {
			refreshTimer = time.NewTimer(wait)
		} else {
			refreshTimer.Reset(wait)
		}
		refreshC = refreshTimer.C
	}
	request := func() { pending = true; arm() }
	emit()
	request()
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-counters:
			if r.epoch != generation.Load() {
				continue
			}
			if r.err != nil {
				sampler.Reset()
				snapshot.Rates = monitor.Rates{}
				snapshot.NIC = ""
				snapshot.NICError = r.err.Error()
			} else {
				snapshot.Rates = sampler.Sample(r.value)
				snapshot.NIC = r.value.Name
				snapshot.NICError = ""
			}
		case r := <-probes:
			if r.epoch != generation.Load() {
				continue
			}
			previous := tracker.Status
			recovered := tracker.Apply(r.value)
			snapshot.Probe = r.value
			// 首次成功和一次丢包恢复已有启动/事件请求，不重复查询定位。
			if recovered && previous == monitor.Offline {
				request()
			}
			if tracker.Status != previous {
				emit()
				arm()
			}
		case <-e.deps.NetworkEvents:
			generation.Add(1)
			tracker.Reset()
			sampler.Reset()
			// 网络变更立即清除旧出口位置；旧异步结果不能覆盖新网络。
			snapshot.Geo = ipinfo.Info{}
			snapshot.GeoError = ""
			snapshot.Rates = monitor.Rates{}
			snapshot.Probe = monitor.Probe{Uplink: monitor.UplinkUnknown}
			if fetchCancel != nil {
				fetchCancel()
			}
			debounceUntil = time.Now().Add(EventDebounce)
			request()
			emit()
		case <-e.Refresh:
			request()
			emit()
		case n := <-e.Interval:
			if ValidInterval(n) {
				snapshot.IntervalSeconds = n
				display.Reset(time.Duration(n) * time.Second)
				emit()
			}
		case <-poll.C:
			request()
		case <-refreshC:
			refreshC = nil
			if !pending || busy || tracker.Status == monitor.Offline {
				continue
			}
			pending = false
			busy = true
			snapshot.GeoRefreshing = true
			limiter.last = time.Now()
			gen := generation.Load()
			fetchCtx, lookupCancel := context.WithTimeout(ctx, 8*time.Second)
			fetchCancel = lookupCancel
			emit()
			go func() {
				defer lookupCancel()
				info, err := e.deps.Fetch(fetchCtx)
				select {
				case geos <- geoResult{info, err, gen}:
				case <-ctx.Done():
				}
			}()
		case r := <-geos:
			busy = false
			snapshot.GeoRefreshing = false
			if fetchCancel != nil {
				fetchCancel()
				fetchCancel = nil
			}
			if r.epoch == generation.Load() {
				if r.err != nil {
					snapshot.GeoError = r.err.Error()
				} else {
					snapshot.Geo = r.value
					snapshot.GeoError = ""
				}
			}
			arm()
			emit()
		case <-display.C:
			emit()
		}
	}
}
