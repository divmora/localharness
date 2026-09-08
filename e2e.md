# End-to-End (E2E) Testing Guide

The AI Agents Office uses [Playwright](https://playwright.dev/) for robust End-to-End testing of the Tauri GUI. Because Tauri wraps a native WebKit (Linux) or WebView2 (Windows) window, running headless tests requires specific dependencies.

## Architecture

Our E2E tests are grouped into modular spec files in `gui/tests/`:
- `office-creation.spec.ts`: Tests basic office generation and assigning a single task.
- `manager-capacity.spec.ts`: Tests Manager capacity logic (max 10 tasks).
- `permanent-hire-capacity.spec.ts`: Tests Permanent capacity logic (max 5 tasks).
- `consultant-capacity.spec.ts`: Tests Consultancy capacity logic (max 2 tasks).

## How to Run Tests

### Recommended: Docker (Linux Headless)
Because WebKit requires X11/Wayland and specific DBus environments to run headlessly, we have provided a fully containerized environment that handles all sandboxing.

Simply run the helper script in the repository root:
```bash
./run-e2e-docker.sh
```

**What it does:**
1. Builds a pristine Ubuntu 22.04 container (`Dockerfile.e2e`) with `xvfb`, `libwebkit2gtk`, and Rust dependencies.
2. Compiles the Tauri backend in `release` mode.
3. Uses `xvfb-run` to trick WebKit into rendering headless.
4. Executes the Playwright test suite and outputs the results to your terminal.

**Troubleshooting Docker:**
- If you encounter a `no space left on device` error, ensure `.dockerignore` exists and you run `docker builder prune -a -f`.
- The first run will take ~3-4 minutes as it caches the Rust backend compilation. Subsequent runs are much faster.

### Running Visually (Desktop Linux, Mac, Windows)
If you have a real monitor (or a non-headless environment) and want to watch Playwright click the buttons, you can run the tests directly on your host machine:

```bash
cd gui
npm run test:e2e -- --headed
```
*Note: Make sure you have the Tauri CLI and Rust installed locally.*
