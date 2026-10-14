// Verbatim copy of reference/Swift/Compositor/Rendering/PatchPixels.h
// (ticket 65, multi-scale PatchMatch content-aware fill). Source commit
// e8c9e29701528413cc0e608d61187c1c61790add (reference/Swift submodule,
// v1.5.1-8-ge8c9e29), byte-for-byte apart from this note; see README.md
// (AGENTS.md invariant #4). Unlike the nine kernels copied at 865e467,
// this file is from the 1.4.10 rewrite that replaced ContentFill.c with
// PatchPixels.c; ContentFill.{c,h} stay in this directory as the
// historical baseline.
#ifndef PatchPixels_h
#define PatchPixels_h
#include <stdint.h>
#include <stddef.h>
// Fills a hole in premultiplied RGBA (4 bytes per pixel, `stride` bytes per row) from the rest of the image, the way
// Content-Aware Fill works: every small patch over the hole is matched to the patch of the image it most resembles
// (PatchMatch), the overlapping matches are blended into each pixel, and that's repeated until it settles, first on
// a small copy for the hole's overall structure and then at each larger size for its detail.
//   `hole` (width * height bytes, `holeStride` per row): nonzero marks a pixel to fill.
// Opaque pixels outside the hole are what's matched and copied; transparent ones outside it are left alone and
// ignored. Returns 1 when the hole is filled, 0 when there's no image to fill it from, -1 when memory runs out.
int patch_fill(uint8_t *rgba, size_t stride, const uint8_t *hole, size_t holeStride, int width, int height, uint32_t seed);
#endif
