package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"piio-lab/internal/domain"
)

const maxQRLength = 4096

type printRequest struct {
	value string
	time  time.Time
}

type Background struct {
	cfg     domain.Config
	manager *Manager
	gateway domain.PeripheralGateway
	logger  domain.Logger
	prints  chan printRequest
	wg      sync.WaitGroup
}

func NewBackground(cfg domain.Config, manager *Manager, gateway domain.PeripheralGateway, logger domain.Logger) *Background {
	return &Background{
		cfg: cfg, manager: manager, gateway: gateway, logger: logger,
		prints: make(chan printRequest, 32),
	}
}

func (b *Background) Start(ctx context.Context) {
	b.wg.Add(3)
	go func() {
		defer b.wg.Done()
		b.runScanner(ctx)
	}()
	go func() {
		defer b.wg.Done()
		b.runESP32(ctx)
	}()
	go func() {
		defer b.wg.Done()
		b.runPrinter(ctx)
	}()
}

func (b *Background) Wait() {
	b.wg.Wait()
}

func (b *Background) runScanner(ctx context.Context) {
	for waitUntilReady(ctx, b.manager, domain.RoleScanner) {
		state := b.manager.State(domain.RoleScanner)
		b.logger.Printf("[LISTEN] SCANNER %s di %s", b.cfg.Scanner.Mode, state.Node)
		emit := func(value string) {
			value = strings.TrimSpace(value)
			if value == "" {
				return
			}
			if len(value) > maxQRLength {
				b.logger.Printf("[QR REJECTED] data melebihi batas %d byte", maxQRLength)
				return
			}
			b.logger.Printf("[QR] %s", value)
			printer := b.manager.State(domain.RolePrinter)
			if !printer.Ready {
				b.logger.Printf("[PRINT SKIPPED] printer tidak READY; request dibatalkan: %s", printer.Err)
				return
			}
			select {
			case b.prints <- printRequest{value: value, time: time.Now()}:
			default:
				b.logger.Println("[PRINT SKIPPED] antrean cetak penuh; request dibatalkan")
			}
		}

		var err error
		if b.cfg.Scanner.Mode == "serial" {
			err = b.gateway.ListenSerial(ctx, state.Node, b.cfg.Scanner.BaudRate, emit)
		} else {
			err = b.gateway.ListenKeyboard(ctx, state.Node, emit)
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("pembacaan scanner berhenti")
		}
		b.manager.OperationError(domain.RoleScanner, err)
		if !waitRetry(ctx) {
			return
		}
	}
}

func (b *Background) runESP32(ctx context.Context) {
	for waitUntilReady(ctx, b.manager, domain.RoleESP32) {
		state := b.manager.State(domain.RoleESP32)
		b.logger.Printf("[MONITOR] ESP32 %s @ %d 8N1", state.Node, b.cfg.ESP32.BaudRate)
		err := b.gateway.ListenSerial(ctx, state.Node, b.cfg.ESP32.BaudRate, func(line string) {
			b.logger.Printf("[ESP32] %s", line)
		})
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("pembacaan serial ESP32 berhenti")
		}
		b.manager.OperationError(domain.RoleESP32, err)
		if !waitRetry(ctx) {
			return
		}
	}
}

func (b *Background) runPrinter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-b.prints:
			state := b.manager.State(domain.RolePrinter)
			if !state.Ready {
				b.logger.Printf("[PRINT SKIPPED] printer tidak READY; request dibatalkan: %s", state.Err)
				continue
			}
			if err := b.gateway.PrintQR(state.Node, request.value, request.time); err != nil {
				b.manager.MarkNotReady(domain.RolePrinter, err)
				if errors.Is(err, domain.ErrPaperOut) {
					b.logger.Printf("[PRINT SKIPPED] %s; request dibatalkan", err)
				} else {
					b.logger.Printf("[PRINT FAILED] %v", err)
				}
				continue
			}
			b.logger.Printf("[PRINT OK] isi QR dikirim ke %s", state.Node)
		}
	}
}

func waitUntilReady(ctx context.Context, manager *Manager, role string) bool {
	for {
		if manager.State(role).Ready {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func waitRetry(ctx context.Context) bool {
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
