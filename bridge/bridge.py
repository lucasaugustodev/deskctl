"""
agent-os unified bridge — controls ANY Windows app in background.
Communicates with Go CLI via stdin/stdout newline-delimited JSON.

Uses:
- pywinauto (Win32 backend): send clicks/keys to unfocused windows
- uiautomation: read accessibility tree from any window (native + browser)
- win32gui/PrintWindow: screenshot windows without focus
"""

import sys
import json
import time
import ctypes
import ctypes.wintypes
import base64
import io
import traceback

import pywinauto
from pywinauto import Desktop
from pywinauto.application import Application
from pywinauto.controls.hwndwrapper import HwndWrapper

try:
    from PIL import Image
except ImportError:
    Image = None

import win32gui
import win32ui
import win32con
import win32api
import win32process

# ── Screenshot via PrintWindow (works on unfocused/background windows) ──

def screenshot_window(hwnd):
    """Capture a screenshot of a window even if it's in the background."""
    try:
        # Get window rect
        left, top, right, bottom = win32gui.GetWindowRect(hwnd)
        w = right - left
        h = bottom - top
        if w <= 0 or h <= 0:
            return screenshot_primary_monitor()

        # Create device contexts
        hwnd_dc = win32gui.GetWindowDC(hwnd)
        mfc_dc = win32ui.CreateDCFromHandle(hwnd_dc)
        save_dc = mfc_dc.CreateCompatibleDC()

        bitmap = win32ui.CreateBitmap()
        bitmap.CreateCompatibleBitmap(mfc_dc, w, h)
        save_dc.SelectObject(bitmap)

        # PrintWindow captures even background windows
        result = ctypes.windll.user32.PrintWindow(hwnd, save_dc.GetSafeHdc(), 3)  # PW_RENDERFULLCONTENT=2|PW_CLIENTONLY=1

        if not result:
            # Fallback: BitBlt (requires window to be visible but not focused)
            save_dc.BitBlt((0, 0), (w, h), mfc_dc, (0, 0), win32con.SRCCOPY)

        # Convert to bytes
        bmp_info = bitmap.GetInfo()
        bmp_bits = bitmap.GetBitmapBits(True)

        if Image:
            img = Image.frombuffer('RGB', (bmp_info['bmWidth'], bmp_info['bmHeight']), bmp_bits, 'raw', 'BGRX', 0, 1)
            # Resize to reduce token usage (max 1280px wide)
            if img.width > 1280:
                ratio = 1280 / img.width
                img = img.resize((1280, int(img.height * ratio)), Image.LANCZOS)
            buf = io.BytesIO()
            img.save(buf, format='JPEG', quality=50)
            b64 = base64.b64encode(buf.getvalue()).decode()
        else:
            b64 = ""

        # Cleanup
        win32gui.DeleteObject(bitmap.GetHandle())
        save_dc.DeleteDC()
        mfc_dc.DeleteDC()
        win32gui.ReleaseDC(hwnd, hwnd_dc)

        return b64
    except Exception:
        return screenshot_primary_monitor()


def screenshot_primary_monitor():
    """Fallback: screenshot the primary monitor."""
    try:
        import dxcam
        cam = dxcam.create()
        frame = cam.grab()
        if frame is not None and Image:
            img = Image.fromarray(frame)
            if img.width > 1280:
                ratio = 1280 / img.width
                img = img.resize((1280, int(img.height * ratio)), Image.LANCZOS)
            buf = io.BytesIO()
            img.save(buf, format='JPEG', quality=50)
            del cam
            return base64.b64encode(buf.getvalue()).decode()
    except Exception:
        pass
    return ""


# ── Window management ──

def list_windows():
    """List all visible top-level windows."""
    windows = []
    def callback(hwnd, _):
        if not win32gui.IsWindowVisible(hwnd):
            return True
        title = win32gui.GetWindowText(hwnd)
        if not title:
            return True
        rect = win32gui.GetWindowRect(hwnd)
        _, pid = win32process.GetWindowThreadProcessId(hwnd)
        windows.append({
            "name": title,
            "hwnd": hwnd,
            "pid": pid,
            "rect": {"x": rect[0], "y": rect[1], "width": rect[2]-rect[0], "height": rect[3]-rect[1]},
        })
        return True
    win32gui.EnumWindows(callback, None)
    return windows


def find_window(title_substr):
    """Find a window by title substring (case-insensitive)."""
    title_lower = title_substr.lower()
    for w in list_windows():
        if title_lower in w["name"].lower():
            return w["hwnd"], w["name"]
    return None, None


