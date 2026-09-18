package linux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"piio-lab/internal/domain"
)

type Initializer struct{ Config domain.Config }

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
			err = InitializePrinter(s.Node)
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

func InitializePrinter(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("tidak dapat membuka %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write([]byte{0x1b, 0x40}); err != nil {
		return fmt.Errorf("inisialisasi ESC/POS gagal: %w", err)
	}
	return nil
}

func PrintTestReceipt(path string, now time.Time) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	data := []byte{0x1b, 0x40, 0x1b, 0x61, 0x01, 0x1b, 0x45, 0x01}
	data = append(data, []byte("PiIO Lab\n")...)
	data = append(data, 0x1b, 0x45, 0x00)
	data = append(data, []byte("EPSON TM-T82X Test\n"+now.Format("2006-01-02 15:04:05")+"\n\n")...)
	data = append(data, 0x1b, 0x61, 0x00)
	data = append(data, []byte("Printer USB berfungsi.\n\n\n")...)
	data = append(data, 0x1d, 0x56, 0x00)
	_, err = f.Write(data)
	return err
}

func ALSADeviceFromNode(node string) (string, error) {
	base := filepath.Base(node)
	var card, device int
	if _, err := fmt.Sscanf(base, "pcmC%dD%dc", &card, &device); err != nil {
		return "", fmt.Errorf("node capture ALSA tidak dikenal: %s", node)
	}
	return fmt.Sprintf("plughw:%d,%d", card, device), nil
}
