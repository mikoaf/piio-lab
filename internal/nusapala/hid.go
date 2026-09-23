package nusapala

import (
	"encoding/binary"
	"time"
)

const (
	EV_KEY = 0x01
)

type linuxInputEvent struct {
	Type  uint16
	Code  uint16
	Value int32
}

// HID boot keyboard reports, matching the scanner interface used by Nusapala.
// Also accepts multiple keys and ignores held-key repeats.
type hidDecoder struct {
	previous   [256]bool
	text       []byte
	last       time.Time
	discarding bool
}

type inputEventDecoder struct {
	text    []byte
	last    time.Time
	discard bool
	shift   bool
	pressed map[uint16]bool
}

func (d *inputEventDecoder) Feed(ev linuxInputEvent, now time.Time) string {
	if ev.Type != EV_KEY || ev.Value == 2 {
		return ""
	}
	if ev.Code == 42 || ev.Code == 54 {
		if ev.Value == 1 {
			d.shift = true
		} else {
			d.shift = false
		}
		return ""
	}
	if ev.Value == 0 {
		return ""
	}
	if ev.Code == 14 {
		if len(d.text) > 0 {
			d.text = d.text[:len(d.text)-1]
		}
		return ""
	}
	if ev.Code == 28 || ev.Code == 88 {
		if len(d.text) == 0 {
			return ""
		}
		s := string(d.text)
		d.text = nil
		return s
	}
	if c, ok := linuxInputChar(ev.Code, d.shift); ok {
		d.last = now
		if d.discard {
			return ""
		}
		if len(d.text) < 4096 {
			d.text = append(d.text, c)
		} else {
			d.text = nil
			d.discard = true
			return ""
		}
	}
	return ""
}

func (d *inputEventDecoder) Flush(now time.Time) string {
	if now.Sub(d.last) < 300*time.Millisecond {
		return ""
	}
	d.discard = false
	s := string(d.text)
	d.text = nil
	return s
}

func decodeInputEvent(raw []byte) (linuxInputEvent, bool) {
	if len(raw) < 24 {
		return linuxInputEvent{}, false
	}
	return linuxInputEvent{
		Type:  binary.LittleEndian.Uint16(raw[16:18]),
		Code:  binary.LittleEndian.Uint16(raw[18:20]),
		Value: int32(binary.LittleEndian.Uint32(raw[20:24])),
	}, true
}

func linuxInputChar(code uint16, shift bool) (byte, bool) {
	lower := map[uint16]byte{
		2: '1', 3: '2', 4: '3', 5: '4', 6: '5', 7: '6', 8: '7', 9: '8', 10: '9', 11: '0',
		16: 'q', 17: 'w', 18: 'e', 19: 'r', 20: 't', 21: 'y', 22: 'u', 23: 'i', 24: 'o', 25: 'p',
		30: 'a', 31: 's', 32: 'd', 33: 'f', 34: 'g', 35: 'h', 36: 'j', 37: 'k', 38: 'l',
		48: 'b', 49: 'n', 50: 'm', 51: ',', 52: '.', 53: '/', 54: ';', 55: '\'', 56: '`',
		57: ' ', 44: 'z', 45: 'x', 46: 'c', 47: 'v',
	}
	upper := map[uint16]byte{
		2: '!', 3: '@', 4: '#', 5: '$', 6: '%', 7: '^', 8: '&', 9: '*', 10: '(', 11: ')',
		16: 'Q', 17: 'W', 18: 'E', 19: 'R', 20: 'T', 21: 'Y', 22: 'U', 23: 'I', 24: 'O', 25: 'P',
		30: 'A', 31: 'S', 32: 'D', 33: 'F', 34: 'G', 35: 'H', 36: 'J', 37: 'K', 38: 'L',
		44: 'Z', 45: 'X', 46: 'C', 47: 'V', 48: 'B', 49: 'N', 50: 'M', 51: '<', 52: '>', 53: '?', 54: ':', 55: '"', 56: '~',
		57: ' ',
	}
	if shift {
		if c, ok := upper[code]; ok {
			return c, true
		}
	}
	if c, ok := lower[code]; ok {
		return c, true
	}
	if code == 14 {
		return 0, false
	}
	if code == 28 || code == 88 {
		return 0, false
	}
	return 0, false
}

func (d *hidDecoder) Feed(report []byte, now time.Time) (string, error) {
	if len(report) < 8 {
		return "", nil
	}
	var current [256]bool
	for _, key := range report[2:8] {
		if key >= 1 && key <= 3 {
			return "", nil
		}
		current[key] = true
	}
	var result string
	for _, key := range report[2:8] {
		if key == 0 || d.previous[key] {
			continue
		}
		if key == 40 || key == 88 {
			d.discarding = false
			if len(d.text) > 0 {
				result = string(d.text)
				d.text = nil
			}
			continue
		}
		if key == 42 {
			if len(d.text) > 0 {
				d.text = d.text[:len(d.text)-1]
			}
			continue
		}
		if c, ok := hidChar(key, report[0]&0x22 != 0); ok {
			d.last = now
			if d.discarding {
				continue
			}
			if len(d.text) < 4096 {
				d.text = append(d.text, c)
			} else {
				d.text = nil
				d.discarding = true
				d.previous = current
				return "", errQRTooLong
			}
			d.last = now
		}
	}
	d.previous = current
	return result, nil
}
func (d *hidDecoder) Flush(now time.Time) string {
	if now.Sub(d.last) < 300*time.Millisecond {
		return ""
	}
	d.discarding = false
	s := string(d.text)
	d.text = nil
	return s
}
func hidChar(key byte, shift bool) (byte, bool) {
	if key >= 4 && key <= 29 {
		base := byte('a')
		if shift {
			base = 'A'
		}
		return base + key - 4, true
	}
	if key >= 30 && key <= 39 {
		chars := "1234567890"
		if shift {
			chars = "!@#$%^&*()"
		}
		return chars[key-30], true
	}
	plain := map[byte]byte{43: '\t', 44: ' ', 45: '-', 46: '=', 47: '[', 48: ']', 49: '\\', 50: '#', 51: ';', 52: '\'', 53: '`', 54: ',', 55: '.', 56: '/'}
	shifted := map[byte]byte{43: '\t', 44: ' ', 45: '_', 46: '+', 47: '{', 48: '}', 49: '|', 50: '~', 51: ':', 52: '"', 53: '~', 54: '<', 55: '>', 56: '?'}
	if shift {
		c, ok := shifted[key]
		return c, ok
	}
	c, ok := plain[key]
	return c, ok
}