def focus_window(title_substr):
    """Select a window as the target for subsequent actions. Does NOT bring it to foreground."""
    hwnd, name = find_window(title_substr)
    if not hwnd:
        return {"success": False, "error": f"Window not found: {title_substr}"}
    # Just set it as target — no SetForegroundWindow, truly background
    return {"success": True, "focused_window": name, "hwnd": hwnd}


# ── Accessibility tree via pywinauto ──

def build_tree(hwnd, max_depth=4):
    """Build accessibility tree for a window using pywinauto."""
    try:
        app = Application(backend="uia").connect(handle=hwnd)
        window = app.window(handle=hwnd)
        elements = []
        _walk_tree(window.wrapper_object(), elements, max_depth, 0)
        return compress_tree(elements)
    except Exception as e:
        return []


def _walk_tree(ctrl, elements, max_depth, depth):
    """Recursively walk the control tree."""
    if depth >= max_depth or len(elements) >= 200:
        return

    try:
        props = ctrl.element_info
        name = props.name or ""
        auto_id = props.automation_id or ""
        control_type = props.control_type or ""
        rect = props.rectangle

        # Skip invisible/tiny elements
        if rect:
            w = rect.right - rect.left
            h = rect.bottom - rect.top
            if w < 4 and h < 4:
                return

        # Only include if it has name or automation_id
        if name or auto_id:
            el = {
                "name": name if name else None,
                "automation_id": auto_id if auto_id else None,
                "control_type": control_type,
                "enabled": props.enabled,
            }
            if rect:
                el["rect"] = {"x": rect.left, "y": rect.top, "width": rect.right - rect.left, "height": rect.bottom - rect.top}
            elements.append(el)

        # Recurse into children
        for child in ctrl.children():
            if len(elements) >= 200:
                break
            _walk_tree(child, elements, max_depth, depth + 1)
    except Exception:
        pass


INTERACTIVE_TYPES = {"Button", "Edit", "CheckBox", "ComboBox", "MenuItem", "ListItem",
                     "RadioButton", "Slider", "TabItem", "Hyperlink", "SplitButton", "MenuBar",
                     "Menu", "ToolBar", "Link", "DataItem", "TreeItem"}
LOW_PRIORITY_TYPES = {"Text", "Static", "Separator", "Image", "Group", "Pane", "Custom"}


def compress_tree(elements, max_count=150):
    """Compress tree to max_count elements, prioritizing interactive ones."""
    if len(elements) <= max_count:
        return elements

    interactive = [e for e in elements if e.get("control_type") in INTERACTIVE_TYPES]
    other = [e for e in elements if e.get("control_type") not in INTERACTIVE_TYPES and e.get("control_type") not in LOW_PRIORITY_TYPES]
    low = [e for e in elements if e.get("control_type") in LOW_PRIORITY_TYPES]

    result = interactive[:max_count]
    remaining = max_count - len(result)
    if remaining > 0:
        result.extend(other[:remaining])
        remaining = max_count - len(result)
    if remaining > 0:
        result.extend(low[:remaining])

    return result


# ── Actions (background-capable via SendMessage/PostMessage) ──

def click_element_bg(hwnd, auto_id=None, name=None):
    """Click an element in a background window. Never moves the mouse."""
    try:
        app = Application(backend="uia").connect(handle=hwnd)
        window = app.window(handle=hwnd)

        if auto_id:
            ctrl = window.child_window(auto_id=auto_id).wrapper_object()
        elif name:
            ctrl = window.child_window(title=name, found_index=0).wrapper_object()
        else:
            return {"success": False, "error": "No auto_id or name provided"}

        # 1. Try Invoke pattern (100% background, no mouse)
        try:
            ctrl.invoke()
            return {"success": True}
        except Exception:
            pass

        # 2. Try Toggle pattern (checkboxes, etc)
        try:
            ctrl.toggle()
            return {"success": True}
        except Exception:
            pass

        # 3. Try SelectionItem pattern
        try:
            ctrl.select()
            return {"success": True}
        except Exception:
            pass

        # 4. Send BM_CLICK message (background, no mouse)
        try:
            if ctrl.handle:
                win32gui.SendMessage(ctrl.handle, win32con.BM_CLICK, 0, 0)
                return {"success": True}
        except Exception:
            pass

        # 5. PostMessage WM_LBUTTONDOWN/UP at element center (background, no mouse movement)
        try:
            rect = ctrl.rectangle()
            # Coordinates relative to the control's parent window
            parent_rect = win32gui.GetWindowRect(hwnd)
            cx = (rect.left + rect.right) // 2 - parent_rect[0]
            cy = (rect.top + rect.bottom) // 2 - parent_rect[1]
            lparam = win32api.MAKELONG(cx, cy)
            target = ctrl.handle if ctrl.handle else hwnd
            win32gui.PostMessage(target, win32con.WM_LBUTTONDOWN, win32con.MK_LBUTTON, lparam)
            time.sleep(0.05)
            win32gui.PostMessage(target, win32con.WM_LBUTTONUP, 0, lparam)
            return {"success": True}
        except Exception:
            pass

        return {"success": False, "error": "All click methods failed"}

    except Exception as e:
        return {"success": False, "error": str(e)}


