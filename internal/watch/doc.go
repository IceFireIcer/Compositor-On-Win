// Package watch watches an open project package (.comp folder) for external
// changes and decides when they are real. It is the Windows port of three Swift
// pieces from reference/Swift/Compositor/IO:
//
//   - ProjectDigest.swift  -> Compute: a SHA-256 fingerprint of the manifest
//     bytes plus each image's name and size, stable for identical content.
//   - ProjectWatcher.swift -> Watcher: file system events for the package
//     folder, its manifest.json and its images folder, coalesced into one
//     report per quiet period, with watches re-armed by path after every event
//     so an atomic rename that replaces the package keeps being followed.
//   - ProjectController+ExternalChanges.swift -> Coordinator: digest comparison
//     against the last read or written state, plus an exponential backoff
//     (250ms * 2^n, capped) that waits out a writer caught mid-save before one
//     stable digest is reported to the reload path.
//
// The writer contract (docs/writing-comp-files.md) is that agents write PNGs
// into images/ first and commit the save by renaming a sibling over
// manifest.json. The watcher's re-arm handles exactly that swap; the
// coordinator's digest compare keeps metadata-only touches and half-written
// packages from reaching the reload path.
package watch
