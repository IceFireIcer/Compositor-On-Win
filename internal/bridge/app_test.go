package bridge

import "testing"

func TestVersion(t *testing.T) {
	got := Version()
	if got == "" {
		t.Fatal("Version() is empty")
	}
	if got != wantVersion {
		t.Fatalf("Version() = %q, want %q", got, wantVersion)
	}
}
