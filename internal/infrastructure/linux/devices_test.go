package linux

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestALSADeviceFromNode(t *testing.T) {
	got, err := ALSADeviceFromNode("/dev/snd/pcmC2D0c")
	if err != nil || got != "plughw:2,0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestKeyCharacter(t *testing.T) {
	if got, ok := KeyCharacter(16, false); !ok || got != 'q' {
		t.Fatalf("normal key: %q %v", got, ok)
	}
	if got, ok := KeyCharacter(16, true); !ok || got != 'Q' {
		t.Fatalf("shift key: %q %v", got, ok)
	}
}

func TestReadSerialLinesNonBlocking(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if err := syscall.SetNonblock(int(r.Fd()), true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	lines := make(chan string, 2)
	done := make(chan error, 1)
	go func() { done <- ReadSerialLines(ctx, r, func(line string) { lines <- line }) }()
	if _, err := w.Write([]byte("one\r\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"one", "two"} {
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("line=%q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for serial line")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("non-blocking serial reader did not stop")
	}
}
