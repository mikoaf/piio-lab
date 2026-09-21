package application

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"piio-lab/internal/domain"
)

type Manager struct {
	cfg         domain.Config
	logger      domain.Logger
	repository  domain.USBRepository
	initializer domain.DeviceInitializer
	events      domain.USBEventWatcher
	mu          sync.RWMutex
	states      map[string]domain.DeviceState
}

func NewManager(cfg domain.Config, logger domain.Logger, repository domain.USBRepository, initializer domain.DeviceInitializer, events domain.USBEventWatcher) *Manager {
	return &Manager{cfg: cfg, logger: logger, repository: repository, initializer: initializer, events: events, states: make(map[string]domain.DeviceState)}
}

func (m *Manager) InitializeAll() {
	devices, err := m.repository.Scan()
	if err != nil {
		m.logger.Printf("[ERROR] Pemindaian USB gagal: %v", err)
		return
	}
	used := make(map[string]bool)
	m.logger.Println("[INIT] Memulai inisialisasi berurutan: printer -> scanner -> audio -> esp32")
	for _, step := range m.steps() {
		state := m.discoverRole(step.role, step.selector, devices, used)
		if state.Connected {
			used[state.Device.SysPath] = true
		}
		state = m.initializeWithRetry(state, 5, 250*time.Millisecond)
		m.setState(state)
		m.logInitialState(state)
	}
	m.printSummary()
}

func (m *Manager) steps() []struct {
	role     string
	selector domain.Selector
} {
	return []struct {
		role     string
		selector domain.Selector
	}{
		{domain.RolePrinter, m.cfg.Printer},
		{domain.RoleScanner, m.cfg.Scanner.Selector},
		{domain.RoleAudio, m.cfg.Audio},
		{domain.RoleESP32, m.cfg.ESP32.Selector},
	}
}

func (m *Manager) discoverAll() map[string]domain.DeviceState {
	devices, err := m.repository.Scan()
	if err != nil {
		m.logger.Printf("[ERROR] Pemindaian ulang USB gagal: %v", err)
		return nil
	}
	used := make(map[string]bool)
	result := make(map[string]domain.DeviceState)
	for _, step := range m.steps() {
		s := m.discoverRole(step.role, step.selector, devices, used)
		if s.Connected {
			used[s.Device.SysPath] = true
		}
		result[step.role] = s
	}
	return result
}

func (m *Manager) discoverRole(role string, selector domain.Selector, devices []domain.USBDevice, used map[string]bool) domain.DeviceState {
	var candidates []domain.USBDevice
	for _, d := range devices {
		if !used[d.SysPath] && matchesSelector(d, selector) && roleCompatible(role, d, m.cfg.Scanner.Mode) {
			candidates = append(candidates, d)
		}
	}
	if len(candidates) == 0 {
		for _, d := range devices {
			if !used[d.SysPath] && selectorIDsMatch(d, selector) && roleCompatible(role, d, m.cfg.Scanner.Mode) {
				candidates = append(candidates, d)
			}
		}
	}
	if len(candidates) == 0 {
		return domain.DeviceState{Role: role, Err: "perangkat tidak ditemukan"}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].SysName < candidates[j].SysName })
	d := candidates[0]
	return domain.DeviceState{Role: role, Connected: true, Device: d, Detail: d.DisplayName()}
}

func matchesSelector(d domain.USBDevice, selector domain.Selector) bool {
	if !selectorIDsMatch(d, selector) {
		return false
	}
	if len(selector.NameContains) == 0 {
		return true
	}
	text := d.AllText()
	for _, keyword := range selector.NameContains {
		if keyword != "" && strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func selectorIDsMatch(d domain.USBDevice, selector domain.Selector) bool {
	return (selector.VendorID == "" || strings.EqualFold(selector.VendorID, d.VendorID)) &&
		(selector.ProductID == "" || strings.EqualFold(selector.ProductID, d.ProductID))
}

func roleCompatible(role string, d domain.USBDevice, scannerMode string) bool {
	switch role {
	case domain.RolePrinter:
		return len(d.Nodes["printer"]) > 0 || strings.Contains(d.AllText(), "printer") || strings.Contains(d.AllText(), "epson")
	case domain.RoleScanner:
		if scannerMode == "serial" {
			return len(d.Nodes["tty"]) > 0
		}
		return len(d.Nodes["input"]) > 0
	case domain.RoleAudio:
		return len(d.Nodes["capture"]) > 0
	case domain.RoleESP32:
		return len(d.Nodes["tty"]) > 0
	default:
		return false
	}
}

func (m *Manager) initializeWithRetry(state domain.DeviceState, attempts int, delay time.Duration) domain.DeviceState {
	if !state.Connected || attempts < 2 {
		return m.initializer.Initialize(state)
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			time.Sleep(delay)
			fresh := m.discoverAll()
			if candidate, ok := fresh[state.Role]; ok && candidate.Connected {
				state = candidate
			}
		}
		state = m.initializer.Initialize(state)
		if state.Ready {
			return state
		}
		if state.Err == domain.ErrPaperOut.Error() {
			return state
		}
	}
	return state
}

func (m *Manager) State(role string) domain.DeviceState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[role]
}

