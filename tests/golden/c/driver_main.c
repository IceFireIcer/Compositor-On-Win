// golden_driver — the tiny C driver behind the golden-benchmark harness
// (docs/testing.md §2). It applies one kernel entry to a raw premultiplied
// RGBA file using numeric parameters from argv, so no JSON parsing or image
// library is needed in C: the Go generator (tests/golden/gen) reads the case
// JSON, writes the input raw, invokes this driver, and wraps the output raw
// into a PNG.
//
// Usage:
//   golden_driver <command> <in.raw> <out.raw> <width> <height> [params...]
//
// Commands (one per kernel file, parameter order documented in gen/main.go):
//   levels        4×5 range floats (black gamma white outBlack outWhite) × RGB,red,green,blue
//   exposure      exposure offset gamma
//   gradient_map  shadowR shadowG shadowB lightR lightG lightB reversed
//   cube          — (synthetic polynomial cube, mirrored in Go tests)
//   grain         amount size roughness seed originX originY unitsPerPixel
//   black_white   r y g c b m tint tintHue tintSaturation
//   color_balance 9 shifts then preserveLuminosity
//   camera_raw_curve  4 point curves (count + x y each), 7 parametric sliders,
//                 refineSaturation, 24 mixer floats, pointCount + 9 floats per
//                 point, 12 grade floats, blending, balance, visualize
//   camera_raw_effects  texture clarity dehaze glow glowStyle glowRange
//                 glowSpread glowWarmth vignetteAmount vignetteMidpoint
//                 vignetteRoundness vignetteFeather vignetteHighlights
//                 vignetteStyle scale
//   camera_raw_detail  sharpenAmount sharpenRadius sharpenDetail sharpenMasking
//                 noiseLuminance noiseLuminanceDetail noiseLuminanceContrast
//                 noiseColor noiseColorDetail noiseColorSmoothness scale
//   camera_raw_optics  removeChromatic lensProfile profileDistortion
//                 profileVignetting distortionK purpleAmount purpleHueLow
//                 purpleHueHigh greenAmount greenHueLow greenHueHigh
//                 vignetteAmount vignetteMidpoint scale
//   camera_raw_calibration  shadowTint redHue redSaturation greenHue
//                 greenSaturation blueHue blueSaturation processVersion
//   colored_vignette  frameX frameY frameWidth frameHeight fillsClear amount
//                 midpoint roundness feather highlights red green blue
//   tonal_contrast  blurRadius amount shadows midtones highlights
//                 (the blurred base is built here with box_blur_rgba,
//                 mirrored in golden_test.go's buildTonalBase)
//   patch_fill    maskPath seed
//   quantize16    — (the 8-bit input widened by 257, the exact 8→16 scale)
//   dither        style levels diffusion density contrast cell angle lightOnDark
//                 originalColors darkR darkG darkB lightR lightG lightB dots wobble
//   scanlines     spacing thickness dots wobble displace threshold split density
//                 contrast blackLevel smoothness originalColors
//                 darkR darkG darkB lightR lightG lightB
//   heal          maskPath opacity mode seed
//   lens          k
//   noise         amount gaussian monochromatic seed
//   wand          seedX seedY radius tolerance contiguous
//   color_range   includeCount (r g b)×n excludeCount (r g b)×m fuzziness invert
//   brush         — (layer_unpremultiply_opaque)
//   deep_take_painted      paintedPath (deep starts as the base widened by 257)
//   deep_take_painted_hdr  paintedPath (deep starts as half floats of base/255)
//   hdr_normalize  hdr16Path scalesOutPath (8-bit input unused)
//   hdr_restore    normal16Path scalesPath (8-bit input unused)
//   hdr_exposure   hdr16Path scale offset inverseGamma
//
// The last five write uint16 output (half-float bit patterns for the HDR
// ones) instead of 8-bit RGBA; their inputs that are 16-bit come from a raw
// file path rather than the usual 8-bit in.raw.
//
// The table builders below (build_levels_tables, build_exposure_table,
// build_gradient_map_table) port the Swift table generation —
// Levels.swift, ExposureSettings.table, GradientMapSettings.apply — because
// the C kernels only consume finished tables; they mirror the Go ports in
// internal/render/lut.go so both sides of the comparison start from the same
// settings.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <stdint.h>

#include "LevelsPixels.h"
#include "AdjustPixels.h"
#include "BrushPixels.h"
#include "PatchPixels.h"
#include "DitherPixels.h"
#include "ScanlinesPixels.h"
#include "HealPixels.h"
#include "LensPixels.h"
#include "NoisePixels.h"
#include "WandPixels.h"
#include "DeepPixels.h"

// float_to_half is a verbatim copy of DeepPixels.c's static deep_to_half (a
// float as IEEE 754 binary16 bits, rounded to nearest). The kernel keeps it
// static, so the driver carries its own copy rather than changing the file
// AGENTS.md invariant #4 keeps byte-identical to upstream.
static uint16_t float_to_half(float value) {
    union { float f; uint32_t u; } in = { value };
    uint32_t sign = (in.u >> 16) & 0x8000, exponent = (in.u >> 23) & 0xff, mantissa = in.u & 0x7fffff;
    if (exponent == 0xff) return (uint16_t)(sign | 0x7c00 | (mantissa ? 0x200 : 0));
    int e = (int)exponent - 127 + 15;
    if (e >= 31) return (uint16_t)(sign | 0x7c00);
    if (e <= 0) {
        if (e < -10) return (uint16_t)sign;
        mantissa |= 0x800000;
        uint32_t shift = (uint32_t)(14 - e);
        uint32_t half = mantissa >> shift, rest = mantissa & ((1u << shift) - 1), halfway = 1u << (shift - 1);
        if (rest > halfway || (rest == halfway && (half & 1))) half += 1;
        return (uint16_t)(sign | half);
    }
    uint32_t half = sign | ((uint32_t)e << 10) | (mantissa >> 13), rest = mantissa & 0x1fff;
    if (rest > 0x1000 || (rest == 0x1000 && (half & 1))) half += 1;
    return (uint16_t)half;
}

static void die(const char *message) {
    fprintf(stderr, "golden_driver: %s\n", message);
    exit(2);
}

