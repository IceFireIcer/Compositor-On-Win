package bridge

// RAW develop flow (ticket 41): the develop sheet opens a half-size LibRaw
// session once, re-develops per slider move from the held frame, and the
// import commits a full-size render — RawImporter.Queue's cached-filter
// semantics. HEIC decodes straight into the import batch (see
// BeginImageImport).

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"compositor-win/internal/domain"
	"compositor-win/internal/rawio"
	"compositor-win/internal/render"
)

// rawExtensions is the develop-sheet trigger set: every camera RAW the
// average user meets, matched by extension like RawImporter.matches did
// via UTType.rawImage conformance.
var rawExtensions = map[string]bool{
	".cr2": true, ".cr3": true, ".crw": true, ".nef": true, ".nrw": true,
	".arw": true, ".srf": true, ".sr2": true, ".dng": true, ".raf": true,
	".orf": true, ".rw2": true, ".pef": true, ".srw": true, ".x3f": true,
	".3fr": true, ".fff": true, ".iiq": true, ".kdc": true, ".dcr": true,
	".mef": true, ".mos": true, ".mrw": true, ".ptx": true, ".rdc": true,
}

// heicExtensions decode straight into the batch.
var heicExtensions = map[string]bool{
	".heic": true, ".heif": true, ".hif": true,
}

// IsRAWPath reports whether the extension routes through the develop sheet.
func IsRAWPath(path string) bool {
	return rawExtensions[strings.ToLower(filepathExt(path))]
}

// IsHEICPath reports whether the extension decodes via libheif.
func IsHEICPath(path string) bool {
	return heicExtensions[strings.ToLower(filepathExt(path))]
}

func filepathExt(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i:]
	}
	return ""
}

// rawDevelopReply is RawBeginDevelop's reply: the half-size session's
// working size, the FULL size (for limit checks) and the asShot defaults.
type rawDevelopReply struct {
	Name    string                `json:"name"`
	FullW   int                   `json:"fullWidth"`
	FullH   int                   `json:"fullHeight"`
	Preview bool                  `json:"preview"`
	AsShot  rawio.DevelopSettings `json:"asShot"`
}

// rawDevelopSession is the open develop sheet's state.
type rawDevelopSession struct {
	mu      sync.Mutex
	session *rawio.Session
	name    string
	half    bool
}

// RawBeginDevelop opens the half-size session and reads the camera's own
// white balance (asShot). One sheet at a time; a second open replaces the
// first (the frontend runs the sheet per raw file sequentially).
func (s *Service) RawBeginDevelop(path string) (string, error) {
	sess, err := rawio.Open(path, true)
	if err != nil {
		return "", err
	}
	s.rawMu.Lock()
	defer s.rawMu.Unlock()
	if s.rawSession != nil {
		s.rawSession.session.Close()
	}
	s.rawSession = &rawDevelopSession{session: sess, name: baseDisplayName(path), half: true}
	asShot := rawio.CameraAsShot(sess.CamMul[0], sess.CamMul[1], sess.CamMul[2])
	reply := rawDevelopReply{
		Name:    s.rawSession.name,
		FullW:   sess.Width,
		FullH:   sess.Height,
		Preview: true,
		AsShot:  asShot,
	}
	b, err := json.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("无法编码 RAW 信息: %w", err)
	}
	return string(b), nil
}

// rawPreviewReply carries the developed preview: JPEG bytes (quality 90)
// and the frame size the sheet displays.
type rawPreviewReply struct {
	JPEG   string `json:"jpeg"`
	Size   int    `json:"size"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

const rawPreviewLimit = 2048

// RawDevelopPreview re-develops the held half-size frame with the given
// settings and returns a JPEG preview. The develop is seconds of work on
// full frames — the sheet's preview runs on the half-size session, exactly
// the original's draft-mode CIRAWFilter.
func (s *Service) RawDevelopPreview(settingsJSON string) (string, error) {
	s.rawMu.Lock()
	defer s.rawMu.Unlock()
	if s.rawSession == nil {
		return "", fmt.Errorf("没有进行中的 RAW 显影")
	}
	settings, err := parseDevelopSettings(settingsJSON)
	if err != nil {
		return "", err
	}
	pix, w, h, err := developFromSession(s.rawSession.session, settings)
	if err != nil {
		return "", err
	}
	bmp := render.NewBitmap(w, h)
	copy(bmp.Pix, pix)
	preview := bmp
	if longest := max(w, h); longest > rawPreviewLimit {
		scale := float64(rawPreviewLimit) / float64(longest)
		preview = render.DownscaleBitmap(bmp, int(float64(w)*scale+0.5), int(float64(h)*scale+0.5))
	}
	data, err := render.EncodeJPEGWithDPI(preview, 90, 0, 255, 255, 255)
	if err != nil {
		return "", fmt.Errorf("预览编码失败: %w", err)
	}
	reply := rawPreviewReply{
		JPEG:   base64.StdEncoding.EncodeToString(data),
		Size:   len(data),
		Width:  preview.W,
		Height: preview.H,
	}
	b, err := json.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("无法编码 RAW 预览: %w", err)
	}
	return string(b), nil
}

// RawFinishDevelop re-opens the file at full size, develops with the
// confirmed settings and lands the frame as a layer (or a new document
// when none is open) — one 导入图像 history entry via the shared commit.
func (s *Service) RawFinishDevelop(path string, settingsJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	settings, err := parseDevelopSettings(settingsJSON)
	if err != nil {
		return "", err
	}
	full, err := rawio.Open(path, false)
	if err != nil {
		return "", err
	}
	defer full.Close()
	if full.Width > domain.MaxSide || full.Height > domain.MaxSide ||
		full.Width*full.Height > domain.MaxSurfacePixels {
		return "", fmt.Errorf("导入超过当前 %.0f 百万像素文档预算或 %d 像素边长限制",
			float64(domain.MaxSurfacePixels)/1e6, domain.MaxSide)
	}
	pix, w, h, err := developFromSession(full, settings)
	if err != nil {
		return "", err
	}
	bmp := render.NewBitmap(w, h)
	copy(bmp.Pix, pix)
	return s.ws.CommitImportedLayers([]pendingImportFile{{name: baseDisplayName(path), bmp: bmp}})
}

// RawCancelDevelop releases the half-size session (sheet closed).
func (s *Service) RawCancelDevelop() {
	s.rawMu.Lock()
	defer s.rawMu.Unlock()
	if s.rawSession != nil {
		s.rawSession.session.Close()
		s.rawSession = nil
	}
}

func parseDevelopSettings(raw string) (rawio.DevelopSettings, error) {
	var settings rawio.DevelopSettings
	if strings.TrimSpace(raw) == "" {
		return settings, fmt.Errorf("缺少显影参数")
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return settings, fmt.Errorf("无法解析显影参数: %w", err)
	}
	return settings, nil
}

// developFromSession runs one develop pass: LibRaw spends the white
// balance (camera's own when asShot), ApplyDevelop spends exposure,
// further Kelvin/tint and boost.
func developFromSession(sess *rawio.Session, settings rawio.DevelopSettings) ([]uint8, int, int, error) {
	pix, w, h, err := sess.Develop(settings)
	if err != nil {
		return nil, 0, 0, err
	}
	return pix, w, h, nil
}

func baseDisplayName(path string) string {
	p := path
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		p = p[i+1:]
	}
	if i := strings.LastIndexByte(p, '.'); i >= 0 {
		p = p[:i]
	}
	return p
}
