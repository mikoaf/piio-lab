//go:build nusapala && linux && cgo

package nusapala

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	linuxio "piio-lab/internal/infrastructure/linux"

	escpos "github.com/ABA-Developer/go-escpos"
	logger "github.com/ABA-Developer/go-logger"
	paymentreader "github.com/ABA-Developer/go-paymentreader"
	"github.com/google/gousb"
)

type Peripherals struct {
	cfg       Config
	status    *Status
	prints    chan string
	printerMu sync.Mutex
}

func NewPeripherals(c Config, s *Status) *Peripherals {
	return &Peripherals{cfg: c, status: s, prints: make(chan string, 32)}
}
func (p *Peripherals) Start(ctx context.Context) {
	if p.cfg.Printer.Enabled {
		p.status.Set("printer", "IDLE", "gousb + go-escpos; menunggu request cetak")
		go p.printerLoop(ctx)
	} else {
		p.status.Set("printer", "DISABLED", "")
	}
	if p.cfg.Scanner.Enabled {
		go p.scannerLoop(ctx)
	} else {
		p.status.Set("scanner", "DISABLED", "")
	}
	if p.cfg.STI.Enabled {
		go p.stiLoop(ctx)
	} else {
		p.status.Set("sti", "DISABLED", "isi port dan PIIO_STI_KEY lalu enabled=true")
	}
	go p.inventory(ctx)
}
func (p *Peripherals) QueuePrint(value string) error {
	if !p.cfg.Printer.Enabled {
		return fmt.Errorf("printer dinonaktifkan")
	}
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 4096 {
		return fmt.Errorf("isi cetak harus 1–4096 byte")
	}
	select {
	case p.prints <- value:
		return nil
	default:
		return fmt.Errorf("antrean printer penuh")
	}
}
func (p *Peripherals) printerLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	p.checkPrinter()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkPrinter()
		case value := <-p.prints:
			p.status.Set("printer", "PRINTING", "go-escpos Done()")
			err := p.print(value)
			p.status.Result("printer", err, "data cetak terkirim")
		}
	}
}
func (p *Peripherals) checkPrinter() {
	p.printerMu.Lock()
	defer p.printerMu.Unlock()
	u := gousb.NewContext()
	defer u.Close()
	v, id, _ := p.cfg.Printer.IDs()
	dev, err := u.OpenDeviceWithVIDPID(gousb.ID(v), gousb.ID(id))
	if dev != nil {
		defer dev.Close()
	}
	if err != nil {
		p.status.Set("printer_usb", "ERROR", err.Error())
		return
	}
	if dev == nil {
		p.status.Set("printer_usb", "DISCONNECTED", "USB device tidak ditemukan")
		return
	}
	p.status.Set("printer_usb", "CONNECTED", "VID/PID dapat dibuka; bukan pemeriksaan kertas")
}
func (p *Peripherals) print(value string) error {
	p.printerMu.Lock()
	defer p.printerMu.Unlock()
	c := p.cfg.Printer
	v, id, _ := c.IDs()
	w := escpos.NewWriter(gousb.ID(v), gousb.ID(id), c.Configuration, c.Interface, c.Alternate)
	w.SetAlign("center")
	w.SetBold(true)
	if err := w.Textln("PIIO NUSAPALA LAB"); err != nil {
		return err
	}
	w.SetBold(false)
	if err := w.Textln(time.Now().Format(time.RFC3339)); err != nil {
		return err
	}
	if err := w.Textln(value); err != nil {
		return err
	}
	w.Qr(value, 5)
	w.Print(4)
	w.Cut()
	return w.Done()
}
func (p *Peripherals) scannerLoop(ctx context.Context) {
	for ctx.Err() == nil {
		err := p.readScanner(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if fallbackErr := p.readScannerEventFallback(ctx); fallbackErr == nil {
				continue
			} else {
				p.status.Result("scanner", fmt.Errorf("raw usb: %w; fallback: %v", err, fallbackErr), "")
			}
		}
		p.status.Result("scanner", err, "")
		if !pause(ctx, 2*time.Second) {
			return
		}
	}
}

func (p *Peripherals) readScannerEventFallback(ctx context.Context) error {
	path, err := findScannerEventPath()
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	p.status.Set("scanner", "READY", fmt.Sprintf("fallback %s (keyboard input)", path))
	decoder := inputEventDecoder{pressed: map[uint16]bool{}}
	buf := make([]byte, 24)
	for ctx.Err() == nil {
		if _, err := io.ReadFull(f, buf); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		ev, ok := decodeInputEvent(buf)
		if !ok || ev.Type != EV_KEY {
			continue
		}
		if s := decoder.Feed(ev, time.Now()); s != "" {
			p.onQR(s)
		}
		if s := decoder.Flush(time.Now()); s != "" {
			p.onQR(s)
		}
	}
	return nil
}

