package linux

import "testing"

func TestPaperPresent(t *testing.T) {
	tests := []struct {
		name   string
		status byte
		want   bool
	}{
		{name: "paper available", status: 0x12, want: true},
		{name: "near end remains usable", status: 0x1e, want: true},
		{name: "paper absent", status: 0x72, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PaperPresent(tt.status); got != tt.want {
				t.Fatalf("PaperPresent(%#02x) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
