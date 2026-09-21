package linux

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Gateway struct {
	Printer *Printer
}

func (g Gateway) ListenKeyboard(ctx context.Context, node string, emit func(string)) error {
	f, err := OpenKeyboardExclusive(node)
	if err != nil {
		return fmt.Errorf("membuka scanner keyboard %s: %w", node, err)
	}
	defer CloseKeyboard(f)
	return ReadKeyboardEvents(ctx, f, emit)
}

func (g Gateway) ListenSerial(ctx context.Context, node string, baudRate int, emit func(string)) error {
	f, err := OpenSerial(node, baudRate)
	if err != nil {
		return err
	}
	defer f.Close()
	return ReadSerialLines(ctx, f, func(line string) {
		if value := strings.TrimSpace(line); value != "" {
			emit(value)
		}
	})
}

func (g Gateway) PrintQR(node, value string, now time.Time) error {
	if g.Printer == nil {
		return fmt.Errorf("adapter printer belum dikonfigurasi")
	}
	return g.Printer.PrintQR(node, value, now)
}
