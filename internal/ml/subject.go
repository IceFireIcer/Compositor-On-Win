// Package ml runs the subject-segmentation pipeline (ticket 37, ADR-0005
// appendix): the u2netp salient-object model through ONNX Runtime, refined
// by render.RefineSubjectMask (the GuidedMatte port).
//
// Windows builds put the pinned u2netp model beside the executable in models/.
// A development checkout may download the same verified model into app data.
// ONNX Runtime is distributed beside the executable. Neither binary is stored
// in the repository.
package ml

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	ort "github.com/yalue/onnxruntime_go"

	"compositor-win/internal/render"
)

// ModelURL is the pinned u2netp ONNX download (the rembg project's copy of
// the original u2net release, Apache-2.0).
const ModelURL = "https://github.com/danielgatis/rembg/releases/download/v0.0.0/u2netp.onnx"

// ModelSHA256 pins the artifact used by the build preparation script as well
// as the packaged, cached and downloaded copies used at runtime.
const ModelSHA256 = "309c8469258dda742793dce0ebea8e6dd393174f89934733ecc8b14c76f4ddd8"

// modelMinBytes gives an early error for truncated model files.
const modelMinBytes = 1 << 21 // 2 MiB (u2netp is ~4.6 MB)

// ModelFileName / LibFileName are what EnsureModel looks for.
const ModelFileName = "u2netp.onnx"

func libFileName() string {
	if runtime.GOOS == "windows" {
		return "onnxruntime.dll"
	}
	if runtime.GOOS == "darwin" {
		return "libonnxruntime.dylib"
	}
	return "libonnxruntime.so"
}

// ModelDir is the cached model's directory: %APPDATA%/Compositor/models on
// Windows, ~/.local/share/Compositor/models elsewhere.
func ModelDir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "Compositor", "models")
}

func bundledModelPath() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(executable), "models", ModelFileName)
}

func cachedModelPath() string { return filepath.Join(ModelDir(), ModelFileName) }

func modelPath() string {
	return preferredModelPath(bundledModelPath(), cachedModelPath())
}

func preferredModelPath(bundled, cached string) string {
	if bundled != "" {
		if _, err := os.Stat(bundled); !os.IsNotExist(err) {
			return bundled
		}
	}
	return cached
}
func libPath() string {
	if executable, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(executable), libFileName())
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, dir := range []string{".", "models"} {
		p := filepath.Join(dir, libFileName())
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// The two prerequisites report separately: ErrRuntimeMissing when the ONNX
// Runtime library is absent (it is not downloaded), ErrModelMissing when only
// the weights are (EnsureModel fetches those on first use).
// The two prerequisites have different remedies, so they are separate errors:
// the previous combined message told the user to run a「下载分割模型」command
// that exists nowhere in the UI (ml.EnsureModel was never called — see
// EnsureReady).
var (
	ErrRuntimeMissing = errors.New("ONNX Runtime 未安装：请把 onnxruntime.dll 放到 Compositor.exe 同级目录")
	ErrModelMissing   = errors.New("分割模型缺失：请重新安装完整安装包，或把 u2netp.onnx 放到程序同级的 models 目录；开发版可自动下载到 %APPDATA%/Compositor/models")
)

var (
	initMtx    sync.Mutex
	modelMtx   sync.Mutex
	initReady  bool
	sessionMtx sync.Mutex
	session    *ort.DynamicAdvancedSession
	sessionFor string
)

func verifyModel(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, f)
	if err != nil {
		return err
	}
	if size < modelMinBytes {
		return fmt.Errorf("分割模型不完整: %d 字节", size)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != ModelSHA256 {
		return fmt.Errorf("分割模型 SHA-256 校验不符: %s", got)
	}
	return nil
}

// EnsureModel accepts the packaged model first. Development builds can fall
// back to the app-data cache and download the same pinned file when absent.
func EnsureModel() error {
	// Multiple entry points may request the first download concurrently.
	modelMtx.Lock()
	defer modelMtx.Unlock()
	p := modelPath()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		return verifyModel(p)
	}
	if err := os.MkdirAll(ModelDir(), 0o755); err != nil {
		return err
	}
	tmp := p + ".download"
	resp, err := httpGet(ModelURL)
	if err != nil {
		return fmt.Errorf("下载分割模型失败: %w", err)
	}
	defer resp.Body.Close()
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, hasher), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if size < modelMinBytes {
		os.Remove(tmp)
		return fmt.Errorf("分割模型下载不完整: %d 字节", size)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != ModelSHA256 {
		os.Remove(tmp)
		return fmt.Errorf("分割模型校验不符: %s", got)
	}
	return os.Rename(tmp, p)
}