VK_MAP = {
    "enter": 0x0D, "return": 0x0D, "tab": 0x09, "escape": 0x1B, "esc": 0x1B,
    "backspace": 0x08, "delete": 0x2E, "del": 0x2E, "home": 0x24, "end": 0x23,
    "pageup": 0x21, "pagedown": 0x22, "up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
    "space": 0x20, "f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73,
    "f5": 0x74, "f6": 0x75, "f7": 0x76, "f8": 0x77,
    "f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
    "ctrl": 0x11, "control": 0x11, "alt": 0x12, "shift": 0x10,
}

def _get_child_edit(hwnd):
    """Find the first Edit/RichEdit child control for text input."""
    result = [None]
    def callback(child_hwnd, _):
        cls = win32gui.GetClassName(child_hwnd)
        if "Edit" in cls or "RichEdit" in cls or "Scintilla" in cls:
            result[0] = child_hwnd
            return False  # stop
        return True
    try:
        win32gui.EnumChildWindows(hwnd, callback, None)
    except Exception:
        pass
    return result[0] or hwnd


def type_text_bg(hwnd, text):
    """Type text into a background window via WM_CHAR messages (no focus needed)."""
    try:
        target = _get_child_edit(hwnd)
        for ch in text:
            win32gui.PostMessage(target, win32con.WM_CHAR, ord(ch), 0)
            time.sleep(0.01)
        return {"success": True}
    except Exception as e:
        return {"success": False, "error": str(e)}


def press_key_bg(hwnd, key):
    """Press a key combo in a background window via PostMessage (no focus needed)."""
    try:
        target = _get_child_edit(hwnd)
        parts = key.lower().split('+')

        # Collect modifiers and main key
        modifiers = []
        main_key = None
        for part in parts:
            part = part.strip()
            if part in ("ctrl", "control", "alt", "shift"):
                modifiers.append(VK_MAP[part])
            elif part in VK_MAP:
                main_key = VK_MAP[part]
            elif len(part) == 1:
                main_key = ord(part.upper())
            else:
                main_key = VK_MAP.get(part, ord(part[0].upper()) if part else 0)

        if main_key is None:
            return {"success": False, "error": f"Unknown key: {key}"}

        # Press modifiers down
        for vk in modifiers:
            win32gui.PostMessage(target, win32con.WM_KEYDOWN, vk, 0)

        # Press main key
        win32gui.PostMessage(target, win32con.WM_KEYDOWN, main_key, 0)
        time.sleep(0.03)
        win32gui.PostMessage(target, win32con.WM_KEYUP, main_key, 0)

        # Release modifiers
        for vk in reversed(modifiers):
            win32gui.PostMessage(target, win32con.WM_KEYUP, vk, 0)

        return {"success": True}
    except Exception as e:
        return {"success": False, "error": str(e)}


