package cli

import "testing"

func TestIsYes(t *testing.T) {
	for _, value := range []string{"y", "Y", "yes", "Ya", " ya \n"} {
		if !isYes(value) {
			t.Fatalf("expected %q to be yes", value)
		}
	}
	for _, value := range []string{"", "n", "no", "tidak"} {
		if isYes(value) {
			t.Fatalf("expected %q to be no", value)
		}
	}
}
