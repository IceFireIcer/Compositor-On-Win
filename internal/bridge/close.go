package bridge

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// OnBeforeClose is the Wails window-close hook. true prevents shutdown.
// Every tab participates, including a saved tab with an edit or save in flight.
// The native Yes/No dialog defaults to No; only an explicit Yes discards work.
func (s *Service) OnBeforeClose(ctx context.Context) (prevent bool) {
	if s.ws == nil {
		return false
	}
	s.ws.mu.Lock()
	needsConfirmation := false
	for _, tab := range s.ws.tabs {
		sess := s.ws.sessions[tab.ID]
		if tab.Dirty || (sess != nil && (sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving)) {
			needsConfirmation = true
			break
		}
	}
	s.ws.mu.Unlock()
	if !needsConfirmation {
		return false
	}
	if ctx == nil || s.messageDialog == nil {
		return true
	}
	// Release the document lock before blocking on a native dialog: ongoing
	// edits and package saves must be able to finish while the user decides.
	answer, err := s.messageDialog(ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "退出 Compositor",
		Message:       "项目中有未保存或尚未完成的更改。退出会丢弃这些更改。确定退出吗？",
		Buttons:       []string{"Yes", "No"},
		DefaultButton: "No",
		CancelButton:  "No",
	})
	return err != nil || answer != "Yes"
}
