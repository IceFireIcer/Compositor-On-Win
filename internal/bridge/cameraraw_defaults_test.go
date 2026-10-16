package bridge

// Camera Raw 的默认值在 Go（render.DefaultCameraRawSettings）与前端面板的内联兜底里各存
// 一份 —— 这正是票 80 反复踩的那个坑的形状。用一份共享夹具把它们钉在一起：
// internal/bridge/testdata/camera_raw_defaults.json 由 Go 生成，前端 state/cameraRawDefaults.ts
// 读同一份文件比对。任何一边改了默认值而没同步另一边，测试都会失败。

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"compositor-win/internal/render"
)

const cameraRawDefaultsFixture = "testdata/camera_raw_defaults.json"

func regenerateCameraRawDefaultsFixture(t *testing.T) {
	t.Helper()
	if os.Getenv("GOLDEN_REGENERATE") != "1" {
		return
	}
	blob, err := json.MarshalIndent(render.DefaultCameraRawSettings(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cameraRawDefaultsFixture, append(blob, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCameraRawDefaultsMatchTheSharedFixture(t *testing.T) {
	regenerateCameraRawDefaultsFixture(t)
	want, err := os.ReadFile(cameraRawDefaultsFixture)
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual any
	if err := json.Unmarshal(want, &expected); err != nil {
		t.Fatalf("夹具不是合法 JSON: %v", err)
	}
	blob, err := json.Marshal(render.DefaultCameraRawSettings())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(blob, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("默认值与共享夹具不符\n夹具: %s\nGo:   %s", want, blob)
	}
}