def convert_key(key):
    """Convert 'ctrl+s' format to pywinauto '{VK_CONTROL down}s{VK_CONTROL up}' format."""
    parts = key.lower().split('+')
    modifiers = []
    main_key = ""

    key_map = {
        "enter": "{ENTER}", "return": "{ENTER}", "tab": "{TAB}",
        "escape": "{ESC}", "esc": "{ESC}", "backspace": "{BACKSPACE}",
        "delete": "{DELETE}", "del": "{DELETE}", "home": "{HOME}",
        "end": "{END}", "pageup": "{PGUP}", "pagedown": "{PGDN}",
        "up": "{UP}", "down": "{DOWN}", "left": "{LEFT}", "right": "{RIGHT}",
        "space": "{SPACE}", "f1": "{F1}", "f2": "{F2}", "f3": "{F3}",
        "f4": "{F4}", "f5": "{F5}", "f6": "{F6}", "f7": "{F7}",
        "f8": "{F8}", "f9": "{F9}", "f10": "{F10}", "f11": "{F11}", "f12": "{F12}",
    }
    mod_map = {
        "ctrl": "^", "control": "^", "alt": "%", "shift": "+", "win": "{LWIN}",
    }

    for part in parts:
        part = part.strip()
        if part in mod_map:
            if part == "win":
                modifiers.append("{LWIN}")
            else:
                modifiers.append(mod_map[part])
        elif part in key_map:
            main_key = key_map[part]
        else:
            main_key = part

    if modifiers and main_key:
        # Check for special win key
        if "{LWIN}" in modifiers:
            modifiers.remove("{LWIN}")
            return "{LWIN down}" + "".join(modifiers) + main_key + "{LWIN up}"
        return "".join(modifiers) + main_key
    return main_key or key


def scroll_window(hwnd, direction, amount):
    """Scroll a background window."""
    try:
        delta = 120 if direction == "up" else -120
        for _ in range(amount):
            win32gui.PostMessage(hwnd, win32con.WM_MOUSEWHEEL,
                                win32api.MAKELONG(0, delta & 0xFFFF), 0)
            time.sleep(0.1)
        return {"success": True}
    except Exception as e:
        return {"success": False, "error": str(e)}


# ── Get state ──

def get_state(hwnd=None):
    """Get screenshot + accessibility tree of the target window."""
    if not hwnd:
        # No target set — return window list instead of random foreground screenshot
        windows = list_windows()
        return {
            "success": True,
            "screenshot": "",
            "tree": [{"name": w["name"], "control_type": "Window"} for w in windows[:20]],
            "focused_window": "(no target window set — use focus_window first)",
        }

    title = win32gui.GetWindowText(hwnd) or "Unknown"
    screenshot = screenshot_window(hwnd)
    tree = build_tree(hwnd)

    return {
        "success": True,
        "screenshot": screenshot,
        "tree": tree,
        "focused_window": title,
    }


# ── Main JSON-RPC loop ──

# Track which window we're targeting
current_hwnd = None

def handle_request(req):
    global current_hwnd

    action = req.get("action", "")

    if action == "list_windows":
        windows = list_windows()
        return {"success": True, "tree": [{"name": w["name"], "hwnd": w["hwnd"], "pid": w["pid"]} for w in windows]}

    if action == "focus_window":
        title = req.get("window", "")
        hwnd, name = find_window(title)
        if hwnd:
            current_hwnd = hwnd
            return {"success": True, "focused_window": name}
        return {"success": False, "error": f"Window not found: {title}"}

    if action == "get_state":
        return get_state(current_hwnd)

    if action == "execute_action":
        atype = req.get("type", "")
        window_title = req.get("window", "")

        # Find target window
        hwnd = current_hwnd
        if window_title:
            h, _ = find_window(window_title)
            if h:
                hwnd = h
        if not hwnd:
            hwnd = win32gui.GetForegroundWindow()

        if atype == "click":
            return click_element_bg(hwnd, auto_id=req.get("automation_id"))
        elif atype == "click_name":
            return click_element_bg(hwnd, name=req.get("name"))
        elif atype == "type_text":
            return type_text_bg(hwnd, req.get("text", ""))
        elif atype == "press_key":
            return press_key_bg(hwnd, req.get("key", ""))
        elif atype == "scroll":
            return scroll_window(hwnd, req.get("direction", "down"), req.get("amount", 3))
        elif atype == "focus_window":
            return focus_window(req.get("window", ""))
        else:
            return {"success": False, "error": f"Unknown type: {atype}"}

    return {"success": False, "error": f"Unknown action: {action}"}


def main():
    # Force UTF-8 for stdin/stdout to handle unicode window titles
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    sys.stdin.reconfigure(encoding='utf-8', errors='replace')
    sys.stderr.write("[bridge.py] started, waiting for commands...\n")
    sys.stderr.flush()

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        try:
            req = json.loads(line)
            resp = handle_request(req)
        except Exception as e:
            resp = {"success": False, "error": f"{e}\n{traceback.format_exc()}"}

        # Remove hwnd from output (not JSON serializable as-is for large ints)
        if "tree" in resp and resp["tree"]:
            for item in resp["tree"]:
                if isinstance(item, dict) and "hwnd" in item:
                    item["hwnd"] = str(item["hwnd"])

        sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
