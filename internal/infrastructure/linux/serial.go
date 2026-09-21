package linux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func OpenSerial(path string, baud int) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("membuka serial %s: %w", path, err)
	}
	var term syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&term)), 0, 0, 0)
	if errno != 0 {
		f.Close()
		return nil, fmt.Errorf("membaca konfigurasi serial: %w", errno)
	}
	speed, ok := baudConstant(baud)
	if !ok {
		f.Close()
		return nil, fmt.Errorf("baud rate %d belum didukung", baud)
	}
	term.Iflag = syscall.IGNPAR
	term.Oflag = 0
	term.Cflag = uint32(speed) | syscall.CS8 | syscall.CLOCAL | syscall.CREAD
	term.Lflag = 0
	term.Cc[syscall.VMIN] = 0
	term.Cc[syscall.VTIME] = 0
	_, _, errno = syscall.Syscall6(syscall.SYS_IOCTL, f.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&term)), 0, 0, 0)
	if errno != 0 {
		f.Close()
		return nil, fmt.Errorf("mengatur serial %d 8N1: %w", baud, errno)
	}
	return f, nil
}

func ReadSerialLines(ctx context.Context, f *os.File, emit func(string)) error {
	buf := make([]byte, 4096)
	pending := make([]byte, 0, 4096)
	for {
		if ctx.Err() != nil {
			return nil
		}
		ready, err := DescriptorReadable(int(f.Fd()), 25*time.Millisecond)
		if err != nil {
			return err
		}
		if !ready {
			continue
		}
		n, err := syscall.Read(int(f.Fd()), buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			for {
				i := bytes.IndexByte(pending, '\n')
				if i < 0 {
					break
				}
				line := strings.TrimSuffix(string(pending[:i]), "\r")
				pending = append(pending[:0], pending[i+1:]...)
				emit(line)
			}
			if len(pending) > 1024*1024 {
				return errors.New("baris serial melebihi batas 1 MiB")
			}
			continue
		}
		if err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		if n == 0 && err == nil {
			return io.EOF
		}
	}
}

func baudConstant(baud int) (uint32, bool) {
	switch baud {
	case 9600:
		return syscall.B9600, true
	case 19200:
		return syscall.B19200, true
	case 38400:
		return syscall.B38400, true
	case 57600:
		return syscall.B57600, true
	case 115200:
		return syscall.B115200, true
	default:
		return 0, false
	}
}
