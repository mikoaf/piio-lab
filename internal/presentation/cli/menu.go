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
	manager *application.Manager
	logger  domain.Logger
	console *linuxio.Console
}

func New(manager *application.Manager, logger domain.Logger) (*CLI, error) {
	console, err := linuxio.NewConsole(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("menyiapkan input terminal: %w", err)
	}
	return &CLI{manager: manager, logger: logger, console: console}, nil
}

func (c *CLI) Run(ctx context.Context) error {
	for {
		fmt.Println("\nMenu:")
		fmt.Println("  1. Record Microphone to WAV")
		fmt.Println("  2. Show Device Status")
		fmt.Println("  3. Exit")
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
			c.recordAudio(ctx)
		case "2":
			c.showStatus()
		case "3", "0", "q", "quit", "exit":
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
	c.logger.Printf("[RECORD OK] AUDIO rekaman tersimpan: %s", output)
}

func (c *CLI) showStatus() {
	fmt.Println("\nDevice status:")
	for _, state := range c.manager.States() {
		status := "DISCONNECTED"
		if state.Connected {
			status = "NOT READY"
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
