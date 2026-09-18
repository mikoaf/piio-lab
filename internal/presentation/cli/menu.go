package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"piio-lab/internal/application"
	"piio-lab/internal/domain"
	linuxio "piio-lab/internal/infrastructure/linux"
)

type CLI struct {
	cfg     domain.Config
	manager *application.Manager
	logger  domain.Logger
	console *linuxio.Console
}

func New(cfg domain.Config, manager *application.Manager, logger domain.Logger) (*CLI, error) {
	console, err := linuxio.NewConsole(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("menyiapkan input terminal: %w", err)
	}
	return &CLI{cfg: cfg, manager: manager, logger: logger, console: console}, nil
}

func (c *CLI) Run(ctx context.Context) error {
	for {
		fmt.Println("\nMenu:")
		fmt.Println("  1. Test printer")
		fmt.Println("  2. Listen QR scanner")
		fmt.Println("  3. Record microphone to WAV")
		fmt.Println("  4. Monitor ESP32 log")
		fmt.Println("  5. Show device status")
		fmt.Println("  0. Exit")
		fmt.Print("Pilih: ")
		line, err := c.console.ReadLine(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch strings.TrimSpace(line) {
		case "1":
			c.testPrinter(ctx)
		case "2":
			c.listenScanner(ctx)
		case "3":
			c.recordAudio(ctx)
		case "4":
			c.monitorESP32(ctx)
		case "5":
			c.showStatus()
		case "0", "q", "quit", "exit":
			c.logger.Println("[STOP] PiIO Lab dihentikan")
			return nil
		default:
			fmt.Println("Pilihan tidak dikenal.")
		}
	}
}

func (c *CLI) requireReady(role string) (domain.DeviceState, bool) {
	state := c.manager.State(role)
	if !state.Connected {
		fmt.Printf("%s tidak terhubung.\n", strings.ToUpper(role))
		return state, false
	}
	if !state.Ready {
		fmt.Printf("%s belum siap: %s\n", strings.ToUpper(role), state.Err)
		return state, false
	}
	return state, true
}

func (c *CLI) testPrinter(ctx context.Context) {
	state, ok := c.requireReady(domain.RolePrinter)
	if !ok {
		return
	}
	fmt.Print("Kertas thermal sudah terpasang? [y/N]: ")
	answer, err := c.console.ReadLine(ctx)
	if err != nil || !isYes(answer) {
		c.logger.Println("[TEST SKIPPED] PRINTER tes cetak dibatalkan; tidak ada data cetak yang dikirim")
		return
	}
	if err := linuxio.PrintTestReceipt(state.Node, time.Now()); err != nil {
		c.manager.OperationError(domain.RolePrinter, err)
		return
	}
	c.logger.Printf("[TEST OK] PRINTER data uji dikirim ke %s", state.Node)
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "ya":
		return true
	default:
		return false
	}
}

func (c *CLI) listenScanner(ctx context.Context) {
	state, ok := c.requireReady(domain.RoleScanner)
	if !ok {
		return
	}
	if c.cfg.Scanner.Mode == "serial" {
		c.listenSerialScanner(ctx, state)
		return
	}
	c.listenKeyboardScanner(ctx, state)
}

func (c *CLI) listenKeyboardScanner(ctx context.Context, state domain.DeviceState) {
	f, err := linuxio.OpenKeyboardExclusive(state.Node)
	if err != nil {
		fmt.Printf("Tidak dapat mengambil input scanner secara eksklusif: %v\n", err)
		fmt.Println("Pastikan user tergabung dalam grup input.")
		return
	}
	defer linuxio.CloseKeyboard(f)
	c.logger.Printf("[LISTEN] SCANNER keyboard di %s", state.Node)
	fmt.Println("Scan QR; tekan Enter pada keyboard utama untuk kembali ke menu.")
	opCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		linuxio.ReadKeyboardEvents(opCtx, f, func(value string) { c.logger.Printf("[QR] %s", value) })
		close(done)
	}()
	c.waitForEnter(ctx, done)
	cancel()
	_ = f.Close()
	<-done
}

func (c *CLI) listenSerialScanner(ctx context.Context, state domain.DeviceState) {
	f, err := linuxio.OpenSerial(state.Node, c.cfg.Scanner.BaudRate)
	if err != nil {
		c.manager.OperationError(domain.RoleScanner, err)
		return
	}
	defer f.Close()
	c.logger.Printf("[LISTEN] SCANNER serial %s @ %d", state.Node, c.cfg.Scanner.BaudRate)
	fmt.Println("Scan QR; tekan Enter untuk kembali ke menu.")
	opCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := linuxio.ReadSerialLines(opCtx, f, func(line string) {
			if value := strings.TrimSpace(line); value != "" {
				c.logger.Printf("[QR] %s", value)
			}
		})
		if err != nil && opCtx.Err() == nil {
			c.manager.OperationError(domain.RoleScanner, err)
		}
	}()
	c.waitForEnter(ctx, done)
	cancel()
	_ = f.Close()
	<-done
}

