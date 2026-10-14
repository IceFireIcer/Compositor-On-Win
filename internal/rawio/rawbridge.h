/* rawbridge.h — C ABI over LibRaw's C++ API (LGPL-2.1, linked from vcpkg).
 * One session per file: the frame stays unpacked in the instance so develop
 * re-renders cost no re-decode, exactly as the original cached its
 * CIRAWFilter between slider moves. */
#ifndef RAWBRIDGE_H
#define RAWBRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

typedef struct RB_Session RB_Session;

/* What this file's camera allows, read at open time (RawDevelopSettings'
 * supports* on the Go side). Lens correction is always 0: the original took
 * it from the lens profile Apple's RAW engine finds in the file, and LibRaw
 * has no lens-profile data at all — a registered parity gap, not a wire
 * left unplugged. */
typedef struct {
    int supports_noise_reduction;    /* LibRaw -n: imgdata.params.threshold */
    int supports_lens_correction;    /* always 0; see above */
    int supports_highlight_recovery; /* LibRaw -H: imgdata.params.highlight */
} RB_Caps;

/* Opens and unpacks a RAW file. halfSize halves both sides (draft mode for
 * the develop sheet's preview). camMul receives the camera's own white
 * balance (R, G, B) and caps what the camera allows. Returns LIBRAW_SUCCESS
 * (0) or the LibRaw error code. */
int rb_open(const char* path, int halfSize, RB_Session** out,
            int* width, int* height, double* camMul, RB_Caps* caps);

/* Re-renders the held frame with the given white balance and engine controls.
 * useCameraWb nonzero uses the camera's own multipliers; otherwise
 * mul4 = {R, G, B, G2} overrides. noiseReduction is 0–1 (LibRaw threshold,
 * dcraw -n's wavelet denoise, scaled to 0–1000); highlightRecovery is
 * LibRaw highlight, 0–9 (dcraw -H). Output is 16-bit interleaved linear RGB
 * (sRGB matrix, no tone curve — the Go develop pass spends exposure/boost).
 * Caller frees with rb_free. */
int rb_develop(RB_Session* s, const double* mul4, int useCameraWb,
               double noiseReduction, int highlightRecovery,
               unsigned short** rgb, int* width, int* height);

void rb_free(unsigned short* p);
void rb_close(RB_Session* s);

/* Last error text (thread-local), empty when none. */
const char* rb_last_error(void);

#ifdef __cplusplus
}
#endif

#endif /* RAWBRIDGE_H */