func (m *Manager) States() []domain.DeviceState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	states := make([]domain.DeviceState, 0, len(domain.Roles))
	for _, role := range domain.Roles {
		states = append(states, m.states[role])
	}
	return states
}

func (m *Manager) setState(state domain.DeviceState) {
	m.mu.Lock()
	m.states[state.Role] = state
	m.mu.Unlock()
}

func (m *Manager) OperationError(role string, err error) {
	m.logger.Printf("[READ ERROR] %s: %v", strings.ToUpper(role), err)
	state := m.State(role)
	state.Ready = false
	state.Err = err.Error()
	m.setState(state)
}

func (m *Manager) MarkNotReady(role string, err error) {
	state := m.State(role)
	state.Ready = false
	state.Err = err.Error()
	m.setState(state)
}

func (m *Manager) Monitor(ctx context.Context) {
	trigger := make(chan struct{}, 1)
	go m.events.Watch(ctx, trigger)
	ticker := time.NewTicker(time.Duration(m.cfg.MonitorIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-trigger:
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			m.refresh()
		case <-ticker.C:
			m.refresh()
		}
	}
}

func (m *Manager) refresh() {
	discovered := m.discoverAll()
	if discovered == nil {
		return
	}
	for _, role := range domain.Roles {
		old, fresh := m.State(role), discovered[role]
		switch {
		case old.Connected && !fresh.Connected:
			m.logger.Printf("[DISCONNECTED] %s %s | USB port %s | node %s", strings.ToUpper(role), old.Device.DisplayName(), old.Device.SysName, old.Node)
			m.setState(fresh)
		case !old.Connected && fresh.Connected:
			m.logger.Printf("[CONNECTED] %s %s | USB port %s | USB ID %s:%s", strings.ToUpper(role), fresh.Device.DisplayName(), fresh.Device.SysName, fresh.Device.VendorID, fresh.Device.ProductID)
			m.logger.Printf("[REINITIALIZING] %s", strings.ToUpper(role))
			fresh = m.initializeWithRetry(fresh, 8, 250*time.Millisecond)
			m.setState(fresh)
			m.logReinit(fresh)
		case old.Connected && fresh.Connected && old.Device.SysName != fresh.Device.SysName:
			m.logger.Printf("[CHANGED] %s berpindah/terenumerasi: USB port %s -> %s", strings.ToUpper(role), old.Device.SysName, fresh.Device.SysName)
			fresh = m.initializeWithRetry(fresh, 8, 250*time.Millisecond)
			m.setState(fresh)
			m.logReinit(fresh)
		case old.Connected && fresh.Connected && stateLocation(old) != stateLocation(fresh):
			m.logger.Printf("[NODE UPDATED] %s | USB port %s | node baru tersedia", strings.ToUpper(role), fresh.Device.SysName)
			fresh = m.initializeWithRetry(fresh, 4, 200*time.Millisecond)
			m.setState(fresh)
			m.logReinit(fresh)
		case old.Connected && fresh.Connected && !old.Ready:
			fresh = m.initializer.Initialize(fresh)
			m.setState(fresh)
			if fresh.Ready {
				m.logReinit(fresh)
			}
		case old.Connected && fresh.Connected && old.Ready:
			checked := m.initializer.Check(old)
			if !checked.Ready {
				m.setState(checked)
				m.logger.Printf("[NOT READY] %s: %s", strings.ToUpper(role), checked.Err)
			}
		}
	}
}

func stateLocation(state domain.DeviceState) string {
	return state.Device.Identity() + "|" + state.Device.SysName + "|" + strings.Join(flattenNodes(state.Device.Nodes), ",")
}

func flattenNodes(nodes map[string][]string) []string {
	set := make(map[string]struct{})
	for _, list := range nodes {
		for _, node := range list {
			set[node] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for node := range set {
		out = append(out, node)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) logInitialState(state domain.DeviceState) {
	label := strings.ToUpper(state.Role)
	if state.Ready {
		m.logger.Printf("[OK] %-7s %s | USB port %s | node %s", label, state.Detail, state.Device.SysName, state.Node)
	} else if state.Connected {
		m.logger.Printf("[WARNING] %-7s %s terdeteksi di %s, tetapi belum siap: %s", label, state.Detail, state.Device.SysName, state.Err)
	} else {
		m.logger.Printf("[ERROR] %-7s %s", label, state.Err)
	}
}

func (m *Manager) printSummary() {
	ready, connected := 0, 0
	for _, state := range m.States() {
		if state.Connected {
			connected++
		}
		if state.Ready {
			ready++
		}
	}
	m.logger.Printf("[INIT] Selesai: %d terhubung, %d siap digunakan", connected, ready)
}

func (m *Manager) logReinit(state domain.DeviceState) {
	if state.Ready {
		m.logger.Printf("[READY] %s %s | USB port %s | node %s", strings.ToUpper(state.Role), state.Device.DisplayName(), state.Device.SysName, state.Node)
	} else {
		m.logger.Printf("[INIT FAILED] %s: %s", strings.ToUpper(state.Role), state.Err)
	}
}
