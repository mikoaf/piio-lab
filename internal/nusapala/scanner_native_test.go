//go:build nusapala && linux && cgo

package nusapala

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	"github.com/google/gousb"
)

func TestScannerReadReportsTreatsExpiredCancelAsIdle(t *testing.T) {
	status := NewStatus(log.New(io.Discard, "", 0))
	p := &Peripherals{cfg: Config{}, status: status, prints: make(chan string, 1)}
	calls := 0
	err := p.readScannerReports(context.Background(), func(ctx context.Context, buf []byte) (int, error) {
		calls++
		switch calls {
		case 1:
			<-ctx.Done()
			return 0, gousb.TransferCancelled
		case 2:
			copy(buf, []byte{0, 0, 4, 0, 0, 0, 0, 0})
			return 8, nil
		case 3:
			copy(buf, []byte{0, 0, 40, 0, 0, 0, 0, 0})
			return 8, nil
		default:
			return 0, gousb.TransferNoDevice
		}
	}, 8)
	if !errors.Is(err, gousb.TransferNoDevice) {
		t.Fatalf("err=%v", err)
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
	got := status.Devices["scanner"].Successes
	if got != 1 {
		t.Fatalf("scanner successes=%d", got)
	}
}

func TestScannerReadIdleClassification(t *testing.T) {
	if !scannerReadIdle(gousb.TransferCancelled, true) {
		t.Fatal("expired cancellation should be idle")
	}
	if scannerReadIdle(gousb.TransferCancelled, false) {
		t.Fatal("non-expired cancellation should stay visible")
	}
	if !scannerReadIdle(gousb.TransferTimedOut, false) {
		t.Fatal("transfer timeout should be idle")
	}
}
