// Package monitor 包含与操作系统无关的速率计算和连通性状态机。
package monitor

import "time"

type Status string

const (
	Unknown  Status = "unknown"
	Online   Status = "online"
	Degraded Status = "degraded"
	Offline  Status = "offline"
)

// 上行证据不能从 NIC 总发送量推断：它可能仅是局域网或其他程序的流量。
type Uplink string

const (
	UplinkConfirmed  Uplink = "confirmed"
	UplinkUnknown    Uplink = "unknown"
	UplinkLocalError Uplink = "local-error"
)

type Probe struct {
	Success bool          `json:"success"`
	Uplink  Uplink        `json:"uplink"`
	RTT     time.Duration `json:"rtt_ns"`
	Target  string        `json:"target"`
	Detail  string        `json:"detail,omitempty"`
	At      time.Time     `json:"at"`
}

type Tracker struct {
	Status   Status
	Failures int
}

func (t *Tracker) Reset() { t.Status, t.Failures = Unknown, 0 }

// Apply 在第二次连续失败时告警；一个成功响应即可恢复。
// 返回 true 表示从故障/未知状态恢复，调用者可请求一次 IP 定位。
func (t *Tracker) Apply(p Probe) bool {
	old := t.Status
	if p.Success {
		t.Status, t.Failures = Online, 0
		return old != Online
	}
	if t.Failures < 2 {
		t.Failures++
	}
	if t.Failures >= 2 {
		t.Status = Offline
	} else {
		t.Status = Degraded
	}
	return false
}

// Counters 是有效出口接口的 64 位累计计数；LUID 避免接口索引复用。
type Counters struct {
	LUID           uint64
	Index          uint32
	Name           string
	Sent, Received uint64
	At             time.Time
}

type Rates struct {
	Up    float64 `json:"up_bytes_per_second"`
	Down  float64 `json:"down_bytes_per_second"`
	Valid bool    `json:"valid"`
}

// EgressEvent 来自本地 TUN/代理配置或传输端点变化，不发送公网请求。
type EgressEvent struct {
	Active  bool
	Changed bool
}

type Sampler struct{ previous *Counters }

func (s *Sampler) Reset() { s.previous = nil }

func (s *Sampler) Sample(c Counters) Rates {
	prev := s.previous
	s.previous = &c
	if prev == nil || prev.LUID != c.LUID || prev.Index != c.Index ||
		c.Sent < prev.Sent || c.Received < prev.Received {
		return Rates{}
	}
	seconds := c.At.Sub(prev.At).Seconds()
	if seconds <= 0 {
		return Rates{}
	}
	return Rates{Up: float64(c.Sent-prev.Sent) / seconds,
		Down: float64(c.Received-prev.Received) / seconds, Valid: true}
}