// Some kernels' outputs are not 8-bit RGBA (the deep/HDR ones are uint16 or
// half-float). A command that fills these in replaces the usual 8-bit write,
// so the CLI contract (out.raw gets the kernel's output) still holds.
static uint8_t *alt_bytes = NULL;
static size_t alt_length = 0;

static void set_alt_output(const void *bytes, size_t length) {
    alt_bytes = malloc(length ? length : 1);
    if (!alt_bytes) die("out of memory");
    memcpy(alt_bytes, bytes, length);
    alt_length = length;
}

static double arg_double(const char *s) {
    char *end;
    double v = strtod(s, &end);
    if (end == s) die("bad numeric argument");
    return v;
}

static long arg_long(const char *s) {
    char *end;
    long v = strtol(s, &end, 10);
    if (end == s) die("bad integer argument");
    return v;
}

static uint8_t *read_file(const char *path, size_t *length) {
    FILE *f = fopen(path, "rb");
    if (!f) die("cannot open input file");
    fseek(f, 0, SEEK_END);
    long size = ftell(f);
    fseek(f, 0, SEEK_SET);
    if (size < 0) die("cannot size input file");
    uint8_t *bytes = malloc((size_t)size ? (size_t)size : 1);
    if (!bytes) die("out of memory");
    if (fread(bytes, 1, (size_t)size, f) != (size_t)size) die("short read");
    fclose(f);
    *length = (size_t)size;
    return bytes;
}

static void write_file(const char *path, const uint8_t *bytes, size_t length) {
    FILE *f = fopen(path, "wb");
    if (!f) die("cannot open output file");
    if (fwrite(bytes, 1, length, f) != length) die("short write");
    fclose(f);
}

// ---- Swift table-builder ports (see file header) ------------------------

static double clamp_to(double n, double lo, double hi, double fallback) {
    if (isnan(n) || isinf(n)) return fallback;
    return fmin(hi, fmax(lo, n));
}

typedef struct { double black, gamma, white, outputBlack, outputWhite; } LevelRange;

static LevelRange normalize_level_range(LevelRange r) {
    r.black = clamp_to(r.black, 0, 254, 0);
    r.white = clamp_to(r.white, r.black + 1, 255, 255);
    r.gamma = clamp_to(r.gamma, 0.1, 9.99, 1);
    r.outputBlack = clamp_to(r.outputBlack, 0, 255, 0);
    r.outputWhite = clamp_to(r.outputWhite, 0, 255, 255);
    return r;
}

static double apply_level_range(LevelRange r, double value) {
    r = normalize_level_range(r);
    double input = fmin(1, fmax(0, (value * 255 - r.black) / (r.white - r.black)));
    return (r.outputBlack + pow(input, 1 / r.gamma) * (r.outputWhite - r.outputBlack)) / 255;
}

// Three tables of 256 floats: red, green, blue — per-channel range first,
// the composite RGB range (ranges[0]) after.
static void build_levels_tables(const double *ranges /* 4×5 */, float *tables /* 3×256 */) {
    static const int channel_index[3] = {1, 2, 3};
    for (int c = 0; c < 3; c++) {
        LevelRange per = {ranges[channel_index[c] * 5 + 0], ranges[channel_index[c] * 5 + 1],
                          ranges[channel_index[c] * 5 + 2], ranges[channel_index[c] * 5 + 3],
                          ranges[channel_index[c] * 5 + 4]};
        LevelRange master = {ranges[0], ranges[1], ranges[2], ranges[3], ranges[4]};
        for (int i = 0; i < 256; i++) {
            double value = (double)i / 255;
            tables[c * 256 + i] = (float)apply_level_range(master, apply_level_range(per, value));
        }
    }
}

static void build_exposure_table(double exposure, double offset, double gamma, float *table /* 256 */) {
    double scale = pow(2, exposure);
    for (int i = 0; i < 256; i++) {
        double encoded = (double)i / 255;
        double linear = encoded <= 0.04045 ? encoded / 12.92 : pow((encoded + 0.055) / 1.055, 2.4);
        linear = pow(fmax(0, linear * scale + offset), 1 / gamma);
        double out = linear <= 0 ? 0 : linear >= 1 ? 1
            : linear <= 0.0031308 ? linear * 12.92 : 1.055 * pow(linear, 1 / 2.4) - 0.055;
        table[i] = (float)fmin(1, fmax(0, out));
    }
}

static void build_gradient_map_table(const double *ends /* shadow rgb, light rgb */, int reversed,
                                     uint8_t *table /* 256×3 */) {
    const double *dark = reversed ? ends + 3 : ends;
    const double *light = reversed ? ends : ends + 3;
    for (int i = 0; i < 256; i++) {
        double t = (double)i / 255;
        for (int c = 0; c < 3; c++) {
            double v = dark[c] + (light[c] - dark[c]) * t;
            double scaled = v * 255;
            long rounded = lround(scaled);
            if (rounded < 0) rounded = 0;
            if (rounded > 255) rounded = 255;
            table[i * 3 + c] = (uint8_t)rounded;
        }
    }
}

// Synthetic cube for exercising cube_apply's trilinear path (the real HSV
// cube is built in Swift, not C). Mirrored exactly in golden_test.go.
static float *build_synthetic_cube(int dimension) {
    float *cube = malloc((size_t)dimension * dimension * dimension * 4 * sizeof(float));
    if (!cube) die("out of memory");
    size_t i = 0;
    for (int b = 0; b < dimension; b++) {
        for (int g = 0; g < dimension; g++) {
            for (int r = 0; r < dimension; r++) {
                double q = (double)r / (dimension - 1);
                double u = (double)g / (dimension - 1);
                double v = (double)b / (dimension - 1);
                cube[i] = (float)(q * q);            // red: quadratic ramp
                cube[i + 1] = (float)(4 * u * (1 - u)); // green: parabola, 0 at both ends
                cube[i + 2] = (float)(2 * v - 1);    // blue: signed linear
                cube[i + 3] = 1;
                i += 4;
            }
        }
    }
    return cube;
}

