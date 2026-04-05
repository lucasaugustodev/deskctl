# deskctl

Desktop control CLI for AI agents. No LLM inside — your AI agent calls the commands directly.

Controls any Windows app in the background: **no mouse movement, no focus stealing**.

```
AI Agent (Claude Code, Cline, Cursor)
  │
  ├── deskctl cdp-session 9345        ← web apps (LinkedIn, Figma, Discord)
  │     CDP WebSocket → Input.dispatch*  (no mouse, no focus)
  │
  ├── deskctl bg-type "hello"          ← native apps (Notepad, Excel)
  │     PostMessage WM_CHAR              (no mouse, no focus)
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

## CDP Commands (Web Apps, Electron, WebView2)

Works with any Chromium-based app: **Chrome, Edge, LinkedIn PWA, Figma, Discord, Slack, VS Code, etc.**

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

# Type text (no focus needed)
deskctl cdp-type 9345 "Hello World"

# Fill input field
deskctl cdp-fill 9345 "input[name=search]" "query"

# Screenshot via CDP
deskctl cdp-screenshot 9345

# Target specific page on multi-page apps
deskctl cdp-eval 9222 "document.title" --page "Untitled"
```

### CDP Session (persistent connection)

For multi-step workflows, use `cdp-session` to keep the WebSocket alive:

```bash
deskctl cdp-session 9345 --page linkedin
```

Then pipe JSON commands:

```json
{"cmd":"nav","url":"https://www.linkedin.com/feed/"}
{"cmd":"wait","ms":2000}
{"cmd":"verify","js":"document.title.includes('LinkedIn')"}
{"cmd":"eval","js":"document.body.innerText.slice(0, 500)"}
{"cmd":"click_text","text":"Comentar"}
{"cmd":"wait","ms":1000}
{"cmd":"verify","js":"document.querySelectorAll('[contenteditable]').length > 0"}
{"cmd":"type","text":"Great post!"}
{"cmd":"verify","js":"document.querySelector('[contenteditable]').innerText.length > 5"}
```

Each response is JSON: `{"ok":true,"result":"..."}` or `{"ok":false,"error":"..."}`.

**Available session commands:**

| Command | Fields | Description |
|---------|--------|-------------|
| `eval` | `js` | Execute JavaScript, return result |
| `nav` | `url` | Navigate to URL |
| `click` | `selector` | Click by CSS selector (dispatchMouseEvent) |
| `click_text` | `text` | Click first visible element with exact text |
| `type` | `text` | Type text (dispatchKeyEvent) |
| `key` | `key` | Press key (Enter, Tab, etc) |
| `fill` | `selector`, `value` | Focus + clear + type into input |
| `screenshot` | — | Capture page as base64 JPEG |
| `wait` | `ms` | Wait N milliseconds |
| `verify` | `js` | Eval JS, fail if result is falsy |

## Background Native Commands (PostMessage)

For Win32 native apps — sends messages directly to window handles. **No mouse movement, no focus change.**

```bash
deskctl bg-target "Bloco de Notas"    # set target (no focus)
deskctl bg-type "Hello World"          # WM_CHAR messages
deskctl bg-key "ctrl+a"                # WM_KEYDOWN/UP
deskctl bg-key "delete"
deskctl bg-click "Save" --window "Notepad"  # UIA InvokePattern
deskctl bg-scroll down 3               # WM_MOUSEWHEEL
```

## Electron App Patching

Many Electron apps (Figma, Discord, Slack) block `--remote-debugging-port`. deskctl can patch them:

```bash
# Detect app type
deskctl detect Figma
# → {"type":"electron","exe":"...\\Figma.exe"}

# Patch to enable CDP (modifies app.asar)
deskctl patch Figma

# Restart the app, then connect
deskctl cdp-list 9222
deskctl cdp-eval 9222 "document.title"
```

The patch replaces one character in `app.asar`: `removeSwitch("remote-debugging-port")` → `removeSwitch("remote-debugXing-port")`. A backup is created automatically.

## Finding Debug Ports

| App Type | How to find port |
|----------|-----------------|
| **WebView2 PWAs** (LinkedIn, WhatsApp) | `deskctl detect LinkedIn` — auto-discovers from running processes |
| **Electron** (Figma, Discord) | `deskctl patch <app>` then launch with `--remote-debugging-port=9222` |
| **Chrome/Edge** | Launch with `--remote-debugging-port=9222` |

## How AI Agents Use It

Claude Code, Cline, or any AI agent calls deskctl via shell:

```bash
# Read a web page
deskctl cdp-eval 9345 "document.body.innerText.slice(0, 2000)"

# Interact with native apps
deskctl bg-target "Excel"
deskctl bg-key "ctrl+s"

# Multi-step with validation
echo '{"cmd":"nav","url":"https://example.com"}
{"cmd":"verify","js":"document.title.includes(\"Example\")"}
{"cmd":"click","selector":"#login-btn"}
{"cmd":"fill","selector":"#email","value":"user@test.com"}
{"cmd":"click","selector":"#submit"}
{"cmd":"verify","js":"location.href.includes(\"/dashboard\")"}' | deskctl cdp-session 9222
```

## Architecture

```
deskctl.exe (Go)
├── pkg/cdp/       Pure Go CDP client (WebSocket, no external deps)
│                  Input.dispatchMouseEvent (click without mouse)
│                  Input.dispatchKeyEvent (type without focus)
│                  Runtime.evaluate (run JS)
│                  Page.captureScreenshot
│
├── pkg/bridge/    Python bridge (PostMessage for native apps)
│                  WM_CHAR, WM_KEYDOWN, WM_LBUTTONDOWN
│                  Zero mouse movement, zero focus change
│
├── pkg/connector/ App detector + Electron patcher
│                  Auto-detect: Electron / WebView2 / Native
│                  Binary patch app.asar for CDP access
│
└── pkg/ndmcp/     native-devtools-mcp wrapper (screenshot, OCR, find_text)
```

## Tested Apps

| App | Method | Background | No Mouse |
|-----|--------|-----------|----------|
| LinkedIn PWA | CDP (port 9345) | ✅ | ✅ |
| Figma Desktop | CDP (patched, port 9222) | ✅ | ✅ |
| Notepad | PostMessage | ✅ | ✅ |
| Chrome/Edge | CDP | ✅ | ✅ |
| Any Electron app | CDP (after patch) | ✅ | ✅ |
| Any WebView2 PWA | CDP (auto-discover) | ✅ | ✅ |

## License

MIT
