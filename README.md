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
  ├── deskctl figma info               ← Figma (auto-managed daemon)
  │     Plugin API: create, edit, read    (automatic, just have file open)
  │
  └── deskctl screenshot --app notepad ← any app
        PrintWindow                      (no focus needed)
```

## Install

```bash
git clone https://github.com/lucasaugustodev/deskctl.git
cd deskctl

# Build CLI
go build -o deskctl.exe .

# Python deps (for bg-* native commands)
pip install pywinauto uiautomation Pillow

# Node deps (for Figma bridge)
npm install

# Screenshot/OCR engine
npm install -g native-devtools-mcp

# Figma support: clone figma-cli next to deskctl
cd .. && git clone https://github.com/silships/figma-cli.git
cd figma-cli && npm install && cd ../deskctl
```

Directory layout after install:

```
your-folder/
├── deskctl/        ← this repo
└── figma-cli/      ← required for Figma commands
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

## Figma

Controls Figma Desktop via Plugin API. **Just have a design file open — everything else is automatic.**

```bash
# Check connection (auto-starts daemon if needed)
deskctl figma info
# → {"page":"My Page","children":10,"fileKey":"abc123"}

# Element tree
deskctl figma tree
deskctl figma tree --depth 4

# Create elements
deskctl figma create-frame "Slide-10" --x 10620 --y 0 --w 1080 --h 1350 --fill "#0D0D12"
deskctl figma create-text "HELLO WORLD" --x 60 --y 950 --size 72 --font "Inter" --style "Bold" --fill "#FFFFFF" --parent "64:2"

# Edit elements
deskctl figma set-text "34:20" "New Title"
deskctl figma move "34:43" --y 874
deskctl figma delete "64:7"

# Run any Plugin API code
deskctl figma eval "figma.currentPage.name"
deskctl figma eval "figma.currentPage.children.map(c => c.name)"
```

**How it works:** deskctl auto-detects if Figma is running, checks if the figma-cli daemon is alive, starts it if needed, and routes commands through it. The daemon connects to Figma's Plugin API sandbox via CDP.

**Requirements:**
- Figma Desktop open with a design file loaded (not the home/feed page)
- figma-cli cloned next to deskctl (see Install)
- First time only: deskctl patches Figma's `app.asar` to enable CDP (backup created automatically)

## CDP Commands (Web Apps, Electron, WebView2)

Works with any Chromium-based app: **LinkedIn PWA, Chrome, Edge, Discord, Slack, VS Code, etc.**

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

# Target specific page
deskctl cdp-eval 9222 "document.title" --page "Untitled"
```

### CDP Session (persistent connection)

For multi-step workflows — keeps WebSocket alive between commands:

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

## Finding Debug Ports

| App Type | How to find |
|----------|-------------|
| **WebView2 PWAs** (LinkedIn, WhatsApp) | `deskctl detect LinkedIn` — auto-discovers |
| **Electron** (Figma, Discord) | `deskctl patch <app>` then relaunch |
| **Chrome/Edge** | Launch with `--remote-debugging-port=9222` |
| **Figma** | Automatic — `deskctl figma` handles everything |

## How AI Agents Use It

Any AI agent calls deskctl via shell:

```bash
# Figma: create a new slide
deskctl figma create-frame "NewSlide" --x 5000 --w 1080 --h 1350
deskctl figma create-text "Title" --size 72 --parent "64:2"

# LinkedIn: read feed and comment
echo '{"cmd":"nav","url":"https://www.linkedin.com/feed/"}
{"cmd":"wait","ms":3000}
{"cmd":"eval","js":"document.body.innerText.slice(0,1000)"}
{"cmd":"click_text","text":"Comentar"}
{"cmd":"type","text":"Great post!"}' | deskctl cdp-session 9345

# Native app: type in Notepad
deskctl bg-target "Notepad"
deskctl bg-key "ctrl+a"
deskctl bg-type "Hello from AI agent"

# Screenshot + OCR
deskctl screenshot --app notepad.exe
deskctl find "Save" --app Notepad
```

## Architecture

```
deskctl.exe (Go)
│
├── pkg/cdp/        Pure Go CDP client (WebSocket)
│                   Input.dispatchMouseEvent (click without mouse)
│                   Input.dispatchKeyEvent (type without focus)
│                   Runtime.evaluate (run JS)
│
├── pkg/figma/      Auto-managed Figma engine
│                   Detects Figma → starts daemon → connects
│                   Full Plugin API via figma-cli daemon
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
├── bridge/         Python PostMessage backend
│
└── figma/          Node.js Figma bridge (alternative to Go engine)
```

## Tested

| App | Method | Command | No Mouse | No Focus |
|-----|--------|---------|----------|----------|
| Figma | Plugin API (auto-daemon) | `deskctl figma ...` | yes | yes |
| LinkedIn PWA | CDP (port 9345) | `deskctl cdp-session 9345` | yes | yes |
| Notepad | PostMessage | `deskctl bg-type "..."` | yes | yes |
| Chrome/Edge | CDP | `deskctl cdp-eval 9222 "..."` | yes | yes |
| Any Electron | CDP (after patch) | `deskctl patch <app>` | yes | yes |
| Any WebView2 | CDP (auto-discover) | `deskctl detect <app>` | yes | yes |

## License

MIT
