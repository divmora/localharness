# Remote Control & Cloudflare Quick Tunnels

LocalHarness includes built-in **Web Remote Control** powered by zero-login **Cloudflare Quick Tunnels** (`cloudflared`).
Just like Google Antigravity's `/remote-control`, you can securely monitor, prompt, and approve actions from your mobile phone, tablet, or any remote browser without creating a Cloudflare account, configuring DNS, opening firewall ports, or running complex reverse proxies.

---

## Key Features

- **Zero-Login & Account-Free**: Uses Cloudflare's TryCloudflare Quick Tunnel feature. Generates an instant, ephemeral `https://<random-subdomain>.trycloudflare.com` URL with zero configuration.
- **Auto-Provisioning**: If `cloudflared` is not installed on your system or in `$CLOUDFLARED_BIN`, LocalHarness automatically downloads official release binaries to `~/.divmora/localharness/bin/cloudflared`.
- **Instant ANSI QR Code**: Prints a scannable QR code directly in your terminal scrollback so you can point your phone camera and connect instantly.
- **Multi-Session Hub**: The tunnel connects directly to the LocalHarness daemon, allowing you to monitor active tasks, switch between sessions, review conversation history, or launch new prompts from a single interface.
- **Persistent Across CLI Stops**: Because the tunnel runs alongside the background daemon, remote control continues uninterrupted even if you close your terminal, press `Ctrl+D`, or run `/detach`. You can re-attach via `lhctl attach` or `lhctl -c` at your desk later.
- **End-to-End Authentication**: Every tunnel URL is protected by the daemon's cryptographically secure 256-bit API key embedded in the URL (`?key=<token>`). Unauthenticated requests are rejected with `401 Unauthorized`.
- **Touch-Friendly Mobile Web App**: Embedded single-page application with zero external CDN dependencies:
  - Real-time token streaming and markdown rendering.
  - Collapsible reasoning blocks (`💭 Thought for Xs`).
  - Action Approval Cards (`Allow Once`, `Allow in Session`, `Deny`).
  - Interactive Question Cards (`ask_question`).
  - Multi-session switcher with real-time search and human-readable conversation descriptions synthesized from initial prompts and goals.

---

## Quick Start

### 1. From the Interactive TUI (`lhctl`)

Inside an active `lhctl` session, type:

```text
/remote-control
```
*(or `/tunnel on`)*

```text
🌐 Cloudflare Quick Tunnel active (PID 83120)
Remote Control URL: https://brave-panda-xyz.trycloudflare.com/?key=4a9f...#0192a5b6

Scan with phone:
█████████████████████████████
█████████████████████████████
████ ▄▄▄▄▄ █▀▀▀█ ▄▄▄▄▄ ████
████ █   █ █▀█ █ █   █ ████
████ █▄▄▄█ █▀▀ █ █▄▄▄█ ████
...
```

Scan the QR code with your mobile camera to open the remote control interface on your phone.

To stop the tunnel, type:
```text
/tunnel off
```

To view the current tunnel URL and QR code again:
```text
/tunnel status
```

---

### 2. From the Command Line (`lhctl run --tunnel`)

Launch an interactive session with the tunnel started automatically:

```bash
# Start interactive session with tunnel enabled
lhctl --tunnel

# Or using the alias
lhctl --remote-control

# Resume the latest session with tunnel
lhctl -c --tunnel
```

---

### 3. Background Daemon with Remote Control

You can keep LocalHarness running continuously in the background with remote control enabled:

```bash
# Start daemon with tunnel
lhctl daemon start --tunnel

# Check status (displays daemon PID, port, and Remote Control URL)
lhctl daemon status

# Stop daemon and tunnel together
lhctl daemon stop
```

---

### 4. Dedicated Tunnel Management CLI

Manage the tunnel independently from the CLI:

```bash
# Start a tunnel forwarding to the running LocalHarness daemon
lhctl tunnel start

# Forward a custom port (e.g. 8080)
lhctl tunnel start --port 8080

# Check tunnel status and display the terminal QR code
lhctl tunnel status

# Stop the active tunnel
lhctl tunnel stop
```

---

## Remote Terminal Attachment (`lhctl attach --url`)

In addition to the Web UI, you can attach another terminal instance of `lhctl` over the remote tunnel:

```bash
lhctl attach <session-id> \
  --url wss://<random-subdomain>.trycloudflare.com \
  --api-key <api-key>
```

This gives you full interactive TUI controls (token streaming, tool spinners, keybindings) over the public internet without SSH or VPN.

---

## Mobile Web UI Architecture

The embedded Web UI (`internal/server/web/remote_control.html`) is served directly by the LocalHarness HTTP server:

```
[ Mobile Phone Browser ]
         │
         ▼  (HTTPS / WSS with ?key=... & format=json)
[ Cloudflare Argo Edge ]
         │
         ▼  (TryCloudflare gRPC/HTTP2 tunnel)
[ cloudflared daemon ]
         │
         ▼  (HTTP / localhost:<daemon-port>)
[ LocalHarness Daemon Server ]
    ├── GET /           ──> Serves standalone Remote Control SPA
    ├── GET /api/sessions──> Returns list of active & recent sessions
    └── WS  /           ──> Dual-protocol WebSocket (protojson TextMessage)
```

### Dual-Protocol Engine
- **Web Browsers**: Native browser JavaScript cannot set custom HTTP headers during the WebSocket handshake and cannot easily decode binary Protobuf without external libraries. When `format=json` or a browser user-agent is detected, the server automatically streams `protojson`-marshaled messages over standard `websocket.TextMessage`.
- **SDKs & lhctl**: Local SDKs and the `lhctl` client continue using low-overhead binary Protobuf over `websocket.BinaryMessage`.

---

## Security Model

1. **Randomized Ephemeral URLs**: TryCloudflare generates completely unguessable domain names with high entropy (e.g. `https://rapid-quiet-glade.trycloudflare.com`).
2. **256-bit Cryptographic API Keys**: All HTTP endpoints and WebSocket upgrades validate the API key supplied in `?key=` or `?api_key=`. Requests without a valid key receive `401 Unauthorized`.
3. **Workspace Boundary Enforcement**: The LocalHarness daemon enforces strict workspace sandboxing. Tool execution remains constrained to authorized project folders regardless of whether commands originate from the local TUI or the remote web interface.
4. **Instant Revocation**: Run `/tunnel off`, `lhctl tunnel stop`, or click **Stop Tunnel** in the web interface to immediately terminate the `cloudflared` process and close the public ingress.

---

## Binary Resolution Hierarchy

The tunnel manager resolves the `cloudflared` executable in the following order:

1. Environment variable `$CLOUDFLARED_BIN`
2. System `$PATH` (`which cloudflared`)
3. Cached binary at `~/.divmora/localharness/bin/cloudflared`
4. Automatic download from official Cloudflare releases:
   - **macOS (Darwin ARM64/AMD64)**: Fetches and unpacks `.tgz` archive
   - **Linux (ARM64/AMD64)**: Fetches standalone executable
   - **Windows (AMD64)**: Fetches `cloudflared.exe`
