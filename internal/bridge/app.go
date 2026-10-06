// Package bridge hosts the Wails service bindings the frontend calls.
// It is the thin transport layer: document state lives in the Workspace's
// per-tab sessions (internal/domain model + history + in-memory bitmaps),
// package IO in project.Store, pixels in render. (Ticket 01 started with
// the version round-trip only; the M2 wiring mounted the full document
// contract onto Service.)
package bridge

import (
	"context"

	"compositor-win/internal/project"
)

// wantVersion is the scaffold milestone version. Bumped per milestone.
const wantVersion = "0.1.0"

// Version reports the application version to the frontend status bar.
func Version() string {
	return wantVersion
}

// Service is the object bound into the Wails runtime (see main.go Bind).
// The Wails context arrives via Startup and backs the file dialogs; the
// dialog functions are fields so tests can stub them.
type Service struct {
	ws    *Workspace
	store project.Store

	ctx context.Context

	openDialog func(ctx context.Context) (string, error)
	saveDialog func(ctx context.Context) (string, error)
}

// NewService wires the service onto a workspace (shared with the
// workspace binding in main.go).
func NewService(ws *Workspace) *Service {
	return &Service{
		ws:         ws,
		openDialog: defaultOpenDialog,
		saveDialog: defaultSaveDialog,
	}
}

// Startup is the Wails lifecycle hook: captures the runtime context the
// dialogs need.
func (s *Service) Startup(ctx context.Context) {
	s.ctx = ctx
}

// Version exposes the app version as a bound method for the frontend.
func (s *Service) Version() string {
	return Version()
}
