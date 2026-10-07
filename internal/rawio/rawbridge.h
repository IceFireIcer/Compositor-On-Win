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

/* Opens and unpacks a RAW file. halfSize halves both sides (draft mode for
 * the develop sheet's preview). camMul receives the camera's own white
 * balance (R, G, B). Returns LIBRAW_SUCCESS (0) or the LibRaw error code. */
int rb_open(const char* path, int halfSize, RB_Session** out,
            int* width, int* height, double* camMul);

/* Re-renders the held frame with the given white balance. useCameraWb
 * nonzero uses the camera's own multipliers; otherwise mul4 = {R, G, B, G2}
 * overrides. Output is 16-bit interleaved linear RGB (sRGB matrix, no tone
 * curve — the Go develop pass spends exposure/boost). Caller frees with
 * rb_free. */
int rb_develop(RB_Session* s, const double* mul4, int useCameraWb,
               unsigned short** rgb, int* width, int* height);

void rb_free(unsigned short* p);
void rb_close(RB_Session* s);

/* Last error text (thread-local), empty when none. */
const char* rb_last_error(void);

#ifdef __cplusplus
}
#endif

#endif /* RAWBRIDGE_H */
