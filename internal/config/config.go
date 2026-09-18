package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"piio-lab/internal/domain"
)

const DefaultBaud = 115200

func Default() domain.Config {
	return domain.Config{
		MonitorIntervalMS: 2000,
		Printer:           domain.Selector{VendorID: "04b8", ProductID: "0e27", NameContains: []string{"epson", "tm-t82"}},
		Scanner:           domain.ScannerConfig{Selector: domain.Selector{VendorID: "23d0", ProductID: "0ce0", NameContains: []string{"honeywell", "hf600g2", "hf680"}}, Mode: "keyboard", BaudRate: DefaultBaud},
		Audio:             domain.Selector{VendorID: "0d8c", ProductID: "0014", NameContains: []string{"c-media", "usb audio", "audio adapter", "unitek"}},
		ESP32:             domain.SerialSelector{Selector: domain.Selector{VendorID: "303a", ProductID: "1001", NameContains: []string{"esp32", "espressif", "cp210", "ch340", "ch341", "usb serial", "uart", "jtag"}}, BaudRate: DefaultBaud},
	}
}

func Load(path string) (domain.Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("membaca %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("format %s tidak valid: %w", path, err)
	}
	if cfg.MonitorIntervalMS < 250 {
		cfg.MonitorIntervalMS = 2000
	}
	if cfg.Scanner.BaudRate == 0 {
		cfg.Scanner.BaudRate = DefaultBaud
	}
	if cfg.ESP32.BaudRate == 0 {
		cfg.ESP32.BaudRate = DefaultBaud
	}
	cfg.Scanner.Mode = strings.ToLower(strings.TrimSpace(cfg.Scanner.Mode))
	if cfg.Scanner.Mode != "keyboard" && cfg.Scanner.Mode != "serial" {
		return cfg, errors.New("scanner.mode harus keyboard atau serial")
	}
	return cfg, nil
}