func httpGet(url string) (*http.Response, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// setup initializes the ONNX runtime, once. A failure is deliberately NOT
// cached: the model is downloaded on first use (EnsureReady), so a call that
// ran before the download must not poison every later one — which is exactly
// what the old sync.Once did.
func setup() error {
	initMtx.Lock()
	defer initMtx.Unlock()
	if initReady {
		return nil
	}
	lib := libPath()
	if lib == "" {
		return ErrRuntimeMissing
	}
	if _, err := os.Stat(modelPath()); err != nil {
		return ErrModelMissing
	}
	ort.SetSharedLibraryPath(lib)
	if err := ort.InitializeEnvironment(); err != nil {
		return fmt.Errorf("初始化 ONNX Runtime 失败: %w", err)
	}
	initReady = true
	return nil
}

// EnsureReady uses the packaged model when present; development runs can
// download the same verified model before initializing ONNX Runtime.
func EnsureReady() error {
	if libPath() == "" {
		return ErrRuntimeMissing
	}
	if err := EnsureModel(); err != nil {
		return err
	}
	return setup()
}

const inputSide = 320

// RunSubjectMaskRaw runs the model over the bitmap and returns the raw
// salient-object mask (normalized to 0–255 gray, the bitmap's size) — the
// cacheable part while a panel is open.
func RunSubjectMaskRaw(b *render.Bitmap) (*render.Bitmap, error) {
	// The packaged model is preferred; development runs can fetch it.
	if err := EnsureReady(); err != nil {
		return nil, err
	}
	sessionMtx.Lock()
	defer sessionMtx.Unlock()
	if session == nil || sessionFor != modelPath() {
		// The graph's tensor names are read from the model instead of being
		// hardcoded: the pinned u2netp export calls its input "input.1" but
		// names the eight outputs after their producer nodes ("1959"…"1966"),
		// so a literal "output.0" made every inference fail with
		// "Invalid output name: output.0" — the feature was dead even with the
		// model and runtime installed.
		inputs, outputs, err := ort.GetInputOutputInfo(modelPath())
		if err != nil {
			return nil, fmt.Errorf("读取分割模型接口失败: %w", err)
		}
		if len(inputs) == 0 || len(outputs) == 0 {
			return nil, fmt.Errorf("分割模型没有输入/输出张量")
		}
		// Pick the output by shape, not by position: u2netp exports one map per
		// decoder stage under node-derived tensor names (the pinned rembg copy
		// calls them "1959"…"1966"), and which stage is listed first is the
		// exporter's business, not ours. The salient map is the 1×1×H×W one;
		// inputs[0] carries the H/W we feed it.
		out := outputs[0].Name
		if len(inputs[0].Dimensions) == 4 {
			h, w := inputs[0].Dimensions[2], inputs[0].Dimensions[3]
			for _, o := range outputs {
				if len(o.Dimensions) == 4 && o.Dimensions[1] == 1 &&
					o.Dimensions[2] == h && o.Dimensions[3] == w {
					out = o.Name
					break
				}
			}
		}
		s, err := ort.NewDynamicAdvancedSession(modelPath(),
			[]string{inputs[0].Name}, []string{out}, nil)
		if err != nil {
			return nil, fmt.Errorf("加载分割模型失败: %w", err)
		}
		session = s
		sessionFor = modelPath()
	}
	// u2netp: 1×3×320×320 NCHW, RGB in 0–1.
	small := render.DownscaleBitmap(b, inputSide, inputSide)
	input := make([]float32, 3*inputSide*inputSide)
	for y := 0; y < inputSide; y++ {
		for x := 0; x < inputSide; x++ {
			i := (y*inputSide + x) * 4
			a := float32(small.Pix[i+3])
			var r, g, bl float32
			if a > 0 {
				r = float32(small.Pix[i]) / a
				g = float32(small.Pix[i+1]) / a
				bl = float32(small.Pix[i+2]) / a
			}
			input[y*inputSide+x] = r
			input[inputSide*inputSide+y*inputSide+x] = g
			input[2*inputSide*inputSide+y*inputSide+x] = bl
		}
	}
	inTensor, err := ort.NewTensor(ort.NewShape(1, 3, inputSide, inputSide), input)
	if err != nil {
		return nil, err
	}
	defer inTensor.Destroy()
	outTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1, inputSide, inputSide))
	if err != nil {
		return nil, err
	}
	defer outTensor.Destroy()
	if err := session.Run([]ort.Value{inTensor}, []ort.Value{outTensor}); err != nil {
		return nil, fmt.Errorf("分割推理失败: %w", err)
	}
	// Normalize the raw d0 map to 0–1 (rembg's min/max stretch).
	raw := outTensor.GetData()
	lo, hi := float32(1e9), float32(-1e9)
	for _, v := range raw {
		lo = min(lo, v)
		hi = max(hi, v)
	}
	span := max(hi-lo, 1e-6)
	mask := render.NewBitmap(b.W, b.H)
	levels := render.GuidedUpscale(raw, inputSide, inputSide, b.W, b.H)
	for i, v := range levels {
		g := uint8(min(255, max(0, (float64(v)-float64(lo))/float64(span)*255+0.5)))
		mask.Pix[i*4] = g
		mask.Pix[i*4+1] = g
		mask.Pix[i*4+2] = g
		mask.Pix[i*4+3] = 255
	}
	return mask, nil
}
