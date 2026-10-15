package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/fsatomic"
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
// UUID plus .png, .mask.png, or — from version 13, for an HDR layer — .exr
// (ADR-0007). Anything else is a path-traversal attempt. A mask is never an
// EXR; checkAsset rejects that combination.
var assetNameRe = regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}(\.mask)?\.(png|exr)$`)

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
	if err := checkAssets(dir, doc, doc.Version); err != nil {
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
//
// The written manifest declares the LOWEST format version the document's
// contents require (ADR-0007 §4), never simply domain.FormatVersion: a
// document that uses nothing newer than version 11 stays readable by macOS
// builds that only understand 11, which is the interop the product is for.
// Upstream instead always writes ProjectManifest.current — it builds a fresh
// ProjectManifest on every save (ProjectStore.swift:15,21 +
// EditorSession+Projects.swift:15), so the version in an opened file is only
// a read gate there. This deviation is deliberate and recorded in ADR-0007.
//
// Stamping matters beyond tidiness: validate.go's gates are written against
// the declared version, so a document opened from a v9/v10 package that
// gained a newer field (colorRuns/fontRuns from ticket 45, guides, …) could
// never be saved again if it kept the version it was loaded with. Open()
// still validates with the version the file declares, so the gates keep
// doing their job on the way in.
//
// The caller's document keeps the version it was loaded with: Version records
// what the file on disk declared, and nothing decides behaviour from it at run
// time (the gates read it at Open, and Save stamps the copy it writes).
func (s *Store) Save(dir string, doc *domain.Document, assets map[string][]byte) error {
	stamped := *doc
	stamped.Version = requiredVersion(doc, assets)
	if err := validateDocument(&stamped); err != nil {
		return err
	}
	manifest, err := encodeManifest(&stamped)
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
		if err := checkAsset(name, data, stamped.Version); err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(imagesDir, name), data); err != nil {
			return encodef("写入资产 %s 失败: %v", name, err)
		}
	}

	// The commit: manifest.json.tmp, then one rename over manifest.json. On
	// Windows a rename replaces an existing target (MOVEFILE_REPLACE_EXISTING),
	// so no remove-first step is needed — and remove-first would lose the
	// manifest outright if the rename then failed against a locked file. See
	// internal/fsatomic for the measurements and the small retry.
	final := filepath.Join(dir, "manifest.json")
	if err := fsatomic.WriteFile(final, manifest, 0o644); err != nil {
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
// and matches the pixel rules for the version the manifest declares.
func checkAssets(dir string, doc *domain.Document, version int) error {
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if l.ImageFile != nil {
			if err := checkAssetFile(dir, *l.ImageFile, MaxImagePixels, false, version); err != nil {
				return err
			}
		}
		if l.MaskFile != nil {
			if err := checkAssetFile(dir, *l.MaskFile, MaxMaskPixels, true, version); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkAssetFile(dir, name string, maxPixels int64, isMask bool, version int) error {
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
	return checkAsset(name, data, version)
}

// checkAsset validates one asset payload against the package rules for the
// version the manifest declares: the header must parse, the pixel count must
// fit the per-asset budget, a mask must be 8-bit grayscale without alpha, a
// 16-bit image needs version 12 and an EXR image version 13 (ADR-0007 §5).
// Only the header is read — decoding is the importer's job, exactly as for
// PNG (ticket 05).
func checkAsset(name string, data []byte, version int) error {
	if isEXRAsset(name) {
		return checkEXR(name, data, version)
	}
	return checkPNG(name, data, version)
}

// isEXRAsset reports whether an asset is an HDR layer's EXR rather than a
// PNG. Masks are always PNG, so a `.mask.exr` name is rejected below.
func isEXRAsset(name string) bool { return strings.HasSuffix(name, ".exr") }

func checkEXR(name string, data []byte, version int) error {
	if isMaskAsset(name) {
		return invalidf("蒙版 %s 必须是 8 位灰度 PNG，不能是 EXR", name)
	}
	// HDR/EXR layer images arrived in version 13.
	if version < domain.VersionHDR {
		return missingImagef("EXR 资产 %s 需要版本 %d 以上，得到 %d", name, domain.VersionHDR, version)
	}
	h, err := parseEXRHeader(data)
	if err != nil {
		return missingImagef("资产 %s 不是可解析的 EXR: %v", name, err)
	}
	if int64(h.Width)*int64(h.Height) > int64(MaxImagePixels) {
		return toLargef("资产 %s 为 %d × %d 像素，超过 %d 上限", name, h.Width, h.Height, MaxImagePixels)
	}
	return nil
}

// checkPNG validates a PNG payload against the asset rules: header must
// parse, pixel count fits the per-asset budget, masks must be 8-bit grayscale
// without alpha, and a 16-bit image needs version 12 (ADR-0007). Other bit
// depths stay accepted, as upstream's `depth <= 8` check accepts them.
func checkPNG(name string, data []byte, version int) error {
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
	} else if h.BitDepth == 16 && version < domain.Version16Bit {
		// Upstream throws ProjectError.missingImage here, not .invalid: the
		// asset cannot be read under this version, which is what the class
		// means to a caller.
		return missingImagef("16 位资产 %s 需要版本 %d 以上，得到 %d", name, domain.Version16Bit, version)
	}
	if int64(h.Width)*int64(h.Height) > limit {
		return toLargef("资产 %s 为 %d × %d 像素，超过 %d 上限", name, h.Width, h.Height, limit)
	}
	return nil
}

func isMaskAsset(name string) bool {
	return strings.HasSuffix(name, ".mask.png") || strings.HasSuffix(name, ".mask.exr")
}

// requiredVersion is the lowest format version a document's contents need,
// which is what Save declares (ADR-0007 §4). Every feature up to version 11
// is covered by the base; an EXR image raises it to 13 and a 16-bit PNG image
// to 12. Masks never raise it — they are 8-bit at every version.
func requiredVersion(doc *domain.Document, assets map[string][]byte) int {
	version := domain.VersionBase
	for i := range doc.Layers {
		name := deref(doc.Layers[i].ImageFile)
		if name == "" {
			continue
		}
		if isEXRAsset(name) {
			return domain.VersionHDR
		}
		if data, ok := assets[name]; ok {
			if h, err := parsePNGHeader(data); err == nil && h.BitDepth == 16 && version < domain.Version16Bit {
				version = domain.Version16Bit
			}
		}
	}
	return version
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

// writeFileAtomic writes one asset through internal/fsatomic: temp file in the
// same directory, then a single rename (with a short retry for the transient
// locks Windows writers run into).
func writeFileAtomic(path string, data []byte) error {
	return fsatomic.WriteFile(path, data, 0o644)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
