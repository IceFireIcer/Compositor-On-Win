package bridge

import (
	"net/url"
	"strings"
)

// safeRenderSegment keeps an encoded path segment from being interpreted as
// another path or as a dot segment before it reaches the workspace lookup.
// Document and layer IDs are UUIDs in validated projects, but this guard also
// protects the HTTP boundary when a malformed client constructs the URL.
func safeRenderSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." || strings.ContainsAny(segment, "/\\") {
		return false
	}
	decoded, err := url.PathUnescape(segment)
	if err != nil || decoded != segment || strings.ContainsAny(decoded, "/\\") {
		return false
	}
	return decoded != "." && decoded != ".."
}
