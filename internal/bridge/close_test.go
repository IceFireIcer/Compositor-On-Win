package bridge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
)

func TestOnBeforeCloseAllowsCleanWorkspaceWithoutShowingDialog(t *testing.T) {
	svc := NewService(NewWorkspace())
	called := false
	svc.messageDialog = func(context.Context, runtime.MessageDialogOptions) (string, error) {
		called = true
		return "No", nil
	}

	if prevent := svc.OnBeforeClose(context.Background()); prevent {
		t.Fatal("a clean workspace should allow the native window to close")
	}
	if called {
		t.Fatal("clean workspace should not show a close confirmation")
	}
}

func TestOnBeforeCloseRequiresExplicitYesForDirtyWorkspace(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	var got runtime.MessageDialogOptions
	svc.messageDialog = func(_ context.Context, options runtime.MessageDialogOptions) (string, error) {
		got = options
		return "No", nil
	}

	if prevent := svc.OnBeforeClose(context.Background()); !prevent {
		t.Fatal("dirty workspace must remain open when the native dialog returns No")
	}
	if got.Type != runtime.QuestionDialog || got.DefaultButton != "No" {
		t.Fatalf("close dialog options = %+v, want question with No as the default", got)
	}
	if !strings.Contains(got.Message, "未保存") {
		t.Fatalf("close dialog message = %q, want an unsaved-work warning", got.Message)
	}

	svc.messageDialog = func(_ context.Context, options runtime.MessageDialogOptions) (string, error) {
		if options.Type != runtime.QuestionDialog {
			t.Fatalf("confirmed close used dialog type %q", options.Type)
		}
		return "Yes", nil
	}
	if prevent := svc.OnBeforeClose(context.Background()); prevent {
		t.Fatal("explicit native Yes should allow the window to close")
	}
}

func TestOnBeforeCloseFailsClosedWhenDialogFailsOrReturnsUnknown(t *testing.T) {
	tests := []struct {
		name     string
		response string
		err      error
	}{
		{name: "dialog error", err: errors.New("dialog unavailable")},
		{name: "unknown response", response: "Cancel"},
		{name: "empty response", response: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(t, 16, 16)
			svc.messageDialog = func(context.Context, runtime.MessageDialogOptions) (string, error) {
				return tt.response, tt.err
			}
			if prevent := svc.OnBeforeClose(context.Background()); !prevent {
				t.Fatal("close must be prevented unless the native dialog explicitly returns Yes")
			}
		})
	}
}

func TestOnBeforeCloseProtectsInFlightEditAndSave(t *testing.T) {
	t.Run("stroke", func(t *testing.T) {
		svc, _ := newTestService(t, 16, 16)
		path := filepath.Join(t.TempDir(), "stroke.comp")
		if _, err := svc.ws.SaveActive(path); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.BeginStroke(1, 1); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = svc.ws.EndStroke() }()

		dialogs := 0
		svc.messageDialog = func(context.Context, runtime.MessageDialogOptions) (string, error) {
			dialogs++
			return "No", nil
		}
		if prevent := svc.OnBeforeClose(context.Background()); !prevent {
			t.Fatal("a live brush stroke must prevent native close")
		}
		if dialogs != 1 {
			t.Fatalf("dialog calls = %d, want 1", dialogs)
		}
	})

	t.Run("save", func(t *testing.T) {
		svc, _ := newTestService(t, 16, 16)
		path := filepath.Join(t.TempDir(), "save.comp")
		if _, err := svc.ws.SaveActive(path); err != nil {
			t.Fatal(err)
		}
		started, release := make(chan struct{}), make(chan struct{})
		var store project.Store
		svc.ws.savePackage = func(path string, doc *domain.Document, assets map[string][]byte) error {
			close(started)
			<-release
			return store.Save(path, doc, assets)
		}
		saved := make(chan error, 1)
		go func() {
			_, err := svc.ws.SaveActive(path)
			saved <- err
		}()
		<-started
		defer func() {
			close(release)
			if err := <-saved; err != nil {
				t.Errorf("overlapping save failed: %v", err)
			}
		}()

		svc.messageDialog = func(context.Context, runtime.MessageDialogOptions) (string, error) {
			return "No", nil
		}
		if prevent := svc.OnBeforeClose(context.Background()); !prevent {
			t.Fatal("a save in flight must prevent native close")
		}
	})
}

func TestOnBeforeCloseWithoutContextFailsClosedForDirtyWorkspace(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	svc.messageDialog = func(context.Context, runtime.MessageDialogOptions) (string, error) {
		t.Fatal("a nil Wails context must not invoke the native dialog")
		return "Yes", nil
	}

	if prevent := svc.OnBeforeClose(nil); !prevent {
		t.Fatal("dirty workspace without a Wails context must prevent close")
	}
}

func TestOnBeforeCloseDoesNotHoldWorkspaceLockDuringDialog(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	dialogReturned := make(chan struct{})
	svc.messageDialog = func(context.Context, runtime.MessageDialogOptions) (string, error) {
		if _, err := svc.ws.newDocumentWithWhiteBackground(8, 8, 72); err != nil {
			t.Errorf("workspace mutation while dialog is open failed: %v", err)
		}
		close(dialogReturned)
		return "No", nil
	}

	done := make(chan bool, 1)
	go func() { done <- svc.OnBeforeClose(context.Background()) }()
	select {
	case <-dialogReturned:
	case <-time.After(time.Second):
		t.Fatal("close dialog appears to hold the workspace lock")
	}
	if prevent := <-done; !prevent {
		t.Fatal("dialog No should prevent close")
	}
}
