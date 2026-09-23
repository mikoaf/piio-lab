package nusapala

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
)

type USBConfig struct {
	Enabled       bool   `json:"enabled"`
	VendorID      string `json:"vendor_id"`
	ProductID     string `json:"product_id"`
	Configuration int    `json:"configuration"`
	Interface     int    `json:"interface"`
	Alternate     int    `json:"alternate"`
	Endpoint      int    `json:"endpoint"`
}
type AudioConfig struct {
	Enabled      bool    `json:"enabled"`
	InputDevice  int     `json:"input_device"`
	OutputDevice int     `json:"output_device"`
	SampleRate   int     `json:"sample_rate"`
	FrameSize    int     `json:"frame_size"`
	Channels     int     `json:"channels"`
	InputGain    float32 `json:"input_gain"`
	OutputGain   float32 `json:"output_gain"`
}
type Config struct {
	Listen    string      `json:"listen"`
	TLSCert   string      `json:"tls_cert"`
	TLSKey    string      `json:"tls_key"`
	Audio     AudioConfig `json:"audio"`
	Printer   USBConfig   `json:"printer"`
	Scanner   USBConfig   `json:"scanner"`
	AutoPrint bool        `json:"auto_print_qr"`
	STI       struct {
		Enabled bool   `json:"enabled"`
		Port    string `json:"port"`
		KeyEnv  string `json:"key_env"`
		PollMS  int    `json:"poll_ms"`
	} `json:"sti"`
}

func DefaultConfig() Config {
	c := Config{Listen: "127.0.0.1:8080", AutoPrint: true,
		Audio:   AudioConfig{Enabled: true, InputDevice: -1, OutputDevice: -1, SampleRate: 48000, FrameSize: 480, Channels: 1, InputGain: 1, OutputGain: 1},
		Printer: USBConfig{Enabled: true, VendorID: "04b8", ProductID: "0e27", Configuration: 1, Endpoint: 1},
		Scanner: USBConfig{Enabled: true, VendorID: "23d0", ProductID: "0ce0", Configuration: 1, Endpoint: 1}}
	c.STI.Port = "/dev/serial/by-id/REPLACE_WITH_STI"
	c.STI.KeyEnv = "PIIO_STI_KEY"
	c.STI.PollMS = 500
	return c
}
func LoadConfig(path string) (Config, error) {
	c := DefaultConfig()
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return fmt.Errorf("tls_cert dan tls_key harus diisi bersama")
	}
	a := c.Audio
	if a.Channels != 1 && a.Channels != 2 {
		return fmt.Errorf("audio.channels harus 1 atau 2")
	}
	if a.SampleRate != 48000 {
		return fmt.Errorf("audio.sample_rate harus 48000 untuk mode WebRTC")
	}
	switch a.FrameSize {
	case 120, 240, 480, 960, 1920, 2880:
	default:
		return fmt.Errorf("audio.frame_size harus 120/240/480/960/1920/2880")
	}
	if a.InputDevice < -1 || a.OutputDevice < -1 || a.InputGain < 0 || a.InputGain > 8 || a.OutputGain < 0 || a.OutputGain > 8 {
		return fmt.Errorf("pilihan device/gain audio tidak valid")
	}
	for name, u := range map[string]USBConfig{"printer": c.Printer, "scanner": c.Scanner} {
		if !u.Enabled {
			continue
		}
		if _, _, err := u.IDs(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if u.Configuration < 1 || u.Interface < 0 || u.Alternate < 0 || u.Endpoint < 1 || u.Endpoint > 15 {
			return fmt.Errorf("%s: konfigurasi/interface/endpoint tidak valid", name)
		}
	}
	if c.Printer.Enabled && c.Printer.Endpoint != 1 {
		return fmt.Errorf("go-escpos v0.1.0 memakai printer endpoint 1")
	}
	if c.STI.Enabled && (c.STI.Port == "" || c.STI.KeyEnv == "" || c.STI.PollMS < 100) {
		return fmt.Errorf("sti membutuhkan port, key_env dan poll_ms >= 100")
	}
	return nil
}
func (u USBConfig) IDs() (uint16, uint16, error) {
	v, e := strconv.ParseUint(u.VendorID, 16, 16)
	if e != nil {
		return 0, 0, fmt.Errorf("vendor_id harus hex: %w", e)
	}
	p, e := strconv.ParseUint(u.ProductID, 16, 16)
	if e != nil {
		return 0, 0, fmt.Errorf("product_id harus hex: %w", e)
	}
	return uint16(v), uint16(p), nil
}
func (c Config) STIKey() (string, error) {
	k := os.Getenv(c.STI.KeyEnv)
	b, e := hex.DecodeString(k)
	if e != nil || len(b) != 16 {
		return "", fmt.Errorf("environment %s harus berisi kunci STI 32 karakter hex", c.STI.KeyEnv)
	}
	return k, nil
}
