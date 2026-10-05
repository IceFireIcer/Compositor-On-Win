package bridge

import "testing"

func TestServiceVersion(t *testing.T) {
	var s Service
	if got := s.Version(); got != wantVersion {
		t.Fatalf("Service.Version() = %q, want %q", got, wantVersion)
	}
}
