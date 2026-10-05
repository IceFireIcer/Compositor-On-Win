package watch

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// ErrNoManifest reports that the package has no manifest.json, so no digest
// can be derived. Callers can single it out with errors.Is: it usually means a
// writer was caught before the atomic rename that commits a save.
var ErrNoManifest = errors.New("manifest.json is missing")

// Compute derives a stable content digest for the project package at dir, ported
// from reference/Swift/Compositor/IO/ProjectDigest.swift: the manifest byte for
// byte, then each regular file in images/ by name and byte size, all fed through
// SHA-256 and returned hex encoded.
//
// A package that was only touched — metadata rewritten, or the same bytes saved
// again — yields the same digest, so it is not treated as a change. Assets are
// not read: hashing every image would hold each check for seconds, and a PNG
// whose pixels change all but always changes size.
//
// A package caught half written yields a digest that matches nothing, or an
// error; both make the caller wait for the next change. A missing manifest
// returns an error wrapping ErrNoManifest.
func Compute(dir string) (string, error) {
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", ErrNoManifest, filepath.Join(dir, "manifest.json"))
		}
		return "", err
	}

	h := sha256.New()
	h.Write(manifest)

	images := filepath.Join(dir, "images")
	entries, err := os.ReadDir(images)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	// os.ReadDir already sorts by name; the explicit sort keeps the guarantee
	// independent of that contract.
	names := make([]string, 0, len(entries))
	byName := make(map[string]os.DirEntry, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue // directories and links are skipped, as Swift counts regular files only
		}
		names = append(names, entry.Name())
		byName[entry.Name()] = entry
	}
	sort.Strings(names)

	for _, name := range names {
		info, err := byName[name].Info()
		if err != nil {
			// The file vanished mid-read (a writer renaming things over): report
			// instead of hashing a digest that looks like a state never written.
			return "", err
		}
		h.Write([]byte(name))
		var size [8]byte
		binary.LittleEndian.PutUint64(size[:], uint64(info.Size()))
		h.Write(size[:])
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
