package domain

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	RolePrinter = "printer"
	RoleScanner = "scanner"
	RoleAudio   = "audio"
	RoleESP32   = "esp32"
)

var Roles = []string{RolePrinter, RoleScanner, RoleAudio, RoleESP32}

var ErrPaperOut = errors.New("kertas thermal habis atau tidak terpasang")

type Selector struct {
	VendorID     string   `json:"vendor_id"`
	ProductID    string   `json:"product_id"`
	NameContains []string `json:"name_contains"`
}

type SerialSelector struct {
	Selector
	BaudRate int `json:"baud_rate"`
}

type ScannerConfig struct {
	Selector
	Mode     string `json:"mode"`
	BaudRate int    `json:"baud_rate"`
}

type Config struct {
	MonitorIntervalMS int            `json:"monitor_interval_ms"`
	Printer           Selector       `json:"printer"`
	Scanner           ScannerConfig  `json:"scanner"`
	Audio             Selector       `json:"audio"`
	ESP32             SerialSelector `json:"esp32"`
}

type USBDevice struct {
	SysName      string
	SysPath      string
	VendorID     string
	ProductID    string
	Manufacturer string
	Product      string
	Serial       string
	Nodes        map[string][]string
}

func (d USBDevice) Identity() string {
	if d.Serial != "" {
		return strings.ToLower(d.VendorID + ":" + d.ProductID + ":" + d.Serial)
	}
	return strings.ToLower(d.VendorID + ":" + d.ProductID + ":" + d.Product)
}

func (d USBDevice) DisplayName() string {
	name := strings.TrimSpace(strings.TrimSpace(d.Manufacturer + " " + d.Product))
	if name == "" {
		name = "USB " + d.VendorID + ":" + d.ProductID
	}
	return name
}

func (d USBDevice) AllText() string {
	return strings.ToLower(strings.Join([]string{d.Manufacturer, d.Product, d.Serial, d.VendorID + ":" + d.ProductID}, " "))
}

type DeviceState struct {
	Role      string
	Connected bool
	Ready     bool
	Device    USBDevice
	Node      string
	Detail    string
	Err       string
}

type Logger interface {
	Printf(format string, args ...any)
	Println(args ...any)
}

type USBRepository interface {
	Scan() ([]USBDevice, error)
}

type DeviceInitializer interface {
	Initialize(DeviceState) DeviceState
	Check(DeviceState) DeviceState
}

type PeripheralGateway interface {
	ListenKeyboard(ctx context.Context, node string, emit func(string)) error
	ListenSerial(ctx context.Context, node string, baudRate int, emit func(string)) error
	PrintQR(node, value string, now time.Time) error
}

type USBEventWatcher interface {
	Watch(ctx context.Context, trigger chan<- struct{})
}
