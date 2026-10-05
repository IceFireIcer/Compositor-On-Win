package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"compositor-win/internal/domain"
)

// Package-level size limits (project-format.md "Limits"). Vars rather than
// consts so tests can shrink them; production code must not reassign.
var (
	MaxManifestBytes = int64(4 << 20)   // 4 MiB manifest
	MaxAssetBytes    = int64(512 << 20) // 512 MiB per encoded asset
)

const (
	MaxImagePixels = 100_000_000 // per layer image, in addition to masks
	MaxMaskPixels  = 100_000_000
)

// assetNameRe matches the strict on-disk asset spelling: an uppercase dashed
// UUID plus .png or .mask.png. Anything else is a path-traversal attempt.
var assetNameRe = regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}(\.mask)?\.png$`)

// Store reads and writes .comp packages on top of the domain model:
// manifest orchestration, atomic saves and the asset pipeline. Validation
// lives in validate.go; byte-level manifest encoding in manifest.go.
type Store struct{}

// Open reads a package: manifest → decode → validate → asset checks. The
// returned document is safe to hand to the session; pixel decoding stays
// with the importer (the store only validates headers and limits).
func (s *Store) Open(dir string) (*domain.Document, error) {
	data, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	doc, err := decodeManifest(data)
	if err != nil {
		return nil, err
	}
	if err := validateDocument(doc); err != nil {
		return nil, err
	}
	if err := checkAssets(dir, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// Save writes a package atomically enough for Windows: every new asset
// lands via temp-file rename first, then manifest.json is replaced last —
// a crash before that rename leaves the previous package intact. Assets
// no longer referenced by the document are removed after the manifest
// commits (the agent contract: PNGs first, manifest rename last).
// assets maps asset file names to complete PNG bytes.
func (s *Store) Save(dir string, doc *domain.Document, assets map[string][]byte) error {
	if err := validateDocument(doc); err != nil {
		return err
	}
	manifest, err := encodeManifest(doc)
	if err != nil {
		return err
	}
	if int64(len(manifest)) > MaxManifestBytes {
		return toLargef("manifest.json 为 %d 字节，超过 %d 上限", len(manifest), MaxManifestBytes)
	}

	referenced := map[string]bool{}
	for i := range doc.Layers {
		l := &doc.Layers[i]
		for _, name := range []string{deref(l.ImageFile), deref(l.MaskFile)} {
			if name == "" {
				continue
			}
			if !assetNameRe.MatchString(name) {
				return invalidf("图层 %s 的资产名 %q 不符合 <UUID>.png / <UUID>.mask.png", l.ID, name)
			}
			referenced[name] = true
		}
	}
	for name := range assets {
		if !referenced[name] {
			return invalidf("资产 %s 未被清单引用", name)
		}
	}

	imagesDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return encodef("无法创建 images 目录: %v", err)
	}
	for name, data := range assets {
		if int64(len(data)) > MaxAssetBytes {
			return toLargef("资产 %s 为 %d 字节，超过 %d 上限", name, len(data), MaxAssetBytes)
		}
		if err := checkPNG(name, data); err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(imagesDir, name), data); err != nil {
			return encodef("写入资产 %s 失败: %v", name, err)
		}
	}

	tmp := filepath.Join(dir, "manifest.json.tmp")
	if err := os.WriteFile(tmp, manifest, 0o644); err != nil {
		return encodef("写入 manifest 暂存文件失败: %v", err)
	}
	final := filepath.Join(dir, "manifest.json")
	// os.Rename refuses to replace an existing file on Windows, so the
	// commit is remove-then-rename; the watcher (internal/watch) re-arms
	// across exactly this rename.
	if err := os.Remove(final); err != nil && !errors.Is(err, fs.ErrNotExist) {
		_ = os.Remove(tmp)
		return encodef("移除旧 manifest 失败: %v", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return encodef("提交 manifest 失败: %v", err)
	}

	s.removeStaleAssets(imagesDir, referenced)
	return nil
}

// ReadAsset returns one asset's bytes after strict name and size checks.
func (s *Store) ReadAsset(dir, name string) ([]byte, error) {
	if !assetNameRe.MatchString(name) {
		return nil, invalidf("资产名 %q 不符合 <UUID>.png / <UUID>.mask.png", name)
	}
	data, err := os.ReadFile(filepath.Join(dir, "images", name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, missingImagef("资产 %s 不存在", name)
		}
		return nil, invalidf("读取资产 %s 失败: %v", name, err)
	}
	if int64(len(data)) > MaxAssetBytes {
		return nil, toLargef("资产 %s 为 %d 字节，超过 %d 上限", name, len(data), MaxAssetBytes)
	}
	return data, nil
}

func readManifest(dir string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, invalidf("目录不是 .comp 包：manifest.json 不存在")
		}
		return nil, invalidf("读取 manifest.json 失败: %v", err)
	}
	if int64(len(data)) > MaxManifestBytes {
		return nil, toLargef("manifest.json 为 %d 字节，超过 %d 上限", len(data), MaxManifestBytes)
	}
	return data, nil
}

// checkAssets verifies every referenced asset exists on disk, is a regular
// file (no symlinks — the unsafe-path rule), fits the byte and pixel limits,
// and that masks are 8-bit grayscale.
func checkAssets(dir string, doc *domain.Document) error {
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if l.ImageFile != nil {
			if err := checkAssetFile(dir, *l.ImageFile, MaxImagePixels, false); err != nil {
				return err
			}
		}
		if l.MaskFile != nil {
			if err := checkAssetFile(dir, *l.MaskFile, MaxMaskPixels, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkAssetFile(dir, name string, maxPixels int64, isMask bool) error {
	if !assetNameRe.MatchString(name) {
		return invalidf("资产名 %q 不符合 <UUID>.png / <UUID>.mask.png", name)
	}
	path := filepath.Join(dir, "images", name)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return missingImagef("清单引用的资产 %s 不存在", name)
		}
		return invalidf("检查资产 %s 失败: %v", name, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return invalidf("资产 %s 不是常规文件（拒绝符号链接等不安全路径）", name)
	}
	if info.Size() > MaxAssetBytes {
		return toLargef("资产 %s 为 %d 字节，超过 %d 上限", name, info.Size(), MaxAssetBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return invalidf("读取资产 %s 失败: %v", name, err)
	}
	return checkPNG(name, data)
}

// checkPNG validates a PNG payload against the asset rules: header must
// parse, pixel count fits the per-asset budget, and masks must be 8-bit
// grayscale without alpha.
func checkPNG(name string, data []byte) error {
	h, err := parsePNGHeader(data)
	if err != nil {
		return missingImagef("资产 %s 不是可解析的 PNG: %v", name, err)
	}
	limit := int64(MaxImagePixels)
	if isMaskAsset(name) {
		limit = int64(MaxMaskPixels)
		if !isGrayscale8(h) {
			return invalidf("蒙版 %s 必须是 8 位灰度 PNG", name)
		}
	}
	if int64(h.Width)*int64(h.Height) > limit {
		return toLargef("资产 %s 为 %d × %d 像素，超过 %d 上限", name, h.Width, h.Height, limit)
	}
	return nil
}

func isMaskAsset(name string) bool {
	return filepath.Ext(name[:len(name)-len(".png")]) == ".mask"
}

func (s *Store) removeStaleAssets(imagesDir string, referenced map[string]bool) {
	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !assetNameRe.MatchString(name) {
			continue // temp files and anything foreign are left alone
		}
		if !referenced[name] {
			_ = os.Remove(filepath.Join(imagesDir, name))
		}
	}
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
