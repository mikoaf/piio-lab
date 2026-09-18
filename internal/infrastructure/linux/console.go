package linux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
	"unsafe"
)

type Console struct {
	fd      int
	pending []byte
}

func NewConsole(f *os.File) (*Console, error) {
	fd := int(f.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	return &Console{fd: fd}, nil
}

func (c *Console) ReadLine(ctx context.Context) (string, error) {
	buf := make([]byte, 256)
	for {
		if i := bytes.IndexByte(c.pending, '\n'); i >= 0 {
			line := string(c.pending[:i+1])
			c.pending = append(c.pending[:0], c.pending[i+1:]...)
			return line, nil
		}
		ready, err := DescriptorReadable(c.fd, 25*time.Millisecond)
		if err != nil {
			return "", err
		}
		if !ready {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			default:
				continue
			}
		}
		n, err := syscall.Read(c.fd, buf)
		if n > 0 {
			c.pending = append(c.pending, buf[:n]...)
			continue
		}
		if err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) {
			return "", err
		}
		if n == 0 && err == nil {
			return "", io.EOF
		}
	}
}

func DescriptorReadable(fd int, timeout time.Duration) (bool, error) {
	if fd < 0 || fd >= int(unsafe.Sizeof(syscall.FdSet{})*8) {
		return false, fmt.Errorf("file descriptor %d di luar batas select", fd)
	}
	var set syscall.FdSet
	wordBits := int(unsafe.Sizeof(uintptr(0)) * 8)
	words := unsafe.Slice((*uintptr)(unsafe.Pointer(&set)), int(unsafe.Sizeof(set)/unsafe.Sizeof(uintptr(0))))
	words[fd/wordBits] |= uintptr(1) << uint(fd%wordBits)
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	n, err := syscall.Select(fd+1, &set, nil, nil, &tv)
	if err != nil {
		if errors.Is(err, syscall.EINTR) {
			return false, nil
		}
		return false, err
	}
	return n > 0, nil
}
