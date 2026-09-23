//go:build nusapala && linux && cgo

package nusapala

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPDoesNotPrintFromForeignOrigin(t *testing.T) {
	s := NewStatus(log.New(io.Discard, "", 0))
	p := NewPeripherals(DefaultConfig(), s)
	h := newHandler(NewIntercom(context.Background(), nil, s), p, s)
	tests := []struct {
		origin, content string
		want            int
	}{
		{"http://evil.example", "application/json", 403},
		{"http://localhost", "text/plain", 415},
		{"http://localhost", "application/json", 202},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("POST", "http://localhost/api/print", strings.NewReader(`{"text":"test"}`))
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Content-Type", tt.content)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Fatalf("status=%d want=%d body=%s", w.Code, tt.want, w.Body)
		}
	}
	if len(p.prints) != 1 {
		t.Fatalf("unexpected prints: %d", len(p.prints))
	}
}
func TestQueueFullAndInvalidCall(t *testing.T) {
	s := NewStatus(log.New(io.Discard, "", 0))
	p := NewPeripherals(DefaultConfig(), s)
	for n := 0; n < 32; n++ {
		if err := p.QueuePrint("test"); err != nil {
			t.Fatal(err)
		}
	}
	if p.QueuePrint("overflow") == nil {
		t.Fatal("unbounded queue")
	}
	if p.QueuePrint(" ") == nil {
		t.Fatal("empty print accepted")
	}
	h := newHandler(NewIntercom(context.Background(), nil, s), p, s)
	r := httptest.NewRequest("POST", "/api/call", strings.NewReader(`{"type":"offer","sdp":"bad"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatal(w.Code)
	}
}
