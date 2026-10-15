package bridge

import (
	"encoding/json"
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
