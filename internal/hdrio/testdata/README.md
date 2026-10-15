# testdata

Committed fixtures for `internal/hdrio`. Every file here was generated, not
copied from a third party, and this file records how.

## What is here

| File | What it is |
| --- | --- |
| `ffmpeg-input-gbrpf32le.raw` | The 4 × 4 input frame, 192 bytes, raw planar float32 (G, B, R). Every fixture below was made from this one frame. |
| `ffmpeg-half-none.exr` | ffmpeg-written OpenEXR, HALF channels, compression NONE. |
| `ffmpeg-half-rle.exr` | ffmpeg-written OpenEXR, HALF channels, compression RLE. |
| `ffmpeg-half-zips.exr` | ffmpeg-written OpenEXR, HALF channels, compression ZIP1 (ZIPS). |
| `ffmpeg-float-zip16.exr` | ffmpeg-written OpenEXR, FLOAT channels, compression ZIP16 (ZIP). This is the only fixture with more than one scanline in a chunk, so it is the one that pins the block layout. |
| `ffmpeg-linear.hdr` | ffmpeg-written Radiance HDR, flat RGBE (no RLE). |

The input frame's G plane is the ramp

    0, 0.25, 0.5, 1, 4, 0.1, 0.75, 0.0031308, 0.04045, 2.5, 0.001, 0.999, 0.125, 0.333, 0.666, 1.5

with B at half and R at twice those values. It spans zero, exact halves, the
sRGB curve's knee at 0.0031308/0.04045, values a half cannot hold exactly, and
light brighter than white. `internal/hdrio/ffmpeg_test.go` rebuilds the same
frame in `ffmpegInput()`, so the live tests need no fixture file.

## ffmpeg

    ffmpeg version 9.0.2-full_build-www.gyan.dev Copyright (c) 2000-2026 the FFmpeg developers
    built with gcc 16.2.0 (Rev3, Built by MSYS2 project)
    configuration: --enable-gpl --enable-version3 --enable-static ... --enable-zlib ...

    C:\Users\IceFire_Icer\AppData\Local\Microsoft\WinGet\Packages\Gyan.FFmpeg_Microsoft.Winget.Source_8wekyb3d8bbwe\ffmpeg-9.0.2-full_build\bin\ffmpeg.exe

## The exact commands

Run from `internal/hdrio`, with `$ff` set to the ffmpeg path above. The
input file is written by the first command's comment (see the Go below):

    # ffmpeg-input-gbrpf32le.raw: 4 × 4, planar float32, plane order G, B, R
    & $ff -y -f rawvideo -pix_fmt gbrpf32le -s 4x4 -i ffmpeg-input-gbrpf32le.raw -c:v exr -format half  -compression none  -gamma 1 testdata/ffmpeg-half-none.exr
    & $ff -y -f rawvideo -pix_fmt gbrpf32le -s 4x4 -i ffmpeg-input-gbrpf32le.raw -c:v exr -format half  -compression rle   -gamma 1 testdata/ffmpeg-half-rle.exr
    & $ff -y -f rawvideo -pix_fmt gbrpf32le -s 4x4 -i ffmpeg-input-gbrpf32le.raw -c:v exr -format half  -compression zip1  -gamma 1 testdata/ffmpeg-half-zips.exr
    & $ff -y -f rawvideo -pix_fmt gbrpf32le -s 4x4 -i ffmpeg-input-gbrpf32le.raw -c:v exr -format float -compression zip16 -gamma 1 testdata/ffmpeg-float-zip16.exr
    & $ff -y -f rawvideo -pix_fmt gbrpf32le -s 4x4 -i ffmpeg-input-gbrpf32le.raw -c:v hdr testdata/ffmpeg-linear.hdr

The raw input was written by this program, run once with Node:

    const ramp = [0, 0.25, 0.5, 1, 4, 0.1, 0.75, 0.0031308, 0.04045, 2.5,
                  0.001, 0.999, 0.125, 0.333, 0.666, 1.5];
    const buf = Buffer.alloc(4 * 4 * 3 * 4);
    let o = 0;
    for (const plane of [ramp, ramp.map(v => v * 0.5), ramp.map(v => v * 2)]) {
      for (const v of plane) { buf.writeFloatLE(v, o); o += 4; }
    }
    require("fs").writeFileSync("ffmpeg-input-gbrpf32le.raw", buf);

## What the tests assume about ffmpeg

Two ffmpeg behaviors are load-bearing, and both were established by dumping
bytes rather than by trusting documentation:

1. **Its EXR encoder stores the frame's linear floats verbatim.** With
   `-gamma 1` — which ffmpeg also writes into the file's `gamma` attribute —
   no transfer function is applied, and the frames are handed over as raw
   float32, so no 8-bit source is in the way either. Dumping the half bit
   patterns in `ffmpeg-half-none.exr` shows exactly the input values.
2. **Its float-to-half conversion truncates rather than rounds.** The
   mantissa is cut, not rounded to nearest even: 0.002 is stored as 0x1818
   where IEEE 754 rounding gives 0x1819, and 0.333 as 0x3553 where rounding
   gives 0x3554. `ffmpegHalf()` in `ffmpeg_test.go` models this, which is what
   lets those expectations be exact.

Two further notes, both measured:

* ffmpeg's EXR decoder outputs half floats natively (`gbrpf16le`, or
  `gbrapf16le` when the file has an alpha channel). Asking it to convert the
  frame to float instead routes the samples through a 16-bit unsigned
  intermediate that clamps anything above white and quantizes the rest — 0.25
  comes back as 0x4004/65535 — so `TestFFmpegDecodesOurEXR` reads the file
  back in the decoder's own pixel format.
* ffmpeg's HDR encoder writes no `EXPOSURE` variable, so nothing in the
  Radiance header is being assumed away.
