// Package bridge hosts the Wails service bindings the frontend calls.
// It is the thin transport layer; document logic lives in internal/domain
// and friends. (Ticket 01: version round-trip only.)
package bridge

// wantVersion is the scaffold milestone version. Bumped per milestone.
const wantVersion = "0.1.0"

// Version reports the application version to the frontend status bar.
func Version() string {
	return wantVersion
}

// Service is the object bound into the Wails runtime (see main.go Bind).
type Service struct{}

// Version exposes the app version as a bound method for the frontend.
func (s *Service) Version() string {
	return Version()
}
