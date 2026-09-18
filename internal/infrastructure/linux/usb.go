package linux

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"piio-lab/internal/domain"
)

type USBRepository struct{}

func (USBRepository) Scan() ([]domain.USBDevice, error) {
	entries, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil {
		return nil, err
	}
	var devices []domain.USBDevice
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, ":") || strings.HasPrefix(name, "usb") {
			continue
		}
		base := filepath.Join("/sys/bus/usb/devices", name)
		vid := readTrimmed(filepath.Join(base, "idVendor"))
		pid := readTrimmed(filepath.Join(base, "idProduct"))
		if vid == "" || pid == "" {
			continue
		}
		real, err := filepath.EvalSymlinks(base)
		if err != nil {
			continue
		}
		d := domain.USBDevice{
			SysName: name, SysPath: real, VendorID: strings.ToLower(vid), ProductID: strings.ToLower(pid),
			Manufacturer: readTrimmed(filepath.Join(base, "manufacturer")),
			Product:      readTrimmed(filepath.Join(base, "product")),
			Serial:       readTrimmed(filepath.Join(base, "serial")), Nodes: make(map[string][]string),
		}
		d.Nodes["printer"] = append(
			classNodes(real, "/sys/class/usbmisc", "lp*", "/dev/usb"),
			classNodes(real, "/sys/class/usb", "lp*", "/dev/usb")...,
		)
		d.Nodes["tty"] = append(classNodes(real, "/sys/class/tty", "ttyUSB*", "/dev"), classNodes(real, "/sys/class/tty", "ttyACM*", "/dev")...)
		d.Nodes["tty"] = append(d.Nodes["tty"], stableAliases(d.Nodes["tty"], "/dev/serial/by-id/*")...)
		d.Nodes["input"] = classNodes(real, "/sys/class/input", "event*", "/dev/input")
		d.Nodes["input"] = append(d.Nodes["input"], stableAliases(d.Nodes["input"], "/dev/input/by-id/*-event-kbd")...)
		d.Nodes["capture"] = captureNodes(real)
		for k := range d.Nodes {
			d.Nodes[k] = uniqueSorted(d.Nodes[k])
		}
		devices = append(devices, d)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].SysName < devices[j].SysName })
	return devices, nil
}

func classNodes(usbReal, classDir, pattern, devDir string) []string {
	matches, _ := filepath.Glob(filepath.Join(classDir, pattern))
	var nodes []string
	for _, classPath := range matches {
		real, err := filepath.EvalSymlinks(filepath.Join(classPath, "device"))
		if err != nil {
			real, err = filepath.EvalSymlinks(classPath)
		}
		if err == nil && nearestUSBDevice(real) == filepath.Clean(usbReal) {
			nodes = append(nodes, filepath.Join(devDir, filepath.Base(classPath)))
		}
	}
	return nodes
}

func captureNodes(usbReal string) []string {
	matches, _ := filepath.Glob("/sys/class/sound/pcmC*D*c")
	var nodes []string
	for _, classPath := range matches {
		real, err := filepath.EvalSymlinks(filepath.Join(classPath, "device"))
		if err != nil {
			real, err = filepath.EvalSymlinks(classPath)
		}
		if err == nil && nearestUSBDevice(real) == filepath.Clean(usbReal) {
			nodes = append(nodes, "/dev/snd/"+filepath.Base(classPath))
		}
	}
	return nodes
}

func nearestUSBDevice(path string) string {
	path = filepath.Clean(path)
	for path != "/" && path != "." {
		if readTrimmed(filepath.Join(path, "idVendor")) != "" && readTrimmed(filepath.Join(path, "idProduct")) != "" {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return ""
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func uniqueSorted(in []string) []string {
	set := make(map[string]struct{})
	for _, s := range in {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func stableAliases(nodes []string, pattern string) []string {
	aliases, _ := filepath.Glob(pattern)
	var out []string
	for _, alias := range aliases {
		target, err := filepath.EvalSymlinks(alias)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			realNode, nodeErr := filepath.EvalSymlinks(node)
			if nodeErr == nil && realNode == target {
				out = append(out, alias)
				break
			}
		}
	}
	return out
}

type EventWatcher struct{ Logger domain.Logger }

func (w EventWatcher) Watch(ctx context.Context, trigger chan<- struct{}) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_DGRAM, syscall.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		w.Logger.Printf("[WARNING] Monitor event kernel tidak tersedia: %v; memakai polling", err)
		return
	}
	defer syscall.Close(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Groups: 1}); err != nil {
		w.Logger.Printf("[WARNING] Tidak dapat berlangganan event kernel: %v; memakai polling", err)
		return
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		w.Logger.Printf("[WARNING] Tidak dapat mengatur monitor kernel: %v; memakai polling", err)
		return
	}
	buf := make([]byte, 16*1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			w.Logger.Printf("[WARNING] Monitor event kernel berhenti: %v", err)
			return
		}
		fields := bytes.Split(buf[:n], []byte{0})
		usb, action := false, ""
		for _, field := range fields {
			s := string(field)
			if s == "SUBSYSTEM=usb" {
				usb = true
			}
			if strings.HasPrefix(s, "ACTION=") {
				action = strings.TrimPrefix(s, "ACTION=")
			}
		}
		if usb && (action == "add" || action == "remove" || action == "bind" || action == "unbind") {
			select {
			case trigger <- struct{}{}:
			default:
			}
		}
	}
}
