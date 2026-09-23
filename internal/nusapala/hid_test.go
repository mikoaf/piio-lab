package nusapala

import (
	"strings"
	"testing"
	"time"
)

func TestQRReportsAndIdleTerminator(t *testing.T) {
	d := hidDecoder{}
	now := time.Now()
	reports := [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}, {0, 0, 4, 0, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 0, 0, 0}, {0x20, 0, 30, 0, 0, 0, 0, 0}, {0, 0, 40, 0, 0, 0, 0, 0}}
	var got string
	for _, r := range reports {
		s, err := d.Feed(r, now)
		if err != nil {
			t.Fatal(err)
		}
		got += s
	}
	if got != "a!" {
		t.Fatalf("QR=%q", got)
	}
	d.Feed([]byte{0, 0, 5, 0, 0, 0, 0, 0}, now)
	if s := d.Flush(now.Add(299 * time.Millisecond)); s != "" {
		t.Fatal("premature flush")
	}
	if s := d.Flush(now.Add(301 * time.Millisecond)); s != "b" {
		t.Fatalf("flush=%q", s)
	}
	if s := d.Flush(now.Add(time.Second)); s != "" {
		t.Fatal("duplicate flush")
	}
}
func TestQRShortReportAndLimit(t *testing.T) {
	d := hidDecoder{}
	now := time.Now()
	if s, e := d.Feed([]byte{1}, now); s != "" || e != nil {
		t.Fatal("short report")
	}
	d.text = []byte(strings.Repeat("x", 4096))
	if _, e := d.Feed([]byte{0, 0, 4, 0, 0, 0, 0, 0}, now); e == nil {
		t.Fatal("oversized QR accepted")
	}
	d.Feed([]byte{0, 0, 5, 0, 0, 0, 0, 0}, now)
	if s := d.Flush(now.Add(time.Second)); s != "" {
		t.Fatal("oversized QR suffix printed")
	}
}
