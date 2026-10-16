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

// 票 78 验收 1 后半：预览取消旧任务，不让旧结果覆盖新设置。
//
// 判据是**预览修订号前进几次**。cancel/replace 语义下先发的慢请求会被丢弃，所以两次请求之后
// 修订号只该前进一次（后发的那次落地）；守卫若失效，慢的先发晚一点落地，会把它推到 2。
//
// 先发的那次必须真的慢，否则它先完成，这条什么也考不到。高斯模糊不行 —— 它是盒式近似，半径
// 拉满也不比半径 1 慢（第一版就是这么写的，去掉守卫照样通过）。动感模糊可以：代价随拖尾距离
// 增长，实测 distance 2000 在 500ms 内回不来，而 distance 1 是毫秒级。
func TestASupersededPreviewNeverOverwritesTheNewerSettings(t *testing.T) {
	svc, _ := newTestService(t, 256, 256)
	if _, err := svc.BeginFilterEdit(FilterMotion, "", `{"angle":0,"distance":1}`, 0, ""); err != nil {
		t.Fatal(err)
	}
	// 先发：拖尾拉满，慢。
	if _, err := svc.UpdateFilterPreview(`{"angle":0,"distance":2000}`); err != nil {
		t.Fatal(err)
	}
	// 后发：立刻到，快。它必须赢，而先发的那次要被丢掉。
	if _, err := svc.UpdateFilterPreview(`{"angle":0,"distance":1}`); err != nil {
		t.Fatal(err)
	}

	// 等后发的落地。
	deadline := time.Now().Add(10 * time.Second)
	for {
		svc.ws.mu.Lock()
		sess := svc.ws.activeSessionLocked()
		rev := 0
		if sess != nil && sess.filter != nil {
			rev = sess.filter.previewRev
		}
		svc.ws.mu.Unlock()
		if rev >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 先发的那次要跑得比这久，才可能「晚到」；给足时间让它落地（若守卫失效的话）。
	time.Sleep(3 * time.Second)
	svc.ws.mu.Lock()
	sess := svc.ws.activeSessionLocked()
	if sess == nil || sess.filter == nil {
		svc.ws.mu.Unlock()
		t.Fatal("滤镜会话不见了")
	}
	rev := sess.filter.previewRev
	svc.ws.mu.Unlock()
	if rev != 1 {
		t.Fatalf("预览修订号是 %d，想要 1 —— 先发的慢请求没有被丢弃", rev)
	}
}
