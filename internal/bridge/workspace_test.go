package bridge

import (
	"strings"
	"testing"
)

func snapIDs(s Snapshot) []string {
	ids := make([]string, len(s.Tabs))
	for i, d := range s.Tabs {
		ids[i] = d.ID
	}
	return ids
}

func TestWorkspaceCreateAddsAndActivates(t *testing.T) {
	ws := NewWorkspace()
	s1, err := ws.NewDocument(1920, 1080, 72)
	if err != nil {
		t.Fatal(err)
	}
	if len(s1.Tabs) != 1 || s1.ActiveID != s1.Tabs[0].ID {
		t.Fatalf("first create: tabs=%v active=%q", snapIDs(s1), s1.ActiveID)
	}
	if s1.Tabs[0].Name != "未命名" {
		t.Fatalf("first name = %q", s1.Tabs[0].Name)
	}
	if !s1.Tabs[0].Dirty {
		t.Fatal("new document should be dirty (未保存)")
	}

	s2, err := ws.NewDocument(800, 600, 72)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Tabs) != 2 {
		t.Fatalf("tabs = %d", len(s2.Tabs))
	}
	if s2.Tabs[1].Name != "未命名 2" {
		t.Fatalf("second name = %q", s2.Tabs[1].Name)
	}
	if s2.ActiveID != s2.Tabs[1].ID {
		t.Fatal("new document should become active")
	}
}

func TestWorkspaceCreateRejectsInvalid(t *testing.T) {
	ws := NewWorkspace()
	_, err := ws.NewDocument(0, 100, 72)
	if err == nil || !strings.Contains(err.Error(), "宽高") {
		t.Fatalf("err = %v, want 宽高 validation failure", err)
	}
	if s := ws.Snapshot(); len(s.Tabs) != 0 {
		t.Fatalf("rejected create must not add a tab, got %d", len(s.Tabs))
	}
}

func TestWorkspaceSelectTab(t *testing.T) {
	ws := NewWorkspace()
	s, _ := ws.NewDocument(100, 100, 72)
	first := s.ActiveID
	s, _ = ws.NewDocument(100, 100, 72)
	second := s.ActiveID

	s, err := ws.SelectTab(first)
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveID != first {
		t.Fatalf("active = %q, want %q", s.ActiveID, first)
	}
	if _, err := ws.SelectTab(second); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.SelectTab("missing"); err == nil {
		t.Fatal("selecting a missing tab should fail")
	}
}

func TestWorkspaceCloseActiveActivatesNeighbor(t *testing.T) {
	ws := NewWorkspace()
	s, _ := ws.NewDocument(100, 100, 72)
	first := s.ActiveID
	s, _ = ws.NewDocument(100, 100, 72)
	second := s.ActiveID

	s, err := ws.CloseTab(second)
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveID != first || len(s.Tabs) != 1 {
		t.Fatalf("after closing active: active=%q tabs=%d", s.ActiveID, len(s.Tabs))
	}
}

func TestWorkspaceCloseLastEmpties(t *testing.T) {
	ws := NewWorkspace()
	s, _ := ws.NewDocument(100, 100, 72)
	s, err := ws.CloseTab(s.ActiveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tabs) != 0 || s.ActiveID != "" {
		t.Fatalf("last close: tabs=%d active=%q", len(s.Tabs), s.ActiveID)
	}
}

func TestWorkspaceCloseInactiveKeepsActive(t *testing.T) {
	ws := NewWorkspace()
	s, _ := ws.NewDocument(100, 100, 72)
	first := s.ActiveID
	s, _ = ws.NewDocument(100, 100, 72)
	_ = s

	// first is now inactive; closing it must keep the current tab active.
	s, err := ws.CloseTab(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tabs) != 1 {
		t.Fatalf("tabs = %d", len(s.Tabs))
	}
	if s.ActiveID == first {
		t.Fatal("closed tab must not remain active")
	}
}

func TestWorkspaceCloseMissingFails(t *testing.T) {
	ws := NewWorkspace()
	if _, err := ws.CloseTab("missing"); err == nil {
		t.Fatal("closing a missing tab should fail")
	}
}
