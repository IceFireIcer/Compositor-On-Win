package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCMYKProofLookupsRefusesWhenNoProfileExists: a proof with no profile is
// an error the menu can show, not an invented lookup. Machines that do have a
// generic CMYK profile take the other branch and must return three lattices.
func TestCMYKProofLookupsRefusesWhenNoProfileExists(t *testing.T) {
	t.Setenv("COMPOSITOR_COLORIO_DATA_DIR", t.TempDir())
	svc, _ := newTestService(t, 8, 8)
	raw, err := svc.CMYKProofLookups()
	if err != nil {
		if strings.Contains(err.Error(), "没有可用的 CMYK 配置") {
			return
		}
		t.Fatalf("CMYKProofLookups: %v", err)
	}
	var reply cmykProofReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatalf("解析校样回包: %v", err)
	}
	if reply.Dimension != 33 || reply.Proof == "" || reply.Warning == "" || reply.Marked == "" {
		t.Fatalf("校样回包不完整: %+v", reply)
	}
}

// TestProofingLeavesTheDocumentAndTheFileAlone is ticket 72's rule: the proof
// is a view transform (ADR-0008 §1). Asking for its lookups — which is all
// "turning the proof on" does on the Go side — must not move a pixel, bump
// the revision, or change a byte of what Save writes.
func TestProofingLeavesTheDocumentAndTheFileAlone(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 8, 8, 120)
	path := filepath.Join(t.TempDir(), "proof.comp")
	svc.saveDialog = func(context.Context) (string, error) { return path, nil }
	save := func() {
		t.Helper()
		if _, err := svc.SaveProjectDialog(); err != nil {
			t.Fatalf("保存: %v", err)
		}
	}
	// A .comp is a directory package, so compare every file it holds.
	contents := func() map[string][]byte {
		t.Helper()
		out := map[string][]byte{}
		err := filepath.WalkDir(path, func(p string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			rel, relErr := filepath.Rel(path, p)
			if relErr != nil {
				return relErr
			}
			data, readErr := os.ReadFile(p)
			if readErr != nil {
				return readErr
			}
			out[rel] = data
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	save()
	before := contents()
	rev := sess.rev

	// The canvas samples these lookups; the menu also lists and chooses
	// profiles. None of it is document state.
	if _, err := svc.CMYKProofLookups(); err != nil && !strings.Contains(err.Error(), "没有可用的 CMYK 配置") {
		t.Fatalf("CMYKProofLookups: %v", err)
	}
	if _, err := svc.CMYKProfiles(); err != nil {
		t.Fatalf("CMYKProfiles: %v", err)
	}

	if sess.rev != rev {
		t.Fatalf("取校样推进了 rev：%d → %d", rev, sess.rev)
	}
	save()
	after := contents()
	if len(before) != len(after) {
		t.Fatalf("保存的文件数变了：%d → %d", len(before), len(after))
	}
	for name, want := range before {
		if !bytes.Equal(want, after[name]) {
			t.Fatalf("取校样改变了 %s", name)
		}
	}
}
