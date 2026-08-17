# E2E Testing Guide (Tauri + Playwright)

This document provides a reference for running our End-to-End Playwright tests on the Tauri application locally and inside headless CI environments.

## Local Testing (With a GUI)

If you are running tests locally on a computer with a monitor (macOS, Windows, or Linux with a desktop environment), you can simply run:

```bash
cd gui
npm run test:e2e
```

Playwright will boot the Tauri app visually, connect to its WebSockets, and run the automated flows (e.g., creating an office, assigning tasks).

## Headless Testing (Linux CI/Remote Servers)

When attempting to run Tauri tests in a headless Linux environment (like GitHub Actions, GitLab CI, or a remote SSH server), the OS lacks a physical display server to render the WebKit2GTK window. To bypass this, we use `xvfb-run` to spin up a virtual display.

### 1. Install System Dependencies

Your headless server must have these dependencies installed (Ubuntu/Debian):

```bash
sudo apt-get update
sudo apt-get install -y \
  xvfb \
  libwebkit2gtk-4.1-dev \
  build-essential \
  curl \
  wget \
  file \
  libxdo-dev \
  libssl-dev \
  libayatana-appindicator3-dev \
  librsvg2-dev
```

### 2. Run the Headless Script

We've added a helper script to `package.json` that sets all required environment variables to disable hardware compositing (which crashes headless WebKit) and runs Playwright inside the `xvfb` virtual display:

```bash
cd gui
npm run test:e2e:headless
```

### Troubleshooting: `ECONNREFUSED /tmp/tauri-playwright.sock`

If you encounter this error while testing headless:
1. **Stale Sockets**: A previous test run may have crashed and left a stale socket. Run `rm -f /tmp/tauri-playwright.sock` to clean it up.
2. **Container Sandboxing Limits**: If you are running this inside a highly constrained Docker container, WebKit's security sandbox (Bubblewrap/bwrap) might fail to initialize because it lacks `SYS_ADMIN` privileges. 
   - **Fix 1**: Boot your Docker container with `--privileged` or `--cap-add=SYS_ADMIN`.
   - **Fix 2**: Add `WEBKIT_FORCE_SANDBOX=0` to your environment variables to explicitly disable the WebKit sandbox for testing. (This is already included in our `test:e2e:headless` script).
