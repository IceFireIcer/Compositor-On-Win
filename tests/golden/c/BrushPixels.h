// Copied verbatim from reference/Swift/Compositor/Rendering/BrushPixels.h
// (865e4675d09de64c3e4ad90aeacee8997542f4ae), unchanged at the current pin
// e8c9e29701528413cc0e608d61187c1c61790add — the revision this copy was
// verified against byte for byte. reference/ stays read-only (AGENTS.md
// invariant #4: kernels travel as copies that name their source).
#ifndef BrushPixels_h
#define BrushPixels_h
#include <stdint.h>
#include <stddef.h>
// Half-open bounds of nonzero alpha in premultiplied RGBA. Empty returns all zero.
void brush_alpha_bounds(const uint8_t *bytes, size_t width, size_t height, size_t stride, size_t bounds[4]);
void layer_extract_alpha(const uint8_t *rgba, size_t rgbaStride, uint8_t *gray, size_t grayStride, size_t width, size_t height);
void layer_unpremultiply_opaque(uint8_t *rgba, size_t stride, size_t width, size_t height);
void layer_restore_alpha(uint8_t *rgba, size_t stride, const uint8_t *alpha, size_t alphaStride, size_t width, size_t height);
#endif
