package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WindowState is the persisted main-window size (ticket 02: size only;
// panel widths join when the layers panel becomes resizable).
type WindowState struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// WindowStore persists the window state as JSON under the user config dir
// (compositor-on-windows/window.json).
type WindowStore struct {
	path string
}

// NewWindowStoreAt targets an explicit path (tests).
func NewWindowStoreAt(path string) *WindowStore {
	return &WindowStore{path: path}
}

// DefaultWindowStore targets the per-user config directory.
func DefaultWindowStore() (*WindowStore, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("无法定位用户配置目录: %w", err)
	}
	return &WindowStore{path: filepath.Join(base, "compositor-on-windows", "window.json")}, nil
}

// Save writes the window size, creating the directory as needed.
func (s *WindowStore) Save(width, height int) error {
	data, err := json.MarshalIndent(WindowState{Width: width, Height: height}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

// Load reads the stored size. A missing file is not an error: it yields the
// zero state, which callers treat as "use defaults". A corrupt file errors.
func (s *WindowStore) Load() (WindowState, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return WindowState{}, nil
	}
	if err != nil {
		return WindowState{}, err
	}
	var state WindowState
	if err := json.Unmarshal(data, &state); err != nil {
		return WindowState{}, fmt.Errorf("窗口状态文件损坏: %w", err)
	}
	return state, nil
}