func (c *CLI) recordAudio(ctx context.Context) {
	state, ok := c.requireReady(domain.RoleAudio)
	if !ok {
		return
	}
	device, err := linuxio.ALSADeviceFromNode(state.Node)
	if err != nil {
		fmt.Println("Audio error:", err)
		return
	}
	fmt.Print("Durasi rekaman dalam detik [10]: ")
	line, _ := c.console.ReadLine(ctx)
	duration := 10
	if value := strings.TrimSpace(line); value != "" {
		duration, err = strconv.Atoi(value)
		if err != nil || duration < 1 || duration > 3600 {
			fmt.Println("Durasi harus 1 sampai 3600 detik.")
			return
		}
	}
	if err := os.MkdirAll("recordings", 0755); err != nil {
		fmt.Println("Audio error:", err)
		return
	}
	output := filepath.Join("recordings", "recording-"+time.Now().Format("20060102-150405")+".wav")
	partial := output + ".partial"
	opCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.logger.Printf("[RECORD] AUDIO %s, mono 48000 Hz 16-bit, maksimum %d detik -> %s", device, duration, output)
	fmt.Println("Tekan Enter untuk membatalkan rekaman dan menghapus audio.")
	cmd := exec.CommandContext(opCtx, "arecord", "-q", "-D", device, "-f", "S16_LE", "-r", "48000", "-c", "1", "-t", "wav", "-d", strconv.Itoa(duration), partial)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		_ = os.Remove(partial)
		c.manager.OperationError(domain.RoleAudio, err)
		return
	}
	commandDone := make(chan error, 1)
	go func() { commandDone <- cmd.Wait() }()
	enterDone := make(chan error, 1)
	go func() {
		_, err := c.console.ReadLine(opCtx)
		enterDone <- err
	}()

	cancelled := false
	var commandErr error
	select {
	case commandErr = <-commandDone:
		cancel()
		<-enterDone
	case <-enterDone:
		cancelled = true
		cancel()
		commandErr = <-commandDone
	case <-ctx.Done():
		cancelled = true
		cancel()
		commandErr = <-commandDone
	}
	if cancelled {
		if err := os.Remove(partial); err != nil && !errors.Is(err, os.ErrNotExist) {
			c.logger.Printf("[WARNING] AUDIO gagal menghapus file parsial %s: %v", partial, err)
		}
		c.logger.Println("[CANCELLED] AUDIO rekaman dibatalkan; tidak ada file audio yang disimpan")
		return
	}
	if commandErr != nil {
		_ = os.Remove(partial)
		c.manager.OperationError(domain.RoleAudio, commandErr)
		return
	}
	if err := os.Rename(partial, output); err != nil {
		_ = os.Remove(partial)
		c.manager.OperationError(domain.RoleAudio, fmt.Errorf("menyimpan hasil rekaman: %w", err))
		return
	}
	c.logger.Printf("[TEST OK] AUDIO rekaman tersimpan: %s", output)
}

func (c *CLI) monitorESP32(ctx context.Context) {
	state, ok := c.requireReady(domain.RoleESP32)
	if !ok {
		return
	}
	f, err := linuxio.OpenSerial(state.Node, c.cfg.ESP32.BaudRate)
	if err != nil {
		c.manager.OperationError(domain.RoleESP32, err)
		return
	}
	defer f.Close()
	c.logger.Printf("[MONITOR] ESP32 %s @ %d 8N1", state.Node, c.cfg.ESP32.BaudRate)
	fmt.Println("Tekan Enter untuk kembali ke menu.")
	opCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := linuxio.ReadSerialLines(opCtx, f, func(line string) { c.logger.Printf("[ESP32] %s", line) })
		if err != nil && opCtx.Err() == nil {
			c.manager.OperationError(domain.RoleESP32, err)
		}
	}()
	c.waitForEnter(ctx, done)
	cancel()
	_ = f.Close()
	<-done
}

func (c *CLI) waitForEnter(ctx context.Context, done <-chan struct{}) {
	go func() {
		select {
		case <-done:
			fmt.Println("Pembacaan perangkat berhenti; tekan Enter untuk kembali ke menu.")
		case <-ctx.Done():
		}
	}()
	_, _ = c.console.ReadLine(ctx)
}

func (c *CLI) showStatus() {
	fmt.Println("\nDevice status:")
	for _, state := range c.manager.States() {
		status := "DISCONNECTED"
		if state.Connected {
			status = "CONNECTED"
		}
		if state.Ready {
			status = "READY"
		}
		fmt.Printf("  %-8s %-12s", strings.ToUpper(state.Role), status)
		if state.Connected {
			fmt.Printf(" %s | port %s | %s", state.Device.DisplayName(), state.Device.SysName, state.Node)
		}
		if state.Err != "" {
			fmt.Printf(" | %s", state.Err)
		}
		fmt.Println()
	}
}