func findScannerEventPath() (string, error) {
	if entries, err := os.ReadDir("/dev/input/by-id"); err == nil {
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if strings.Contains(name, "honeywell") || strings.Contains(name, "hf600") || strings.Contains(name, "hf680") || strings.Contains(name, "event-kbd") {
				return "/dev/input/by-id/" + e.Name(), nil
			}
		}
	}
	if entries, err := os.ReadDir("/dev/input"); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "event") {
				return "/dev/input/" + e.Name(), nil
			}
		}
	}
	return "", fmt.Errorf("scanner keyboard fallback tidak tersedia di /dev/input")
}
func (p *Peripherals) readScanner(ctx context.Context) error {
	c := p.cfg.Scanner
	v, id, _ := c.IDs()
	u := gousb.NewContext()
	defer u.Close()
	dev, err := u.OpenDeviceWithVIDPID(gousb.ID(v), gousb.ID(id))
	if dev != nil {
		defer dev.Close()
	}
	if err != nil {
		return err
	}
	if dev == nil {
		return fmt.Errorf("QR USB %s:%s tidak ditemukan", c.VendorID, c.ProductID)
	}
	if err = dev.SetAutoDetach(true); err != nil {
		return err
	}
	cfg, err := dev.Config(c.Configuration)
	if err != nil {
		return err
	}
	defer cfg.Close()
	intf, err := cfg.Interface(c.Interface, c.Alternate)
	if err != nil {
		return err
	}
	defer intf.Close()
	ep, err := intf.InEndpoint(c.Endpoint)
	if err != nil {
		return err
	}
	p.status.Set("scanner", "READY", fmt.Sprintf("gousb %s:%s interface=%d endpoint=%d", c.VendorID, c.ProductID, c.Interface, c.Endpoint))
	return p.readScannerReports(ctx, ep.ReadContext, ep.Desc.MaxPacketSize)
}

func (p *Peripherals) readScannerReports(ctx context.Context, read func(context.Context, []byte) (int, error), packetSize int) error {
	decoder := hidDecoder{}
	buf := make([]byte, packetSize)
	for ctx.Err() == nil {
		readCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		n, e := read(readCtx, buf)
		expired := errors.Is(readCtx.Err(), context.DeadlineExceeded)
		cancel()
		if e != nil && !scannerReadIdle(e, expired) {
			return e
		}
		if n > 0 {
			text, decodeErr := decoder.Feed(buf[:n], time.Now())
			if decodeErr != nil {
				p.status.Result("scanner", decodeErr, "")
			}
			p.onQR(text)
		}
		p.onQR(decoder.Flush(time.Now()))
	}
	return ctx.Err()
}

func scannerReadIdle(err error, expired bool) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, gousb.ErrorTimeout) || errors.Is(err, gousb.TransferTimedOut) {
		return true
	}
	return expired && errors.Is(err, gousb.TransferCancelled)
}
func (p *Peripherals) onQR(value string) {
	if value == "" {
		return
	}
	p.status.Result("scanner", nil, fmt.Sprintf("QR: %q", value))
	if p.cfg.AutoPrint {
		if err := p.QueuePrint(value); err != nil {
			p.status.Event("printer", "PRINT SKIPPED: "+err.Error())
		}
	}
}
func (p *Peripherals) stiLoop(ctx context.Context) {
	key, err := p.cfg.STIKey()
	if err != nil {
		p.status.Result("sti", err, "")
		return
	}
	p.status.Set("sti", "STARTING", p.cfg.STI.Port+" @ 38400 8N1")
	failed := make(chan error, 1)
	// v0.4.4 has no Close method. Keep one Reader for this process lifetime;
	// never reopen after failure and leak another serial reader goroutine.
	reader, err := paymentreader.NewReader(p.cfg.STI.Port, func(e error) {
		select {
		case failed <- e:
		default:
		}
		time.Sleep(100 * time.Millisecond)
	})
	if err != nil {
		p.status.Result("sti", err, "")
		return
	}
	l := logger.NewSync("STI", false)
	reader.SetLogger(l)
	reader.SetWaitTimeout(1)
	if err = reader.Initialize(key); err != nil {
		p.status.Result("sti", fmt.Errorf("initialize: %w; restart program setelah diperbaiki", err), "")
		return
	}
	p.status.Set("sti", "READY", "reader initialized; polling CheckBalance")
	for ctx.Err() == nil {
		select {
		case e := <-failed:
			p.status.Result("sti", fmt.Errorf("serial: %w; restart program untuk membuka ulang port", e), "")
			return
		default:
		}
		card, err := reader.CheckBalance()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			p.status.Result("sti", err, "")
		} else {
			p.status.Result("sti", nil, fmt.Sprintf("type=%s card=%s balance=%d", card.Type.String(), maskCard(card.Number), card.Balance))
		}
		if !pause(ctx, time.Duration(p.cfg.STI.PollMS)*time.Millisecond) {
			return
		}
	}
}
func maskCard(number string) string {
	if len(number) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(number)-4) + number[len(number)-4:]
}
func (p *Peripherals) inventory(ctx context.Context) {
	repo := linuxio.USBRepository{}
	previous := map[string]string{}
	for ctx.Err() == nil {
		devices, err := repo.Scan()
		if err != nil {
			p.status.Set("usb", "ERROR", err.Error())
		} else {
			current := map[string]string{}
			for _, d := range devices {
				key := d.SysName
				description := fmt.Sprintf("%s %s:%s %s nodes=%v", key, d.VendorID, d.ProductID, d.DisplayName(), d.Nodes)
				current[key] = description
				if previous[key] != description {
					p.status.Event("usb", "PRESENT "+description)
				}
			}
			for key, description := range previous {
				if _, ok := current[key]; !ok {
					p.status.Event("usb", "REMOVED "+description)
				}
			}
			previous = current
		}
		if !pause(ctx, 2*time.Second) {
			return
		}
	}
}
