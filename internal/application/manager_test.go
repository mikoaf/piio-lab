package application

import (
	"testing"

	"piio-lab/internal/domain"
)

func TestMatchesSelector(t *testing.T) {
	d := domain.USBDevice{VendorID: "04b8", ProductID: "0e27", Manufacturer: "EPSON", Product: "TM-T82X"}
	if !matchesSelector(d, domain.Selector{VendorID: "04B8", NameContains: []string{"tm-t82"}}) {
		t.Fatal("expected Epson selector to match")
	}
	if matchesSelector(d, domain.Selector{VendorID: "0c2e"}) {
		t.Fatal("unexpected vendor match")
	}
}

func TestRoleCompatibility(t *testing.T) {
	d := domain.USBDevice{Nodes: map[string][]string{"tty": {"/dev/ttyUSB0"}, "input": {"/dev/input/event4"}}}
	if !roleCompatible(domain.RoleScanner, d, "keyboard") || !roleCompatible(domain.RoleScanner, d, "serial") || !roleCompatible(domain.RoleESP32, d, "") {
		t.Fatal("expected compatible device nodes")
	}
	if roleCompatible(domain.RoleAudio, d, "") {
		t.Fatal("serial device must not match audio")
	}
}
