package monitor

import (
	"testing"
	"time"
)

func TestFailureThresholdAndRecovery(t *testing.T) {
	var tr Tracker
	tr.Reset()
	for i, want := range []Status{Degraded, Offline, Offline} {
		tr.Apply(Probe{Uplink: UplinkUnknown})
		if tr.Status != want {
			t.Fatalf("failure %d: %s", i+1, tr.Status)
		}
	}
	if !tr.Apply(Probe{Success: true, Uplink: UplinkConfirmed}) || tr.Status != Online || tr.Failures != 0 {
		t.Fatal("successful reply must immediately recover")
	}
	tr.Apply(Probe{})
	tr.Apply(Probe{Success: true})
	tr.Apply(Probe{})
	if tr.Status != Degraded {
		t.Fatal("non-consecutive failures must not mark offline")
	}
}

func TestRatesResetAndActualElapsedTime(t *testing.T) {
	var s Sampler
	c := Counters{LUID: 1, Index: 2, Sent: 100, Received: 200, At: time.Unix(0, 0)}
	if s.Sample(c).Valid {
		t.Fatal("first sample is a baseline")
	}
	c.Sent, c.Received, c.At = 1100, 2200, c.At.Add(2*time.Second)
	r := s.Sample(c)
	if !r.Valid || r.Up != 500 || r.Down != 1000 {
		t.Fatalf("wrong rates: %+v", r)
	}
	c.LUID = 3
	if s.Sample(c).Valid {
		t.Fatal("interface switch must reset baseline")
	}
	c.At = c.At.Add(time.Second)
	c.Sent = 0
	if s.Sample(c).Valid {
		t.Fatal("counter rollback must not underflow")
	}
	c.At = c.At.Add(time.Second)
	c.Sent = 512
	c.Received += 256
	if !s.Sample(c).Valid {
		t.Fatal("baseline must recover")
	}
}
