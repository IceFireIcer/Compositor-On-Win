/* clipbridge.h — C side of the clipboard seam: GlobalAlloc/GlobalLock and
 * SetClipboardData/GetClipboardData are Win32 handle work that go vet's
 * unsafeptr check cannot express; doing it in C keeps the Go side to plain
 * byte slices. */
#ifndef CLIPBRIDGE_H
#define CLIPBRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

/* Copies size bytes as a CF_DIB onto the clipboard. Returns 0 on success,
 * 1 when the clipboard could not be opened, 2 on allocation failure. */
int clip_put_dib(const unsigned char* data, unsigned long size);

/* Reads one clipboard format into buf. name is a registered format name
 * ("PNG", "image/png") or NULL for CF_DIB. Returns 0 on success (*out_size =
 * bytes written), 1 when the clipboard could not be opened, 2 when that
 * format is not on the clipboard, 3 when cap is too small (*out_size = the
 * size needed, so the caller can retry with a big enough buffer). */
int clip_get_data(const char* name, unsigned char* buf, unsigned long cap, unsigned long* out_size);

/* The clipboard sequence number: it changes whenever anything writes to the
 * clipboard, which is what invalidates the app's own remembered payload
 * (NSPasteboard.changeCount's Windows counterpart). 0 when unreadable. */
unsigned long clip_sequence(void);

#ifdef __cplusplus
}
#endif

#endif /* CLIPBRIDGE_H */
