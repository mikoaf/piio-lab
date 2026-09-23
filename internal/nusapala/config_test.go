package nusapala

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigurationRejectsUnusableAudioAndUSB(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{"sample rate", func(c *Config) { c.Audio.SampleRate = 44100 }},
		{"frame size", func(c *Config) { c.Audio.FrameSize = 1024 }},
		{"channels", func(c *Config) { c.Audio.Channels = 3 }},
		{"device", func(c *Config) { c.Audio.InputDevice = -2 }},
		{"endpoint", func(c *Config) { c.Printer.Endpoint = 2 }},
		{"vid", func(c *Config) { c.Scanner.VendorID = "xyz" }},
		{"tls pair", func(c *Config) { c.TLSCert = "cert.pem" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			tt.change(&c)
			if c.Validate() == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestConfigRejectsTypoAndMissingSTIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"audio":{"sample_rates":48000}}`), 0600)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("unknown field accepted")
	}
	c := DefaultConfig()
	c.STI.KeyEnv = "PIIO_TEST_KEY"
	t.Setenv(c.STI.KeyEnv, "")
	if _, err := c.STIKey(); err == nil {
		t.Fatal("empty key accepted")
	}
	t.Setenv(c.STI.KeyEnv, "0123456789abcdef0123456789abcdef")
	if _, err := c.STIKey(); err != nil {
		t.Fatal(err)
	}
}
