package project

import (
	"errors"
	"fmt"

	"compositor-win/internal/domain"
)

// Errors ported from the macOS ProjectError cases; rejection classes match
// the original one-to-one (messages localized, following internal/domain).
// Callers test with errors.Is / errors.As; wrapping adds the offending
// file or layer so the message stays actionable.
var (
	// ErrInvalid mirrors ProjectError.invalid.
	ErrInvalid = errors.New("这不是有效的 Compositor 项目，或其元数据已损坏")
	// ErrMissingImage mirrors ProjectError.missingImage.
	ErrMissingImage = errors.New("项目内的图像缺失或已损坏。当前文档未被替换")
	// ErrTooLarge mirrors ProjectError.tooLarge.
	ErrTooLarge = errors.New("项目超出画布、图层、文件大小或 2 亿像素的文档上限")
	// ErrEncode mirrors ProjectError.encode.
	ErrEncode = errors.New("图像无法保存。先前的项目未被替换")
)

// VersionError mirrors ProjectError.version.
type VersionError struct{ Version int }

func (e *VersionError) Error() string {
	return fmt.Sprintf("项目使用格式版本 %d。本应用支持版本 1–%d。", e.Version, domain.FormatVersion)
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func toLargef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTooLarge, fmt.Sprintf(format, args...))
}

func missingImagef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMissingImage, fmt.Sprintf(format, args...))
}

func encodef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrEncode, fmt.Sprintf(format, args...))
}
