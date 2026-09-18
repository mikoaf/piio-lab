package linux

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	eventKey      = 0x01
	keyPressed    = 0x01
	keyEnter      = 28
	keyLeftShift  = 42
	keyRightShift = 54
	evIOCGrab     = 0x40044590
)

type inputEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

func OpenKeyboardExclusive(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), evIOCGrab, 1)
	if errno != 0 {
		f.Close()
		return nil, fmt.Errorf("mengambil input scanner secara eksklusif: %w", errno)
	}
	return f, nil
}

func CloseKeyboard(f *os.File) {
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), evIOCGrab, 0)
	_ = f.Close()
}

func ReadKeyboardEvents(ctx context.Context, f *os.File, emit func(string)) {
	size := int(unsafe.Sizeof(inputEvent{}))
	buf := make([]byte, size)
	var text bytes.Buffer
	shift := false
	if err := syscall.SetNonblock(int(f.Fd()), true); err != nil {
		return
	}
	for ctx.Err() == nil {
		ready, err := DescriptorReadable(int(f.Fd()), 25*time.Millisecond)
		if err != nil || !ready {
			continue
		}
		n, err := syscall.Read(int(f.Fd()), buf)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				continue
			}
			return
		}
		if n != size {
			continue
		}
		var ev inputEvent
		if err := binary.Read(bytes.NewReader(buf), binary.LittleEndian, &ev); err != nil || ev.Type != eventKey {
			continue
		}
		if ev.Code == keyLeftShift || ev.Code == keyRightShift {
			shift = ev.Value != 0
			continue
		}
		if ev.Value != keyPressed {
			continue
		}
		if ev.Code == keyEnter {
			if text.Len() > 0 {
				emit(text.String())
				text.Reset()
			}
			continue
		}
		if ch, ok := KeyCharacter(ev.Code, shift); ok {
			text.WriteByte(ch)
		}
	}
}

func KeyCharacter(code uint16, shift bool) (byte, bool) {
	keys := map[uint16][2]byte{
		2: {'1', '!'}, 3: {'2', '@'}, 4: {'3', '#'}, 5: {'4', '$'}, 6: {'5', '%'},
		7: {'6', '^'}, 8: {'7', '&'}, 9: {'8', '*'}, 10: {'9', '('}, 11: {'0', ')'},
		12: {'-', '_'}, 13: {'=', '+'}, 16: {'q', 'Q'}, 17: {'w', 'W'}, 18: {'e', 'E'},
		19: {'r', 'R'}, 20: {'t', 'T'}, 21: {'y', 'Y'}, 22: {'u', 'U'}, 23: {'i', 'I'},
		24: {'o', 'O'}, 25: {'p', 'P'}, 26: {'[', '{'}, 27: {']', '}'}, 30: {'a', 'A'},
		31: {'s', 'S'}, 32: {'d', 'D'}, 33: {'f', 'F'}, 34: {'g', 'G'}, 35: {'h', 'H'},
		36: {'j', 'J'}, 37: {'k', 'K'}, 38: {'l', 'L'}, 39: {';', ':'}, 40: {'\'', '"'},
		41: {'`', '~'}, 43: {'\\', '|'}, 44: {'z', 'Z'}, 45: {'x', 'X'}, 46: {'c', 'C'},
		47: {'v', 'V'}, 48: {'b', 'B'}, 49: {'n', 'N'}, 50: {'m', 'M'}, 51: {',', '<'},
		52: {'.', '>'}, 53: {'/', '?'}, 57: {' ', ' '},
	}
	pair, ok := keys[code]
	if !ok {
		return 0, false
	}
	if shift {
		return pair[1], true
	}
	return pair[0], true
}
