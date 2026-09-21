package linux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"

	"piio-lab/internal/domain"
)

type Initializer struct {
	Config  domain.Config
	Printer *Printer
}

func (i Initializer) Initialize(s domain.DeviceState) domain.DeviceState {
	if !s.Connected {
		s.Ready = false
		return s
	}
	var err error
	switch s.Role {
	case domain.RolePrinter:
		s.Node, err = firstNode(s.Device.Nodes["printer"])
		if err != nil {
			err = errors.New("node printer belum tersedia; cek modul usblp dan /dev/usb/lp*")
		} else {
			if i.Printer == nil {
				err = errors.New("adapter printer belum dikonfigurasi")
			} else {
				err = i.Printer.Initialize(s.Node)
			}
		}
	case domain.RoleScanner:
		kind := "input"
		if i.Config.Scanner.Mode == "serial" {
			kind = "tty"
		}
		s.Node, err = firstNode(s.Device.Nodes[kind])
		if err == nil {
			err = ProbeReadable(s.Node)
		}
		s.Detail = fmt.Sprintf("%s (%s)", s.Device.DisplayName(), i.Config.Scanner.Mode)
	case domain.RoleAudio:
		s.Node, err = firstNode(s.Device.Nodes["capture"])
		if err == nil {
			if _, lookupErr := exec.LookPath("arecord"); lookupErr != nil {
				err = errors.New("arecord tidak terpasang; instal paket alsa-utils")
			}
		}
	case domain.RoleESP32:
		s.Node, err = firstNode(s.Device.Nodes["tty"])
		if err == nil {
			var f *os.File
			f, err = OpenSerial(s.Node, i.Config.ESP32.BaudRate)
			if f != nil {
				_ = f.Close()
			}
		}
	}
	if err != nil {
		s.Ready = false
		s.Err = err.Error()
		return s
	}
	s.Ready = true
	s.Err = ""
	return s
}

func (i Initializer) Check(s domain.DeviceState) domain.DeviceState {
	if !s.Connected || s.Role != domain.RolePrinter || i.Printer == nil {
		return s
	}
	if err := i.Printer.Check(s.Node); err != nil {
		s.Ready = false
		s.Err = err.Error()
		return s
	}
	s.Ready = true
	s.Err = ""
	return s
}

func firstNode(nodes []string) (string, error) {
	if len(nodes) == 0 {
		return "", errors.New("node Linux belum tersedia")
	}
	sort.Strings(nodes)
	return nodes[0], nil
}

func ProbeReadable(path string) error {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("tidak dapat membuka %s: %w", path, err)
	}
	return f.Close()
}

func ALSADeviceFromNode(node string) (string, error) {
	base := filepath.Base(node)
	var card, device int
	if _, err := fmt.Sscanf(base, "pcmC%dD%dc", &card, &device); err != nil {
		return "", fmt.Errorf("node capture ALSA tidak dikenal: %s", node)
	}
	return fmt.Sprintf("plughw:%d,%d", card, device), nil
}