// ---- Camera Raw curve table builders (mirror CameraRawCurveSettings) ----
// toneTable/channelTable route the camera's 0…1 coordinates through the
// CurvesSettings.value math at the 0…255 scale, exactly as the Go port in
// internal/render/lut.go (curveValue) does; both sides of the comparison
// build their tables with these mirrored builders.

static double cam_curve_value(const double *xs, const double *ys, int n, double x) {
    int i = -1;
    for (int j = n - 1; j >= 0; --j) {
        if (xs[j] <= x) { i = j; break; }
    }
    if (i < 0) i = 0;
    if (i > n - 2) i = n - 2;
    double *d = malloc((size_t)(n - 1) * sizeof(double));
    if (!d) die("out of memory");
    for (int j = 0; j + 1 < n; ++j) d[j] = (ys[j + 1] - ys[j]) / (xs[j + 1] - xs[j]);
    double slope[2];
    for (int k = 0; k < 2; ++k) {
        int j = i + k;
        if (j == 0) slope[k] = d[0];
        else if (j == n - 1) slope[k] = d[n - 2];
        else if (d[j - 1] * d[j] <= 0) slope[k] = 0;
        else slope[k] = 2 / (1 / d[j - 1] + 1 / d[j]);
    }
    double h = xs[i + 1] - xs[i];
    double t = (x - xs[i]) / h;
    if (t < 0) t = 0;
    if (t > 1) t = 1;
    double t2 = t * t, t3 = t2 * t;
    double y = (2 * t3 - 3 * t2 + 1) * ys[i] + (t3 - 2 * t2 + t) * h * slope[0]
        + (-2 * t3 + 3 * t2) * ys[i + 1] + (t3 - t2) * h * slope[1];
    free(d);
    return y < 0 ? 0 : y > 255 ? 255 : y;
}

static double cam_curve_point(const double *xs, const double *ys, int n, double x) {
    if (n < 2) return x;
    double *sx = malloc((size_t)n * sizeof(double));
    double *sy = malloc((size_t)n * sizeof(double));
    if (!sx || !sy) die("out of memory");
    for (int j = 0; j < n; ++j) {
        sx[j] = xs[j] * 255;
        sy[j] = ys[j] * 255;
    }
    double v = cam_curve_value(sx, sy, n, x * 255) / 255.0;
    free(sx);
    free(sy);
    return v;
}

static double cam_bend(double tone, double lower, double low, double upper, double high) {
    double strength = 1.66;
    if (tone < lower && lower > 0) return lower * pow(tone / lower, pow(2, -low / 100 * strength));
    if (tone > upper && upper < 1) {
        double rest = 1 - upper;
        return 1 - rest * pow((1 - tone) / rest, pow(2, high / 100 * strength));
    }
    return tone;
}

#define CAM_CURVE_MAX 128

static void build_camera_raw_tables(const double *par7, const double *cx[4], const double *cy[4],
                                    const int cn[4], float *tables /* 4×256 */) {
    int hasParam = par7[0] != 0 || par7[1] != 0 || par7[2] != 0 || par7[3] != 0;
    for (int i = 0; i < 256; ++i) {
        double x = (double)i / 255;
        double tone = x;
        if (hasParam) {
            double ax[33], ay[33];
            for (int k = 0; k <= 32; ++k) {
                double ak = (double)k / 32;
                ax[k] = ak;
                ay[k] = cam_bend(cam_bend(ak, par7[4] / 100, par7[0], par7[6] / 100, par7[3]),
                                 par7[5] / 100, par7[1], par7[5] / 100, par7[2]);
            }
            tone = cam_curve_point(ax, ay, 33, x);
        }
        tables[0 * 256 + i] = (float)cam_curve_point(cx[0], cy[0], cn[0], tone);
        tables[1 * 256 + i] = (float)cam_curve_point(cx[1], cy[1], cn[1], x);
        tables[2 * 256 + i] = (float)cam_curve_point(cx[2], cy[2], cn[2], x);
        tables[3 * 256 + i] = (float)cam_curve_point(cx[3], cy[3], cn[3], x);
    }
}

// Gray output (masks) expanded to RGBA so every command writes one layout.
static void gray_to_rgba(const uint8_t *gray, size_t count, uint8_t *out) {
    for (size_t i = 0; i < count; i++) {
        out[i * 4] = out[i * 4 + 1] = out[i * 4 + 2] = gray[i];
        out[i * 4 + 3] = 255;
    }
}

// RGBA box blur for tonal_contrast's blurred base (edge-clamped window,
// double sums, lround output) — mirrored exactly in golden_test.go.
static void box_blur_rgba(const uint8_t *src, uint8_t *dst, size_t width, size_t height, int radius) {
    size_t window = (size_t)(radius * 2 + 1);
    uint8_t *temp = malloc(width * height * 4);
    if (!temp) die("out of memory");
    for (size_t y = 0; y < height; ++y) {
        for (size_t x = 0; x < width; ++x) {
            double sums[4] = {0, 0, 0, 0};
            for (int k = -radius; k <= radius; ++k) {
                long cx = (long)x + k;
                if (cx < 0) cx = 0;
                if (cx >= (long)width) cx = (long)width - 1;
                const uint8_t *p = src + y * width * 4 + (size_t)cx * 4;
                for (int c = 0; c < 4; ++c) sums[c] += p[c];
            }
            uint8_t *o = temp + (y * width + x) * 4;
            for (int c = 0; c < 4; ++c) o[c] = (uint8_t)lround(sums[c] / (double)window);
        }
    }
    for (size_t y = 0; y < height; ++y) {
        for (size_t x = 0; x < width; ++x) {
            double sums[4] = {0, 0, 0, 0};
            for (int k = -radius; k <= radius; ++k) {
                long cy = (long)y + k;
                if (cy < 0) cy = 0;
                if (cy >= (long)height) cy = (long)height - 1;
                const uint8_t *p = temp + ((size_t)cy * width + x) * 4;
                for (int c = 0; c < 4; ++c) sums[c] += p[c];
            }
            uint8_t *o = dst + (y * width + x) * 4;
            for (int c = 0; c < 4; ++c) o[c] = (uint8_t)lround(sums[c] / (double)window);
        }
    }
    free(temp);
}

