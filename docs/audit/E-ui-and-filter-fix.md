# E — UI and filter error handling audit

Date: 2026-10-13

## Changes

- The editor shell now uses a quieter dark workbench palette with clearer text, focus rings, selected tool states, and unsaved indicators.
- The right sidebar is a vertical stack: the Transform inspector stays visible at the top and the layer list owns its own scroll area. This prevents the inspector and layer rows from competing for the same narrow horizontal space.
- Tabs, menus, layer rows, and primary actions now have clearer hover, focus, and selected states.
- `removeBackground` now propagates ONNX runtime, model, and inference errors from the filter preview and commit paths. A missing segmentation mask cannot be treated as a successful filter operation.
- ONNX Runtime lookup now checks the directory containing the executable before the working directory and `models` directory. `EnsureReady` checks for the runtime before downloading the model.

## Verification

- `frontend/npm.cmd run check`: passed with 0 errors and 0 warnings.
- Transform and transform-session tests: 100 passed.
- UI component and transform tests: 29 passed.
- `go test ./internal/bridge -run 'Test.*Filter' -count=1`: passed.
- `go test ./internal/ml -count=1`: passed (no test files).
- `go vet ./internal/bridge ./internal/ml`: passed.
- `git diff --check`: passed.
- No files under `reference/Swift/` were changed.

## Limits

- No Wails executable was rebuilt in this pass, per the user request.
- No Computer Use or native Windows UI session was started in this pass, because the user was using the computer.
- A broader `go test ./internal/bridge ./internal/ml -count=1` run hit a Go 1.26 Windows process-level `net.setKeepAliveCount` fault while a test listener was starting. The filter-only bridge run passed separately.
- ONNX Runtime and the four shadow/glow effects still depend on their existing deployment and implementation work; this pass does not claim those features complete.
