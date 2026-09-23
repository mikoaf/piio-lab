//go:build nusapala && linux && cgo

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"piio-lab/internal/nusapala"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "config.nusapala.json", "file konfigurasi")
	list := flag.Bool("list-audio", false, "daftar device PortAudio dan keluar")
	flag.Parse()
	if *list {
		return nusapala.ListAudio(os.Stdout)
	}
	cfg, err := nusapala.LoadConfig(*path)
	if err != nil {
		return err
	}
	if cfg.STI.Enabled {
		if _, err := cfg.STIKey(); err != nil {
			return err
		}
	}
	if err = os.MkdirAll("logs", 0755); err != nil {
		return err
	}
	f, err := os.OpenFile("logs/nusapala-"+time.Now().Format("20060102")+".log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	logger := log.New(io.MultiWriter(os.Stdout, f), "", log.Ldate|log.Ltime|log.Lmicroseconds)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return nusapala.Run(ctx, cfg, logger)
}