int main(int argc, char **argv) {
    if (argc < 5) die("usage: golden_driver <command> <in.raw> <out.raw> <width> <height> [params...]");
    const char *command = argv[1], *in_path = argv[2], *out_path = argv[3];
    long width = arg_long(argv[4]), height = arg_long(argv[5]);
    if (width <= 0 || height <= 0 || width > 4096 || height > 4096) die("bad canvas size");
    size_t count = (size_t)width * (size_t)height;
    size_t length;
    uint8_t *pixels = read_file(in_path, &length);
    if (length != count * 4) die("input raw is not width×height×4 bytes");
    int argi = 6;

    if (!strcmp(command, "levels")) {
        if (argc < argi + 20) die("levels needs 20 range values");
        double ranges[20];
        for (int i = 0; i < 20; i++) ranges[i] = arg_double(argv[argi + i]);
        float tables[3 * 256];
        build_levels_tables(ranges, tables);
        levels_apply(pixels, count, tables);
    } else if (!strcmp(command, "exposure")) {
        if (argc < argi + 3) die("exposure needs 3 values");
        float table[256];
        build_exposure_table(arg_double(argv[argi]), arg_double(argv[argi + 1]), arg_double(argv[argi + 2]), table);
        float tables[3 * 256];
        for (int c = 0; c < 3; c++) memcpy(tables + c * 256, table, sizeof(table));
        levels_apply(pixels, count, tables);
    } else if (!strcmp(command, "gradient_map")) {
        if (argc < argi + 7) die("gradient_map needs 6 color floats + reversed");
        double ends[6];
        for (int i = 0; i < 6; i++) ends[i] = arg_double(argv[argi + i]);
        uint8_t table[256 * 3];
        build_gradient_map_table(ends, (int)arg_long(argv[argi + 6]), table);
        adjust_gradient_map(pixels, (size_t)width, (size_t)height, (size_t)width * 4, table);
    } else if (!strcmp(command, "cube")) {
        float *cube = build_synthetic_cube(33);
        cube_apply(pixels, count, cube, 33);
        free(cube);
    } else if (!strcmp(command, "grain")) {
        if (argc < argi + 7) die("grain needs amount size roughness seed originX originY unitsPerPixel");
        adjust_grain(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                     arg_double(argv[argi]), arg_double(argv[argi + 1]), arg_double(argv[argi + 2]),
                     (uint32_t)arg_long(argv[argi + 3]), arg_double(argv[argi + 4]),
                     arg_double(argv[argi + 5]), arg_double(argv[argi + 6]));
    } else if (!strcmp(command, "black_white")) {
        if (argc < argi + 9) die("black_white needs 6 weights + tint + hue + saturation");
        float weights[6];
        for (int i = 0; i < 6; i++) weights[i] = (float)arg_double(argv[argi + i]);
        adjust_black_white(pixels, (size_t)width, (size_t)height, (size_t)width * 4, weights,
                           (int)arg_long(argv[argi + 6]), arg_double(argv[argi + 7]), arg_double(argv[argi + 8]));
    } else if (!strcmp(command, "color_balance")) {
        if (argc < argi + 10) die("color_balance needs 9 shifts + preserveLuminosity");
        float shifts[9];
        for (int i = 0; i < 9; i++) shifts[i] = (float)arg_double(argv[argi + i]);
        adjust_color_balance(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                             shifts, shifts + 3, shifts + 6, (int)arg_long(argv[argi + 9]));
    } else if (!strcmp(command, "camera_raw_curve")) {
        // argv: rgbN (x y)*, redN (x y)*, greenN (x y)*, blueN (x y)*, then
        // shadows darks lights highlights shadowSplit darkSplit lightSplit
        // refineSaturation, mixer[24], pointCount (9 floats)×n, grade[12],
        // blending balance visualize.
        const double *cx[4], *cy[4];
        int cn[4];
        double *xs[4], *ys[4];
        int argi2 = argi;
        int ok = 1;
        for (int c = 0; c < 4 && ok; ++c) {
            if (argi2 >= argc) { die("camera_raw_curve: curve count missing"); }
            cn[c] = (int)arg_long(argv[argi2++]);
            if (cn[c] < 0 || cn[c] > CAM_CURVE_MAX) die("camera_raw_curve: bad curve count");
            xs[c] = malloc((size_t)(cn[c] ? cn[c] : 1) * sizeof(double));
            ys[c] = malloc((size_t)(cn[c] ? cn[c] : 1) * sizeof(double));
            if (!xs[c] || !ys[c]) die("out of memory");
            for (int j = 0; j < cn[c]; ++j) {
                xs[c][j] = arg_double(argv[argi2++]);
                ys[c][j] = arg_double(argv[argi2++]);
            }
            cx[c] = xs[c];
            cy[c] = ys[c];
        }
        if (ok) {
            double par7[7];
            for (int i = 0; i < 7; ++i) par7[i] = arg_double(argv[argi2++]);
            double refineSaturation = arg_double(argv[argi2++]);
            float mixer[24];
            for (int i = 0; i < 24; ++i) mixer[i] = (float)arg_double(argv[argi2++]);
            int pointCount = (int)arg_long(argv[argi2++]);
            if (pointCount < 0) die("camera_raw_curve: bad point count");
            float *points = malloc((size_t)(pointCount ? pointCount * 9 : 1) * sizeof(float));
            if (!points) die("out of memory");
            for (int i = 0; i < pointCount * 9; ++i) points[i] = (float)arg_double(argv[argi2++]);
            float grade[12];
            for (int i = 0; i < 12; ++i) grade[i] = (float)arg_double(argv[argi2++]);
            double blending = arg_double(argv[argi2++]);
            double balance = arg_double(argv[argi2++]);
            int visualize = (int)arg_long(argv[argi2++]);
            float tables[4 * 256];
            build_camera_raw_tables(par7, cx, cy, cn, tables);
            // NULL mixer/grade tables: this case drives the formula path; the
            // measured tables those arguments take are ticket 78's data.
            adjust_camera_raw_curve_color(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                                          tables, tables + 256, tables + 512, tables + 768,
                                          refineSaturation, mixer, pointCount, points,
                                          grade, blending, balance, visualize,
                                          NULL, NULL, 33);
            free(points);
        }
        for (int c = 0; c < 4; ++c) {
            free(xs[c]);
            free(ys[c]);
        }
    } else if (!strcmp(command, "camera_raw_effects")) {
        if (argc < argi + 15) die("camera_raw_effects needs 13 doubles + 2 style ints + scale");
        adjust_camera_raw_effects(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                                  arg_double(argv[argi]), arg_double(argv[argi + 1]),
                                  arg_double(argv[argi + 2]), arg_double(argv[argi + 3]),
                                  (int)arg_long(argv[argi + 4]), arg_double(argv[argi + 5]),
                                  arg_double(argv[argi + 6]), arg_double(argv[argi + 7]),
                                  arg_double(argv[argi + 8]), arg_double(argv[argi + 9]),
                                  arg_double(argv[argi + 10]), arg_double(argv[argi + 11]),
                                  arg_double(argv[argi + 12]), (int)arg_long(argv[argi + 13]),
                                  arg_double(argv[argi + 14]));
    } else if (!strcmp(command, "camera_raw_detail")) {
        if (argc < argi + 11) die("camera_raw_detail needs 10 sliders + scale");
        double d[11];
        for (int i = 0; i < 11; i++) d[i] = arg_double(argv[argi + i]);
        adjust_camera_raw_detail(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                                 d[0], d[1], d[2], d[3], d[4], d[5], d[6], d[7], d[8], d[9],
                                 d[10]);
    } else if (!strcmp(command, "camera_raw_optics")) {
        if (argc < argi + 14) die("camera_raw_optics needs 2 flags + 11 doubles + scale");
        adjust_camera_raw_optics(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                                 (int)arg_long(argv[argi]), (int)arg_long(argv[argi + 1]),
                                 arg_double(argv[argi + 2]), arg_double(argv[argi + 3]),
                                 arg_double(argv[argi + 4]), arg_double(argv[argi + 5]),
                                 arg_double(argv[argi + 6]), arg_double(argv[argi + 7]),
                                 arg_double(argv[argi + 8]), arg_double(argv[argi + 9]),
                                 arg_double(argv[argi + 10]), arg_double(argv[argi + 11]),
                                 arg_double(argv[argi + 12]), arg_double(argv[argi + 13]),
                                 // No exposure tables: the lens-vignetting
                                 // correction now reads them (ticket 78), so
                                 // this case leaves the vignette slider at 0.
                                 NULL, 0);
    } else if (!strcmp(command, "camera_raw_calibration")) {
        if (argc < argi + 8) die("camera_raw_calibration needs 7 doubles + processVersion");
        adjust_camera_raw_calibration(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                                      arg_double(argv[argi]), arg_double(argv[argi + 1]),
                                      arg_double(argv[argi + 2]), arg_double(argv[argi + 3]),
                                      arg_double(argv[argi + 4]), arg_double(argv[argi + 5]),
                                      arg_double(argv[argi + 6]), (int)arg_long(argv[argi + 7]));
    } else if (!strcmp(command, "colored_vignette")) {
        if (argc < argi + 13) die("colored_vignette needs 4 frame + 1 flag + 9 doubles");
        double f[13];
        for (int i = 0; i < 13; i++) f[i] = arg_double(argv[argi + i]);
        adjust_colored_vignette(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                                f[0], f[1], f[2], f[3], (int)f[4],
                                f[5], f[6], f[7], f[8], f[9], f[10], f[11], f[12]);
    } else if (!strcmp(command, "tonal_contrast")) {
        if (argc < argi + 5) die("tonal_contrast needs blurRadius amount shadows midtones highlights");
        double t[5];
        for (int i = 0; i < 5; i++) t[i] = arg_double(argv[argi + i]);
        uint8_t *base = malloc(count * 4);
        if (!base) die("out of memory");
        box_blur_rgba(pixels, base, (size_t)width, (size_t)height, (int)t[0]);
        adjust_tonal_contrast(pixels, base, (size_t)width, (size_t)height,
                              (size_t)width * 4, (size_t)width * 4, t[1], t[2], t[3], t[4]);
        free(base);
    } else if (!strcmp(command, "patch_fill")) {
        if (argc < argi + 2) die("patch_fill needs a mask path and a seed");
        size_t mask_length;
        uint8_t *mask = read_file(argv[argi], &mask_length);
        if (mask_length != count) die("hole raw is not width×height bytes");
        if (patch_fill(pixels, (size_t)width * 4, mask, (size_t)width, (int)width, (int)height,
                       (uint32_t)arg_long(argv[argi + 1])) != 1)
            die("patch_fill failed");
        free(mask);
    } else if (!strcmp(command, "quantize16")) {
        // The harness's input pipeline is 8-bit RGBA and dither_quantize16
        // takes 16-bit, so widen every byte by 257 (the exact 8→16 scale) and
        // run the case through the same procedural inputs as the others.
        uint16_t *wide = malloc((size_t)count * 4 * sizeof(uint16_t));
        if (!wide) die("out of memory");
        for (size_t i = 0; i < (size_t)count * 4; ++i) wide[i] = (uint16_t)(pixels[i] * 257);
        dither_quantize16(wide, pixels, (size_t)width, (size_t)height, (size_t)width * 4);
        free(wide);
    } else if (!strcmp(command, "deep_take_painted")) {
        // argv: paintedPath. The 8-bit input is the layer's base; the deep
        // raster starts as its exact 8→16 widening (v*257), as Go does.
        if (argc < argi + 1) die("deep_take_painted needs a painted raw path");
        size_t painted_length;
        uint8_t *painted = read_file(argv[argi], &painted_length);
        if (painted_length != count * 4) die("painted raw is not width×height RGBA");
        size_t words = (size_t)count * 4;
        uint16_t *deep = malloc(words * sizeof(uint16_t));
        if (!deep) die("out of memory");
        for (size_t i = 0; i < words; ++i) deep[i] = (uint16_t)(pixels[i] * 257);
        deep_take_painted(deep, (size_t)width * 8, painted, pixels, (size_t)width * 4,
                          (size_t)width, (size_t)height);
        set_alt_output(deep, words * sizeof(uint16_t));
        free(deep);
        free(painted);
    } else if (!strcmp(command, "deep_take_painted_hdr")) {
        // argv: paintedPath. The deep raster starts as half floats of the
        // 8-bit base (v/255), so the untouched pixels stay at their own depth.
        if (argc < argi + 1) die("deep_take_painted_hdr needs a painted raw path");
        size_t painted_length;
        uint8_t *painted = read_file(argv[argi], &painted_length);
        if (painted_length != count * 4) die("painted raw is not width×height RGBA");
        size_t words = (size_t)count * 4;
        uint16_t *deep = malloc(words * sizeof(uint16_t));
        if (!deep) die("out of memory");
        for (size_t i = 0; i < words; ++i) deep[i] = float_to_half((float)pixels[i] / 255.0f);
        deep_take_painted_hdr(deep, (size_t)width * 8, painted, pixels, (size_t)width * 4,
                              (size_t)width, (size_t)height);
        set_alt_output(deep, words * sizeof(uint16_t));
        free(deep);
        free(painted);
    } else if (!strcmp(command, "hdr_normalize")) {
        // argv: hdr16Path scalesOutPath. The HDR input is a raw uint16 file
        // (half-float bit patterns); the 8-bit input is unused.
        if (argc < argi + 2) die("hdr_normalize needs an hdr16 raw and a scales out path");
        size_t hdr_length;
        uint8_t *hdr_bytes = read_file(argv[argi], &hdr_length);
        size_t words = (size_t)count * 4;
        if (hdr_length != words * sizeof(uint16_t)) die("hdr16 raw is not width×height RGBA half");
        const uint16_t *hdr = (const uint16_t *)hdr_bytes;
        uint16_t *normal = malloc(words * sizeof(uint16_t));
        float *scales = malloc((size_t)count * sizeof(float));
        if (!normal || !scales) die("out of memory");
        deep_hdr_normalize(hdr, (size_t)width * 8, normal, (size_t)width * 8, scales,
                           (size_t)width, (size_t)height);
        write_file(argv[argi + 1], (const uint8_t *)scales, (size_t)count * sizeof(float));
        set_alt_output(normal, words * sizeof(uint16_t));
        free(normal);
        free(scales);
        free(hdr_bytes);
    } else if (!strcmp(command, "hdr_restore")) {
        // argv: normal16Path scalesPath. Rebuilds half floats from a 16-bit
        // raster and the scales hdr_normalize wrote.
        if (argc < argi + 2) die("hdr_restore needs a normal16 raw and a scales path");
        size_t normal_length, scales_length;
        uint8_t *normal_bytes = read_file(argv[argi], &normal_length);
        uint8_t *scales_bytes = read_file(argv[argi + 1], &scales_length);
        size_t words = (size_t)count * 4;
        if (normal_length != words * sizeof(uint16_t)) die("normal16 raw is not width×height RGBA");
        if (scales_length != (size_t)count * sizeof(float)) die("scales raw is not width×height floats");
        uint16_t *out = malloc(words * sizeof(uint16_t));
        if (!out) die("out of memory");
        deep_hdr_restore((const uint16_t *)normal_bytes, (size_t)width * 8,
                         (const float *)scales_bytes, out, (size_t)width * 8,
                         (size_t)width, (size_t)height);
        set_alt_output(out, words * sizeof(uint16_t));
        free(out);
        free(normal_bytes);
        free(scales_bytes);
    } else if (!strcmp(command, "hdr_exposure")) {
        // argv: hdr16Path scale offset inverseGamma.
        if (argc < argi + 4) die("hdr_exposure needs an hdr16 raw and scale offset inverseGamma");
        size_t hdr_length;
        uint8_t *hdr_bytes = read_file(argv[argi], &hdr_length);
        size_t words = (size_t)count * 4;
        if (hdr_length != words * sizeof(uint16_t)) die("hdr16 raw is not width×height RGBA half");
        uint16_t *hdr = malloc(words * sizeof(uint16_t));
        if (!hdr) die("out of memory");
        memcpy(hdr, hdr_bytes, words * sizeof(uint16_t));
        deep_hdr_exposure(hdr, (size_t)width * 8, (size_t)width, (size_t)height,
                          (float)arg_double(argv[argi + 1]), (float)arg_double(argv[argi + 2]),
                          (float)arg_double(argv[argi + 3]));
        set_alt_output(hdr, words * sizeof(uint16_t));
        free(hdr);
        free(hdr_bytes);
    } else if (!strcmp(command, "deep_split")) {
        // argv: deep16Path step channel. The 8-bit input is unused.
        if (argc < argi + 3) die("deep_split needs a deep16 raw, step and channel");
        size_t deep_length;
        uint8_t *deep_bytes = read_file(argv[argi], &deep_length);
        size_t words = (size_t)count * 4;
        if (deep_length != words * sizeof(uint16_t)) die("deep16 raw is not width×height RGBA");
        uint8_t *out = malloc(words);
        if (!out) die("out of memory");
        deep_split((const uint16_t *)deep_bytes, (size_t)width * 8, out, (size_t)width * 4,
                   (size_t)width, (size_t)height, (int)arg_long(argv[argi + 1]), (int)arg_long(argv[argi + 2]));
        set_alt_output(out, words);
        free(out);
        free(deep_bytes);
    } else if (!strcmp(command, "deep_merge")) {
        // argv: deep16Path lowPath near0 near1 near2 mid0 mid1 mid2 far0 far1 far2 mixes.
        // The 8-bit input is unused; every run raster is width×height RGBA bytes.
        if (argc < argi + 12) die("deep_merge needs a deep16 raw, a low raw, nine run raws and mixes");
        size_t deep_length;
        uint8_t *deep_bytes = read_file(argv[argi], &deep_length);
        size_t words = (size_t)count * 4;
        if (deep_length != words * sizeof(uint16_t)) die("deep16 raw is not width×height RGBA");
        uint8_t *runs[11];
        for (int i = 0; i < 11; i++) {
            size_t run_length;
            runs[i] = read_file(argv[argi + 1 + i], &run_length);
            if (run_length != words) die("a run raw is not width×height RGBA bytes");
        }
        uint16_t *out = malloc(words * sizeof(uint16_t));
        if (!out) die("out of memory");
        const uint8_t *near[3] = { runs[2], runs[3], runs[4] };
        const uint8_t *mid[3] = { runs[5], runs[6], runs[7] };
        const uint8_t *far[3] = { runs[8], runs[9], runs[10] };
        deep_merge((const uint16_t *)deep_bytes, (size_t)width * 8, runs[1], near, mid, far,
                   (size_t)width * 4, out, (size_t)width * 8, (size_t)width, (size_t)height,
                   (int)arg_long(argv[argi + 11]));
        set_alt_output(out, words * sizeof(uint16_t));
        free(out);
        for (int i = 0; i < 11; i++) free(runs[i]);
        free(deep_bytes);
    } else if (!strcmp(command, "deep_roundtrip")) {
        // argv: deep16Path mixes. Splits the 16-bit raster, runs an identity
        // operation on each run, merges, and writes the 16-bit result — the
        // whole DeepLayer.run pipeline in one command. The 8-bit input is unused.
        if (argc < argi + 2) die("deep_roundtrip needs a deep16 raw and mixes");
        size_t deep_length;
        uint8_t *deep_bytes = read_file(argv[argi], &deep_length);
        size_t words = (size_t)count * 4;
        if (deep_length != words * sizeof(uint16_t)) die("deep16 raw is not width×height RGBA");
        int mixes = (int)arg_long(argv[argi + 1]);
        int channels = mixes ? 3 : 1;
        // (step, channel) pairs: low first, then 1/2/4 steps.
        uint8_t *runs[10];
        int n = 0;
        runs[n] = malloc(words);
        if (!runs[n]) die("out of memory");
        deep_split((const uint16_t *)deep_bytes, (size_t)width * 8, runs[n], (size_t)width * 4,
                   (size_t)width, (size_t)height, 0, -1);
        n++;
        const int steps[3] = { 1, 2, 4 };
        for (int s = 0; s < 3; s++) {
            for (int c = 0; c < channels; c++) {
                runs[n] = malloc(words);
                if (!runs[n]) die("out of memory");
                deep_split((const uint16_t *)deep_bytes, (size_t)width * 8, runs[n], (size_t)width * 4,
                           (size_t)width, (size_t)height, steps[s], mixes ? c : -1);
                n++;
            }
        }
        uint16_t *out = malloc(words * sizeof(uint16_t));
        if (!out) die("out of memory");
        // Mirrors DeepLayer.run's runs(_:): without mixes each step has one run,
        // read for all three channels; with it, the step's three runs are red,
        // green and blue stepped alone.
        const int per = mixes ? 3 : 1;
        const int off[3] = { 0, mixes ? 1 : 0, mixes ? 2 : 0 };
        const uint8_t *near[3] = { runs[1], runs[1 + off[1]], runs[1 + off[2]] };
        const uint8_t *mid[3] = { runs[1 + per], runs[1 + per + off[1]], runs[1 + per + off[2]] };
        const uint8_t *far[3] = { runs[1 + 2 * per], runs[1 + 2 * per + off[1]], runs[1 + 2 * per + off[2]] };
        deep_merge((const uint16_t *)deep_bytes, (size_t)width * 8, runs[0], near, mid, far,
                   (size_t)width * 4, out, (size_t)width * 8, (size_t)width, (size_t)height, mixes);
        set_alt_output(out, words * sizeof(uint16_t));
        free(out);
        for (int i = 0; i < n; i++) free(runs[i]);
        free(deep_bytes);
    } else if (!strcmp(command, "camera_raw_compose")) {
        // argv: tablesPath specPath. The 8-bit input is unused. The tables are
        // the real decoded resource, written by the test; the spec says which
        // of them each stage blends:
        //
        //   grid size count
        //   idx:weight [idx:weight ...]     one line a stage, up to 8 pairs
        if (argc < argi + 2) die("camera_raw_compose needs a tables raw and a spec");
        size_t tables_length;
        uint8_t *tables = read_file(argv[argi], &tables_length);
        FILE *spec = fopen(argv[argi + 1], "rb");
        if (!spec) die("cannot open the compose spec");
        int grid = 0, size = 0, count = 0;
        if (fscanf(spec, "%d %d %d", &grid, &size, &count) != 3) die("bad spec header");
        if (grid < 2 || size < 2 || count < 0 || count > 64) die("bad spec geometry");
        size_t cells = (size_t)size * size * size;
        if (tables_length < cells * 3) die("the tables raw is too short");
        static CameraRawStage stages[64];
        char line[1024];
        if (!fgets(line, sizeof line, spec)) die("the spec has no stage lines");
        for (int s = 0; s < count; ++s) {
            if (!fgets(line, sizeof line, spec)) die("the spec has too few stage lines");
            memset(&stages[s], 0, sizeof stages[s]);
            int slot = 0;
            for (char *token = strtok(line, " \t\r\n"); token && slot < 8; token = strtok(NULL, " \t\r\n")) {
                int index = 0;
                float weight = 0;
                if (sscanf(token, "%d:%f", &index, &weight) != 2) die("bad stage entry");
                // -1 is a nil table: the stage passes that slot's colour through.
                if (index < 0) {
                    stages[s].table[slot] = NULL;
                } else {
                    if ((size_t)index >= tables_length / (cells * 3)) die("table index out of range");
                    stages[s].table[slot] = tables + (size_t)index * cells * 3;
                }
                stages[s].weight[slot] = weight;
                slot++;
            }
        }
        fclose(spec);
        size_t out_length = (size_t)grid * grid * grid * 3 * sizeof(float);
        float *out = malloc(out_length);
        if (!out) die("out of memory");
        camera_raw_compose(out, grid, stages, count, size);
        set_alt_output(out, out_length);
        free(out);
        free(tables);
    } else if (!strcmp(command, "dither")) {
        if (argc < argi + 17) die("dither needs 17 parameters");
        DitherParams params;
        memset(&params, 0, sizeof(params));
        params.style = (int)arg_long(argv[argi]);
        params.levels = (int)arg_long(argv[argi + 1]);
        params.diffusion = (float)arg_double(argv[argi + 2]);
        params.density = (float)arg_double(argv[argi + 3]);
        params.contrast = (float)arg_double(argv[argi + 4]);
        params.cell = (int)arg_long(argv[argi + 5]);
        params.angle = (float)arg_double(argv[argi + 6]);
        params.lightOnDark = (int)arg_long(argv[argi + 7]);
        params.originalColors = (int)arg_long(argv[argi + 8]);
        params.dark[0] = (uint8_t)arg_long(argv[argi + 9]);
        params.dark[1] = (uint8_t)arg_long(argv[argi + 10]);
        params.dark[2] = (uint8_t)arg_long(argv[argi + 11]);
        params.light[0] = (uint8_t)arg_long(argv[argi + 12]);
        params.light[1] = (uint8_t)arg_long(argv[argi + 13]);
        params.light[2] = (uint8_t)arg_long(argv[argi + 14]);
        params.dots = (float)arg_double(argv[argi + 15]);
        params.wobble = (float)arg_double(argv[argi + 16]);
        if (dither_apply(pixels, (size_t)width, (size_t)height, (size_t)width * 4, &params) != 1)
            die("dither_apply failed");
    } else if (!strcmp(command, "scanlines")) {
        if (argc < argi + 18) die("scanlines needs 18 parameters");
        ScanlinesParams params;
        memset(&params, 0, sizeof(params));
        params.spacing = (int)arg_long(argv[argi]);
        params.thickness = (float)arg_double(argv[argi + 1]);
        params.dots = (float)arg_double(argv[argi + 2]);
        params.wobble = (float)arg_double(argv[argi + 3]);
        params.displace = (float)arg_double(argv[argi + 4]);
        params.threshold = (float)arg_double(argv[argi + 5]);
        params.split = (float)arg_double(argv[argi + 6]);
        params.density = (float)arg_double(argv[argi + 7]);
        params.contrast = (float)arg_double(argv[argi + 8]);
        params.blackLevel = (float)arg_double(argv[argi + 9]);
        params.smoothness = (float)arg_double(argv[argi + 10]);
        params.originalColors = (int)arg_long(argv[argi + 11]);
        params.dark[0] = (uint8_t)arg_long(argv[argi + 12]);
        params.dark[1] = (uint8_t)arg_long(argv[argi + 13]);
        params.dark[2] = (uint8_t)arg_long(argv[argi + 14]);
        params.light[0] = (uint8_t)arg_long(argv[argi + 15]);
        params.light[1] = (uint8_t)arg_long(argv[argi + 16]);
        params.light[2] = (uint8_t)arg_long(argv[argi + 17]);
        if (scanlines_apply(pixels, (size_t)width, (size_t)height, (size_t)width * 4, &params) != 1)
            die("scanlines_apply failed");
    } else if (!strcmp(command, "heal")) {
        if (argc < argi + 4) die("heal needs maskPath opacity mode seed");
        size_t mask_length;
        uint8_t *mask = read_file(argv[argi], &mask_length);
        if (mask_length != count) die("coverage raw is not width×height bytes");
        if (spot_heal(pixels, mask, (size_t)width, (size_t)height, (size_t)width * 4,
                      (float)arg_double(argv[argi + 1]), (int)arg_long(argv[argi + 2]),
                      (uint32_t)arg_long(argv[argi + 3])) != 0)
            die("spot_heal failed");
        free(mask);
    } else if (!strcmp(command, "lens")) {
        if (argc < argi + 1) die("lens needs k");
        uint8_t *destination = malloc(count * 4);
        if (!destination) die("out of memory");
        memset(destination, 0, count * 4);
        lens_distort(pixels, destination, (size_t)width, (size_t)height, (size_t)width * 4,
                     arg_double(argv[argi]));
        memcpy(pixels, destination, count * 4);
        free(destination);
    } else if (!strcmp(command, "noise")) {
        if (argc < argi + 4) die("noise needs amount gaussian monochromatic seed");
        noise_add(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                  (float)arg_double(argv[argi]), (int)arg_long(argv[argi + 1]),
                  (int)arg_long(argv[argi + 2]), (uint32_t)arg_long(argv[argi + 3]));
    } else if (!strcmp(command, "wand")) {
        if (argc < argi + 5) die("wand needs seedX seedY radius tolerance contiguous");
        uint8_t *mask = malloc(count);
        if (!mask) die("out of memory");
        long selected = wand_mask(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                                  (size_t)arg_long(argv[argi]), (size_t)arg_long(argv[argi + 1]),
                                  (size_t)arg_long(argv[argi + 2]), (int)arg_long(argv[argi + 3]),
                                  (int)arg_long(argv[argi + 4]), mask);
        if (selected < 0) die("wand_mask failed");
        gray_to_rgba(mask, count, pixels);
        free(mask);
    } else if (!strcmp(command, "color_range")) {
    // params: includeCount (r g b)×n excludeCount (r g b)×m fuzziness invert
    if (argc < argi + 2) die("color_range needs counts");
    long includeCount = arg_long(argv[argi]);
    long excludeCount = arg_long(argv[argi + 1]);
    if (argc < argi+2+(includeCount+excludeCount)*3+2) die("color_range color tables short");
    size_t tableBytes = (size_t)(includeCount + excludeCount) * 3;
    uint8_t *colors = malloc(tableBytes ? tableBytes : 1);
    if (!colors) die("out of memory");
    long v = argi + 2;
    for (long i = 0; i < (includeCount + excludeCount) * 3; i++) colors[i] = (uint8_t)arg_long(argv[v++]);
    long fuzziness = arg_long(argv[v++]);
    long invert = arg_long(argv[v++]);
    uint8_t *mask = malloc(count);
    if (!mask) die("out of memory");
    if (color_range_mask(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                         colors, (int)includeCount, colors + includeCount * 3, (int)excludeCount,
                         (int)fuzziness, (int)invert, mask) < 0)
      die("color_range_mask failed");
    gray_to_rgba(mask, count, pixels);
    free(mask);
    free(colors);
  } else if (!strcmp(command, "brush")) {
        layer_unpremultiply_opaque(pixels, (size_t)width * 4, (size_t)width, (size_t)height);
    } else {
        die("unknown command");
    }

    if (alt_bytes) {
        write_file(out_path, alt_bytes, alt_length);
        free(alt_bytes);
    } else {
        write_file(out_path, pixels, count * 4);
    }
    free(pixels);
    return 0;
}
