package nusapala

import "time"

// HID boot keyboard reports, matching the scanner interface used by Nusapala.
// Also accepts multiple keys and ignores held-key repeats.
type hidDecoder struct {
	previous   [256]bool
	text       []byte
	last       time.Time
	discarding bool
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
