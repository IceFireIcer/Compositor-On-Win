// Copied verbatim from reference/Swift/Compositor/Rendering/LevelsPixels.h
// (865e4675d09de64c3e4ad90aeacee8997542f4ae), unchanged at the current pin
// e8c9e29701528413cc0e608d61187c1c61790add — the revision this copy was
// verified against byte for byte. reference/ stays read-only (AGENTS.md
// invariant #4: kernels travel as copies that name their source).
#include <stdint.h>
#include <stddef.h>
void levels_apply(uint8_t *pixels, size_t count, const float *tables);
void levels_histogram(const uint8_t *pixels, const uint8_t *coverage, size_t count, double *bins);
void cube_apply(uint8_t *pixels, size_t count, const float *cube, int dimension);
