// PiIO Lab is a Raspberry Pi peripheral diagnostic utility.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"piio-lab/internal/application"
	"piio-lab/internal/config"
	linuxio "piio-lab/internal/infrastructure/linux"
	"piio-lab/internal/presentation/cli"
)

const (
	appName    = "PiIO Lab"
	configPath = "config.json"
	logDir     = "logs"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("%s hanya mendukung Linux/Raspberry Pi OS", appName)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	logger, closeLog, err := newLogger()
	if err != nil {
		return err
	}
	defer closeLog()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Printf("%s - Raspberry Pi Peripheral Tester\n\n", appName)

	repository := linuxio.USBRepository{}
	initializer := linuxio.Initializer{Config: cfg}
	events := linuxio.EventWatcher{Logger: logger}
	manager := application.NewManager(cfg, logger, repository, initializer, events)
	logger.Println("[START] PiIO Lab dimulai")
	manager.InitializeAll()

	interfaceCLI, err := cli.New(cfg, manager, logger)
	if err != nil {
		return err
	}
	go manager.Monitor(ctx)
	return interfaceCLI.Run(ctx)
}

func newLogger() (*log.Logger, func(), error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("membuat direktori log: %w", err)
	}
	name := filepath.Join(logDir, "piio-"+time.Now().Format("20060102")+".log")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("membuka log: %w", err)
	}
	logger := log.New(io.MultiWriter(os.Stdout, f), "", log.Ldate|log.Ltime|log.Lmicroseconds)
	return logger, func() { _ = f.Close() }, nil
}
