// Command gen regenerates the golden reference PNGs from the case JSONs by
// running the compiled C driver — the reproducibility entry point of the
// golden harness (docs/testing.md §2).
//
// Build the driver once (provenance and toolchain notes in ../c/README.md):
//
//	gcc -O2 -std=gnu99 -fblocks -I tests/golden/c -o tests/golden/c/golden_driver.exe tests/golden/c/*.c
//
// then, from the repository root:
//
//	go run ./tests/golden/gen
//
// Flags -driver/-cases/-out override the default locations.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	golden "compositor-win/tests/golden"
)

func main() {
	driver := flag.String("driver", "tests/golden/c/golden_driver.exe", "compiled C driver")
	casesDir := flag.String("cases", "tests/golden/cases", "case JSON directory")
	outDir := flag.String("out", "tests/golden/ref", "reference PNG output directory")
	flag.Parse()
	if _, err := os.Stat(*driver); err != nil {
		log.Fatalf("driver not found at %s — build it first (see tests/golden/c/README.md)", *driver)
	}
	cases, err := golden.LoadCases(*casesDir)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, c := range cases {
		tmp, err := os.MkdirTemp("", "golden")
		if err != nil {
			log.Fatal(err)
		}
		raw, err := golden.RunDriver(*driver, c, tmp)
		os.RemoveAll(tmp)
		if err != nil {
			log.Fatalf("%s: %v", c.CaseName, err)
		}
		file, err := golden.WrapPNG(raw, c.Width, c.Height)
		if err != nil {
			log.Fatalf("%s: %v", c.CaseName, err)
		}
		out := filepath.Join(*outDir, c.CaseName+".png")
		if err := os.WriteFile(out, file, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s → %s\n", c.CaseName, out)
	}
	fmt.Printf("regenerated %d references\n", len(cases))
}
