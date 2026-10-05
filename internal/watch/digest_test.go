package watch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeMissingManifestIsDistinctError(t *testing.T) {
	dir := t.TempDir()
	_, err := Compute(dir)
	if !errors.Is(err, ErrNoManifest) {
		t.Fatalf("Compute on package without manifest: err = %v, want ErrNoManifest", err)
	}
}

func TestComputeStableForIdenticalContent(t *testing.T) {
	dir := setupProject(t, manifestV1, map[string]string{"A.png": pngOne})

	first, err := Compute(dir)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// A save that writes the very same bytes again must not read as a change:
	// rewrite both the image and the manifest with identical content.
	if err := os.WriteFile(filepath.Join(dir, "images", "A.png"), []byte(pngOne), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifestAtomically(t, dir, manifestV1)

	second, err := Compute(dir)
	if err != nil {
		t.Fatalf("Compute after identical rewrite: %v", err)
	}
	if first != second {
		t.Fatalf("digest changed for identical content: %s vs %s", first, second)
	}
}

func TestComputeChangesWhenContentChanges(t *testing.T) {
	dir := setupProject(t, manifestV1, map[string]string{"A.png": pngOne})
	base, err := Compute(dir)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// A manifest byte changes the digest.
	writeManifestAtomically(t, dir, manifestV2)
	afterManifest, err := Compute(dir)
	if err != nil {
		t.Fatal(err)
	}
	if afterManifest == base {
		t.Fatal("digest unchanged after manifest edit")
	}

	// A longer image (different size) changes the digest.
	if err := os.WriteFile(filepath.Join(dir, "images", "A.png"), []byte(pngTwo), 0o644); err != nil {
		t.Fatal(err)
	}
	afterImage, err := Compute(dir)
	if err != nil {
		t.Fatal(err)
	}
	if afterImage == afterManifest {
		t.Fatal("digest unchanged after image size change")
	}

	// Adding an image changes the digest.
	if err := os.WriteFile(filepath.Join(dir, "images", "A.mask.png"), []byte("mask"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterAdd, err := Compute(dir)
	if err != nil {
		t.Fatal(err)
	}
	if afterAdd == afterImage {
		t.Fatal("digest unchanged after adding an image")
	}

	// Removing it again returns to exactly the earlier digest: the digest is
	// content addressed, not a counter.
	if err := os.Remove(filepath.Join(dir, "images", "A.mask.png")); err != nil {
		t.Fatal(err)
	}
	afterRemove, err := Compute(dir)
	if err != nil {
		t.Fatal(err)
	}
	if afterRemove != afterImage {
		t.Fatalf("digest after remove = %s, want the earlier %s", afterRemove, afterAdd)
	}
}

func TestComputeToleratesMissingImagesDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifestV1), 0o644); err != nil {
		t.Fatal(err)
	}
	onlyManifest, err := Compute(dir)
	if err != nil {
		t.Fatalf("Compute without images dir: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	emptyImages, err := Compute(dir)
	if err != nil {
		t.Fatalf("Compute with empty images dir: %v", err)
	}
	if onlyManifest != emptyImages {
		t.Fatal("digest differs between missing and empty images dir")
	}
}

func TestComputeIgnoresSubdirectoriesInImages(t *testing.T) {
	dir := setupProject(t, manifestV1, map[string]string{"A.png": pngOne})
	with, err := Compute(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Swift's digest counts regular files only.
	if err := os.MkdirAll(filepath.Join(dir, "images", "QuickLook"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "images", "QuickLook", "Preview.jpg"), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	without, err := Compute(dir)
	if err != nil {
		t.Fatal(err)
	}
	if with != without {
		t.Fatal("digest changed when only a subdirectory was added")
	}
}
