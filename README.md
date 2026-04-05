# deskctl

Desktop control CLI for AI agents. No LLM inside — your AI agent calls the commands directly.

Controls any Windows app in the background: **no mouse movement, no focus stealing**.

```
AI Agent (Claude Code, Cline, Cursor)
  │
  ├── deskctl cdp-session 9345        ← web apps (LinkedIn, Discord)
  │     CDP WebSocket → Input.dispatch*  (no mouse, no focus)
  │
  ├── deskctl bg-type "hello"          ← native apps (Notepad, Excel)
  │     PostMessage WM_CHAR              (no mouse, no focus)
  │
  ├── deskctl figma ...                ← Figma (via daemon + Plugin API)
  │     figma.createFrame(), figma.createText()...
  │
  └── deskctl screenshot --app notepad ← any app
        PrintWindow                      (no focus needed)
```

## Install

```bash
git clone https://github.com/lucasaugustodev/deskctl.git
cd deskctl

# Build
go build -o deskctl.exe .

# Python deps (for bg-* native commands)
pip install pywinauto uiautomation Pillow

# native-devtools-mcp (for screenshot/OCR/find commands)
npm install -g native-devtools-mcp

# Node deps (for Figma bridge)
npm install
```

## Quick Start

```bash
# List all windows
deskctl windows

# Screenshot without focusing
deskctl screenshot --app notepad.exe

# Find text via OCR (returns screen coordinates)
deskctl find "Submit" --app LinkedIn

# Type into background Notepad (no mouse, no focus)
deskctl bg-target "Notepad"
deskctl bg-type "Hello World"
deskctl bg-key "ctrl+s"
```

## Figma (Plugin API via daemon)

deskctl controls Figma Desktop through its Plugin API — create frames, text, edit elements, read the tree.

**Requirement:** Figma must have a design file open. The figma-cli daemon connects to the Plugin API sandbox which only exists when a design file is loaded.

### Setup (one time)

```bash
# 1. Clone figma-cli (provides the daemon + Figma patcher)
git clone https://github.com/silships/figma-cli.git
cd figma-cli && npm install && cd ..

# 2. Patch Figma to enable CDP (or use: deskctl patch Figma)
cd figma-cli && node src/index.js connect
# This patches app.asar, restarts Figma, and starts the speed daemon
```

### Before each session

```
1. Open Figma Desktop
2. Open a design file (e.g. click on a project)
3. Start the daemon:
   cd figma-cli && DAEMON_PORT=3456 DAEMON_MODE=auto node src/daemon.js &
4. deskctl is ready to control Figma
```

**Important:** The `figma` Plugin API only exists when a design file is open and loaded. If Figma shows the home/feed/recents page, the daemon will report "Could not find Figma execution context." Open any design file to fix this.

### Figma commands via bridge

```bash
# Read canvas info
echo '{"cmd":"canvas-info"}' | node figma/bridge.mjs

# Get element tree
echo '{"cmd":"tree","depth":2}' | node figma/bridge.mjs

# Create a frame
echo '{"cmd":"create-frame","name":"MyFrame","x":0,"y":0,"w":1080,"h":1350,"fill":"#0D0D12"}' | node figma/bridge.mjs

# Create text
echo '{"cmd":"create-text","text":"Hello","x":60,"y":950,"size":72,"font":"Inter","style":"Bold","fill":"#FFFFFF","parent":"64:2"}' | node figma/bridge.mjs

# Edit text
echo '{"cmd":"set-text","id":"34:20","text":"New Title"}' | node figma/bridge.mjs

# Move element
echo '{"cmd":"move","id":"34:43","y":874}' | node figma/bridge.mjs

# Delete element
echo '{"cmd":"delete","id":"64:2"}' | node figma/bridge.mjs

# Run arbitrary Plugin API code
echo '{"cmd":"eval","code":"figma.currentPage.children.length"}' | node figma/bridge.mjs
```

### Direct daemon calls (without bridge)

```bash
TOKEN=$(cat ~/.figma-ds-cli/.daemon-token)

# Health check
curl -s http://localhost:3456/health -H "X-Daemon-Token: $TOKEN"

# Eval Plugin API
curl -s -X POST http://localhost:3456/exec \
  -H "Content-Type: application/json" \
  -H "X-Daemon-Token: $TOKEN" \
  -d '{"action":"eval","code":"figma.currentPage.name"}'
```

## CDP Commands (Web Apps, Electron, WebView2)

Works with any Chromium-based app: **Chrome, Edge, LinkedIn PWA, Discord, Slack, VS Code, etc.**

Zero mouse movement — uses `Input.dispatchMouseEvent` and `Input.dispatchKeyEvent`.

```bash
# List targets on a debug port
deskctl cdp-list 9345

# Execute JavaScript
deskctl cdp-eval 9345 "document.title"

# Navigate
deskctl cdp-nav 9345 "https://www.linkedin.com/feed/"

# Click DOM element (no mouse movement)
deskctl cdp-click 9345 "button.submit"

# Type text
deskctl cdp-type 9345 "Hello World"

# Fill input field
deskctl cdp-fill 9345 "input[name=search]" "query"

# Screenshot via CDP
deskctl cdp-screenshot 9345

# Target specific page
deskctl cdp-eval 9222 "document.title" --page "Untitled"
```

