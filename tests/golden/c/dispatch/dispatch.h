// Minimal Windows stand-in for Apple's Grand Central Dispatch, used by
// DitherPixels.c (copied verbatim from the macOS tree). The harness runs
// small canvases, so a serial, in-order dispatch_apply is both correct and
// reproducible. Requires clang -fblocks (llvm-mingw ships the blocks runtime).
#ifndef GOLDEN_DISPATCH_SHIM_H
#define GOLDEN_DISPATCH_SHIM_H
#include <stddef.h>
#define DISPATCH_APPLY_AUTO ((void *)0)
void golden_dispatch_apply(size_t bands, void *queue, void (^block)(size_t));
// __VA_ARGS__ passes the queue argument and the block literal — commas inside
// the block body included — through verbatim.
#define dispatch_apply(bands, ...) golden_dispatch_apply((bands), __VA_ARGS__)
#endif
