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
//   camera_raw    11 grade doubles then clipping
//   content_fill  maskPath
//   dither        style levels diffusion density contrast cell angle lightOnDark
//                 originalColors darkR darkG darkB lightR lightG lightB dots wobble
//   heal          maskPath opacity mode seed
//   lens          k
//   noise         amount gaussian monochromatic seed
//   wand          seedX seedY radius tolerance contiguous
//   color_range   includeCount (r g b)×n excludeCount (r g b)×m fuzziness invert
//   brush         — (layer_unpremultiply_opaque)
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
#include "ContentFill.h"
#include "DitherPixels.h"
#include "HealPixels.h"
#include "LensPixels.h"
#include "NoisePixels.h"
#include "WandPixels.h"

static void die(const char *message) {
    fprintf(stderr, "golden_driver: %s\n", message);
    exit(2);
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

// Gray output (masks) expanded to RGBA so every command writes one layout.
static void gray_to_rgba(const uint8_t *gray, size_t count, uint8_t *out) {
    for (size_t i = 0; i < count; i++) {
        out[i * 4] = out[i * 4 + 1] = out[i * 4 + 2] = gray[i];
        out[i * 4 + 3] = 255;
    }
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
    } else if (!strcmp(command, "camera_raw")) {
        if (argc < argi + 12) die("camera_raw needs 11 grade doubles + clipping");
        double p[11];
        for (int i = 0; i < 11; i++) p[i] = arg_double(argv[argi + i]);
        adjust_camera_raw(pixels, (size_t)width, (size_t)height, (size_t)width * 4,
                          p[0], p[1], p[2], p[3], p[4], p[5], p[6], p[7], p[8], p[9], p[10],
                          (int)arg_long(argv[argi + 11]));
    } else if (!strcmp(command, "content_fill")) {
        if (argc < argi + 1) die("content_fill needs a mask path");
        size_t mask_length;
        uint8_t *mask = read_file(argv[argi], &mask_length);
        if (mask_length != count) die("mask raw is not width×height bytes");
        if (content_fill(pixels, (size_t)width * 4, mask, (size_t)width, (int)width, (int)height) != 1)
            die("content_fill failed");
        free(mask);
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

    write_file(out_path, pixels, count * 4);
    free(pixels);
    return 0;
}
