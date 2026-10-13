package bridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"compositor-win/internal/domain"
)

type filterPreviewEnvelope struct {
	Rev       int              `json:"rev"`
	FilterRev int              `json:"filterRev"`
	Doc       *domain.Document `json:"doc"`
}

func decodeFilterPreviewEnvelope(raw string) (filterPreviewEnvelope, error) {
	var out filterPreviewEnvelope
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return filterPreviewEnvelope{}, err
	}
	return out, nil
}

func parseFilterPreviewEnvelope(t *testing.T, raw string) filterPreviewEnvelope {
	t.Helper()
	out, err := decodeFilterPreviewEnvelope(raw)
	if err != nil {
		t.Fatalf("预览快照不是合法 JSON: %v\n%s", err, raw)
	}
	return out
}

func renderFilterPreviewRevision(svc *Service, documentID string) (int, error) {
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render/"+documentID+".json", nil)
	svc.RenderHandler().ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		return 0, fmt.Errorf("渲染快照状态 = %d: %s", rec.Code, rec.Body.String())
	}
	var snapshot struct {
		FilterRev int `json:"filterRev"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		return 0, err
	}
	return snapshot.FilterRev, nil
}

func TestUpdateFilterPreviewRepliesAreSafeDuringWorkerWrites(t *testing.T) {
	svc, _ := newTestService(t, 64, 64)
	before := mustDocumentSnapshot(t, svc)
	if _, err := svc.BeginFilterEdit(FilterAdjustPrefix+"Levels", "", `{"adjustment":null}`, 0, ""); err != nil {
		t.Fatal(err)
	}

	// Concurrent callers model slider updates arriving through separate Wails
	// calls while earlier preview workers are still rendering.
	const callers = 4
	const updatesPerCaller = 8
	start := make(chan struct{})
	errs := make(chan error, callers*updatesPerCaller)
	var workers sync.WaitGroup
	workers.Add(callers)
	for caller := 0; caller < callers; caller++ {
		go func(caller int) {
			defer workers.Done()
			<-start
			for update := 0; update < updatesPerCaller; update++ {
				raw, err := svc.UpdateFilterPreview(`{"adjustment":null}`)
				if err != nil {
					errs <- err
					continue
				}
				got, decodeErr := decodeFilterPreviewEnvelope(raw)
				if decodeErr != nil {
					errs <- fmt.Errorf("预览回复不是合法 JSON: %v", decodeErr)
					continue
				}
				if got.Doc == nil {
					errs <- fmt.Errorf("预览回复缺少文档")
					continue
				}
				if !reflect.DeepEqual(got.Doc, before.Doc) {
					errs <- fmt.Errorf("预览回复改变了文档内容")
				}
			}
		}(caller)
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if _, err := svc.UpdateFilterPreview(`{"adjustment":null}`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		filterRev, err := renderFilterPreviewRevision(svc, before.Doc.DocumentID)
		if err != nil {
			t.Fatal(err)
		}
		if filterRev > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("并发预览未在时限内落地")
		}
		time.Sleep(time.Millisecond)
	}

	canceled, err := svc.CancelFilterEdit()
	if err != nil {
		t.Fatal(err)
	}
	got := parseFilterPreviewEnvelope(t, canceled)
	if got.FilterRev != 0 {
		t.Fatalf("取消后 filterRev = %d，想要 0", got.FilterRev)
	}
	if got.Rev != before.Rev+1 || !reflect.DeepEqual(got.Doc, before.Doc) {
		t.Fatalf("取消预览改变了文档或修订未前进: rev %d/%d", before.Rev, got.Rev)
	}
}
