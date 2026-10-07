/* clipbridge.h — C side of the clipboard seam: GlobalAlloc/GlobalLock and
 * SetClipboardData are Win32 handle work that go vet's unsafeptr check
 * cannot express; doing it in C keeps the Go side to plain byte slices. */
#ifndef CLIPBRIDGE_H
#define CLIPBRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

/* Copies size bytes as a CF_DIB onto the clipboard. Returns 0 on success,
 * 1 when the clipboard could not be opened, 2 on allocation failure. */
int clip_put_dib(const unsigned char* data, unsigned long size);

#ifdef __cplusplus
}
#endif

#endif /* CLIPBRIDGE_H */
