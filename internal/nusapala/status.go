package nusapala

import (
	"fmt"
	"log"
	"sync"
	"time"
)

type DeviceStatus struct {
	State     string    `json:"state"`
	Detail    string    `json:"detail"`
	Updated   time.Time `json:"updated"`
	Successes uint64    `json:"successes"`
	Errors    uint64    `json:"errors"`
}
type Event struct {
	At      time.Time `json:"at"`
	Device  string    `json:"device"`
	Message string    `json:"message"`
}
type Status struct {
	mu      sync.Mutex
	Devices map[string]DeviceStatus
	Events  []Event
	logger  *log.Logger
}

func NewStatus(logger *log.Logger) *Status {
	return &Status{Devices: map[string]DeviceStatus{}, logger: logger}
}
func (s *Status) Set(device, state, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Devices[device]
	changed := old.State != state || old.Detail != detail
	old.State = state
	old.Detail = detail
	old.Updated = time.Now()
	s.Devices[device] = old
	if changed {
		s.eventLocked(device, state+": "+detail)
	}
}
func (s *Status) Result(device string, err error, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.Devices[device]
	d.Updated = time.Now()
	if err != nil {
		d.Errors++
		d.State = "ERROR"
		d.Detail = err.Error()
	} else {
		d.Successes++
		d.State = "READY"
		d.Detail = detail
	}
	s.Devices[device] = d
	s.eventLocked(device, fmt.Sprintf("%s: %s", d.State, d.Detail))
}
func (s *Status) Event(device, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventLocked(device, message)
}
func (s *Status) eventLocked(device, message string) {
	s.Events = append(s.Events, Event{At: time.Now(), Device: device, Message: message})
	if len(s.Events) > 200 {
		s.Events = append([]Event(nil), s.Events[len(s.Events)-200:]...)
	}
	s.logger.Printf("[%s] %s", device, message)
}
func (s *Status) Snapshot() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := make(map[string]DeviceStatus, len(s.Devices))
	for k, v := range s.Devices {
		d[k] = v
	}
	return struct {
		Devices map[string]DeviceStatus `json:"devices"`
		Events  []Event                 `json:"events"`
	}{d, append([]Event{}, s.Events...)}
}

// Heartbeat updates counters without flooding the event history every second.
func (s *Status) Progress(device, detail string, frames uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.Devices[device]
	recovered := d.State != "READY"
	d.State = "READY"
	d.Detail = detail
	d.Updated = time.Now()
	d.Successes = frames
	s.Devices[device] = d
	if recovered {
		s.eventLocked(device, "READY: "+detail)
	}
}
