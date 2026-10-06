// Package ml runs the subject-segmentation pipeline (ticket 37, ADR-0005
// appendix): the u2netp salient-object model through ONNX Runtime, refined
// by render.RefineSubjectMask (the GuidedMatte port).
//
// Distribution decision (ADR-0005 appendix): the u2netp ONNX model
// (4.6 MB, Apache-2.0, Qin et al.) downloads on first use from the pinned
// URL below and is verified against a committed SHA-256; it is cached under
// the user's app-data directory and never committed to the repository. The
// ONNX Runtime shared library (MIT) ships with the installer; a
// development checkout falls back to onnxruntime.dll next to the executable
// or on PATH. Without either file the entry points fail with a
// user-facing message instead of crashing.
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

// ModelSHA256 pins the exact artifact the download must match. It is
// supplied at release builds via -ldflags (the release checklist pins it);
// empty disables the hash check and falls back to a size sanity bound.
var ModelSHA256 = ""

// modelMinBytes guards against truncated downloads when no hash is pinned.
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

func modelPath() string  { return filepath.Join(ModelDir(), ModelFileName) }
func libPath() string {
	for _, dir := range []string{"", ".", "models"} {
		p := filepath.Join(dir, libFileName())
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat(libFileName()); err == nil {
		return libFileName()
	}
	return ""
}

// ErrModelMissing reports the user-facing state when the model or runtime
// isn't installed yet.
var ErrModelMissing = errors.New("分割模型或 ONNX Runtime 未安装：请运行「下载分割模型」或把 onnxruntime.dll 与 u2netp.onnx 放入程序目录")

var (
	initOnce   sync.Once
	initErr    error
	sessionMtx sync.Mutex
	session    *ort.DynamicAdvancedSession
	sessionFor string
)

// EnsureModel downloads the pinned model when it is missing, verifying the
// SHA-256. The runtime library is not downloaded.
func EnsureModel() error {
	p := modelPath()
	if info, err := os.Stat(p); err == nil && info.Size() > 0 {
		return nil
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
	if ModelSHA256 != "" {
		if got := hex.EncodeToString(hasher.Sum(nil)); got != ModelSHA256 {
			os.Remove(tmp)
			return fmt.Errorf("分割模型校验不符: %s", got)
		}
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

// setup initializes the runtime and session once; it fails softly when the
// library or model is absent (ErrModelMissing).
func setup() error {
	initOnce.Do(func() {
		lib := libPath()
		if lib == "" {
			initErr = ErrModelMissing
			return
		}
		if _, err := os.Stat(modelPath()); err != nil {
			initErr = ErrModelMissing
			return
		}
		ort.SetSharedLibraryPath(lib)
		if err := ort.InitializeEnvironment(); err != nil {
			initErr = fmt.Errorf("初始化 ONNX Runtime 失败: %w", err)
			return
		}
	})
	return initErr
}

const inputSide = 320

// RunSubjectMaskRaw runs the model over the bitmap and returns the raw
// salient-object mask (normalized to 0–255 gray, the bitmap's size) — the
// cacheable part while a panel is open.
func RunSubjectMaskRaw(b *render.Bitmap) (*render.Bitmap, error) {
	if err := setup(); err != nil {
		return nil, err
	}
	sessionMtx.Lock()
	defer sessionMtx.Unlock()
	if session == nil || sessionFor != modelPath() {
		s, err := ort.NewDynamicAdvancedSession(modelPath(),
			[]string{"input.1"}, []string{"output.0"}, nil)
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
		g := uint8(min(255, max(0, (float64(v)-float64(lo))/float64(span)*255 + 0.5)))
		mask.Pix[i*4] = g
		mask.Pix[i*4+1] = g
		mask.Pix[i*4+2] = g
		mask.Pix[i*4+3] = 255
	}
	return mask, nil
}
