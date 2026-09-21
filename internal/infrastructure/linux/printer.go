package linux

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"piio-lab/internal/domain"
)

const printerStatusTimeout = 750 * time.Millisecond

// Printer serializes real-time status requests and print data. ESC/POS requires
// the response to DLE EOT to be received before more data is sent.
type Printer struct {
	mu sync.Mutex
}

func (p *Printer) Initialize(path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	f, err := openPrinter(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte{0x1b, 0x40}); err != nil {
		return fmt.Errorf("inisialisasi ESC/POS gagal: %w", err)
	}
	return requirePaper(f)
}

func (p *Printer) Check(path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	f, err := openPrinter(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return requirePaper(f)
}

func (p *Printer) PrintQR(path, value string, now time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	f, err := openPrinter(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := requirePaper(f); err != nil {
		return err
	}

	value = strings.TrimSpace(value)
	data := []byte{0x1b, 0x40, 0x1b, 0x61, 0x01, 0x1b, 0x45, 0x01}
	data = append(data, []byte("PiIO Lab\n")...)
	data = append(data, 0x1b, 0x45, 0x00)
	data = append(data, []byte("QR Scanner\n"+now.Format("2006-01-02 15:04:05")+"\n\n")...)
	data = append(data, 0x1b, 0x61, 0x00)
	data = append(data, []byte(value+"\n\n\n")...)
	data = append(data, 0x1d, 0x56, 0x00)
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("mengirim data cetak: %w", err)
	}
	return nil
}

func openPrinter(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("tidak dapat membuka %s: %w", path, err)
	}
	return f, nil
}

func requirePaper(f *os.File) error {
	status, err := queryPaperStatus(f)
	if err != nil {
		return err
	}
	if !PaperPresent(status) {
		return domain.ErrPaperOut
	}
	return nil
}

func queryPaperStatus(f *os.File) (byte, error) {
	if _, err := f.Write([]byte{0x10, 0x04, 0x04}); err != nil {
		return 0, fmt.Errorf("meminta status sensor kertas: %w", err)
	}
	deadline := time.Now().Add(printerStatusTimeout)
	var response [1]byte
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining > 50*time.Millisecond {
			remaining = 50 * time.Millisecond
		}
		ready, err := DescriptorReadable(int(f.Fd()), remaining)
		if err != nil {
			return 0, fmt.Errorf("menunggu status sensor kertas: %w", err)
		}
		if !ready {
			continue
		}
		n, err := syscall.Read(int(f.Fd()), response[:])
		if n == 1 {
			return response[0], nil
		}
		if err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) {
			return 0, fmt.Errorf("membaca status sensor kertas: %w", err)
		}
	}
	return 0, errors.New("printer tidak merespons pemeriksaan sensor kertas")
}

// PaperPresent interprets the roll-paper status returned by DLE EOT n=4.
func PaperPresent(status byte) bool {
	return status&0x60 != 0x60
}
