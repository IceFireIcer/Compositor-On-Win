#include "dispatch/dispatch.h"
void golden_dispatch_apply(size_t bands, void *queue, void (^block)(size_t)) {
    (void)queue; // serial stand-in: the only queue behavior we need is "runs".
    for (size_t i = 0; i < bands; i++) block(i);
}

// A 20-line blocks runtime for the serial-only path above: clang -fblocks
// emits references to these symbols, but on this serial shim the block is
// always invoked on its own stack frame, so the runtime bookkeeping they
// would perform never matters. The dllimport indirection pointers are
// provided alongside the definitions because the blocks ABI on Windows
// targets references them through __imp_.
void *_NSConcreteStackBlock[32];
void *__imp__NSConcreteStackBlock = _NSConcreteStackBlock;
void *_NSConcreteGlobalBlock[32];
void *__imp__NSConcreteGlobalBlock = _NSConcreteGlobalBlock;
void golden_Block_object_assign(void *dst, const void *src, long flags) { (void)dst; (void)src; (void)flags; }
void golden_Block_object_dispose(const void *src, long flags) { (void)src; (void)flags; }
void *__imp__Block_object_assign = golden_Block_object_assign;
void *__imp__Block_object_dispose = golden_Block_object_dispose;