### CDP Session (persistent connection)

For multi-step workflows, `cdp-session` keeps the WebSocket alive:

```bash
echo '{"cmd":"nav","url":"https://www.linkedin.com/feed/"}
{"cmd":"wait","ms":2000}
{"cmd":"verify","js":"document.title.includes(\"LinkedIn\")"}
{"cmd":"eval","js":"document.body.innerText.slice(0, 500)"}
{"cmd":"click_text","text":"Comentar"}
{"cmd":"wait","ms":1000}
{"cmd":"verify","js":"document.querySelectorAll(\"[contenteditable]\").length > 0"}
{"cmd":"type","text":"Great post!"}
{"cmd":"verify","js":"document.querySelector(\"[contenteditable]\").innerText.length > 5"}' | deskctl cdp-session 9345 --page linkedin
```

Each response: `{"ok":true,"result":"..."}` or `{"ok":false,"error":"..."}`.

| Command | Fields | Description |
|---------|--------|-------------|
| `eval` | `js` | Execute JavaScript, return result |
| `nav` | `url` | Navigate to URL |
| `click` | `selector` | Click by CSS selector (dispatchMouseEvent) |
| `click_text` | `text` | Click visible element by exact text |
| `type` | `text` | Type text (dispatchKeyEvent) |
| `key` | `key` | Press key (Enter, Tab, etc) |
| `fill` | `selector`, `value` | Focus + clear + type into input |
| `screenshot` | — | Capture page as base64 JPEG |
| `wait` | `ms` | Wait N milliseconds |
| `verify` | `js` | Eval JS, fail if falsy |

## Background Native Commands (PostMessage)

For Win32 native apps. **No mouse movement, no focus change.**

```bash
deskctl bg-target "Bloco de Notas"    # set target (no focus)
deskctl bg-type "Hello World"          # WM_CHAR messages
deskctl bg-key "ctrl+a"                # WM_KEYDOWN/UP
deskctl bg-key "delete"
deskctl bg-click "Save" --window "Notepad"  # UIA InvokePattern
deskctl bg-scroll down 3               # WM_MOUSEWHEEL
```

## Electron App Patching

Many Electron apps block `--remote-debugging-port`. deskctl patches `app.asar`:

```bash
deskctl detect Figma    # → {"type":"electron","exe":"..."}
deskctl patch Figma     # patches app.asar (backup created)
```

The patch: `removeSwitch("remote-debugging-port")` → `removeSwitch("remote-debugXing-port")`.

## Finding Debug Ports

| App Type | How to find |
|----------|-------------|
| **WebView2 PWAs** (LinkedIn, WhatsApp) | `deskctl detect LinkedIn` — auto-discovers |
| **Electron** (Figma, Discord) | `deskctl patch <app>` then relaunch |
| **Chrome/Edge** | Launch with `--remote-debugging-port=9222` |

## How AI Agents Use It

Any AI agent calls deskctl via shell:

```bash
# Read a web page
deskctl cdp-eval 9345 "document.body.innerText.slice(0, 2000)"

# Native app interaction
deskctl bg-target "Excel"
deskctl bg-key "ctrl+s"

# Figma automation
echo '{"cmd":"canvas-info"}' | node figma/bridge.mjs
echo '{"cmd":"create-text","text":"Hello","x":60,"y":100,"parent":"1:2"}' | node figma/bridge.mjs

# Multi-step with validation
echo '{"cmd":"nav","url":"https://example.com"}
{"cmd":"verify","js":"document.title.includes(\"Example\")"}
{"cmd":"click","selector":"#login-btn"}
{"cmd":"fill","selector":"#email","value":"user@test.com"}' | deskctl cdp-session 9222
```

## Architecture

```
deskctl.exe (Go)
├── pkg/cdp/        Pure Go CDP client (WebSocket)
│                   Input.dispatchMouseEvent (click without mouse)
│                   Input.dispatchKeyEvent (type without focus)
│                   Runtime.evaluate (run JS)
│
├── pkg/bridge/     Python bridge (PostMessage for native apps)
│                   WM_CHAR, WM_KEYDOWN, WM_LBUTTONDOWN
│
├── pkg/connector/  App detector + Electron patcher
│                   Auto-detect: Electron / WebView2 / Native
│                   Binary patch app.asar for CDP
│
├── pkg/ndmcp/      native-devtools-mcp (screenshot, OCR, find_text)
│
└── figma/          Figma bridge (Node.js → daemon HTTP)
                    figma-cli speed daemon on port 3456
                    Full Plugin API: create, edit, delete, read
```

## Tested Apps

| App | Method | Background | No Mouse | Notes |
|-----|--------|-----------|----------|-------|
| LinkedIn PWA | CDP (port 9345) | yes | yes | WebView2, auto-discovered |
| Figma Desktop | Plugin API (daemon) | yes | yes | Requires design file open |
| Notepad | PostMessage | yes | yes | Any Win32 app works |
| Chrome/Edge | CDP | yes | yes | Launch with --remote-debugging-port |
| Any Electron | CDP (after patch) | yes | yes | deskctl patch <app> |
| Any WebView2 PWA | CDP (auto-discover) | yes | yes | deskctl detect <app> |

## License

MIT
