package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"piio-lab/internal/domain"
)

type gatewayStub struct {
	mu     sync.Mutex
	prints []string
	err    error
}

func (g *gatewayStub) ListenKeyboard(context.Context, string, func(string)) error { return nil }
func (g *gatewayStub) ListenSerial(context.Context, string, int, func(string)) error {
	return nil
}
func (g *gatewayStub) PrintQR(_ string, value string, _ time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prints = append(g.prints, value)
	return g.err
}

type loggerStub struct {
	mu    sync.Mutex
	lines []string
}

func (l *loggerStub) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, format)
}
func (l *loggerStub) Println(args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, "line")
}

func TestPrinterWorkerPrintsOnlyWhenReady(t *testing.T) {
	manager := &Manager{states: map[string]domain.DeviceState{
		domain.RolePrinter: {Role: domain.RolePrinter, Connected: true, Ready: true, Node: "/dev/usb/lp0"},
	}}
	gateway := &gatewayStub{}
	background := NewBackground(domain.Config{}, manager, gateway, &loggerStub{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		background.runPrinter(ctx)
		close(done)
	}()
	background.prints <- printRequest{value: "QR-123", time: time.Now()}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		gateway.mu.Lock()
		count := len(gateway.prints)
		gateway.mu.Unlock()
		if count == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if len(gateway.prints) != 1 || gateway.prints[0] != "QR-123" {
		t.Fatalf("prints = %#v, want one QR print", gateway.prints)
	}
}

func TestPrinterWorkerDropsRequestWhenNotReady(t *testing.T) {
	manager := &Manager{states: map[string]domain.DeviceState{
		domain.RolePrinter: {Role: domain.RolePrinter, Connected: true, Ready: false, Err: "paper out"},
	}}
	gateway := &gatewayStub{}
	background := NewBackground(domain.Config{}, manager, gateway, &loggerStub{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		background.runPrinter(ctx)
		close(done)
	}()
	background.prints <- printRequest{value: "QR-123", time: time.Now()}
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done

	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if len(gateway.prints) != 0 {
		t.Fatalf("prints = %#v, want dropped request", gateway.prints)
	}
}
