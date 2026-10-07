// Dummy translation unit: its presence makes cgo drive the link with the
// C++ compiler, pulling in libc++/libunwind for libheif's static build.
extern "C" void heicio_cxx_link(void) {}
