package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"deskctl/pkg/bridge"
	"deskctl/pkg/connector"
	"deskctl/pkg/ndmcp"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	// ── App connector ──
	case "detect":
		requireArgs(rest, 1, "detect <app>")
		info, err := connector.Detect(rest[0])
		fatal(err)
		printJSON(info)

	case "patch":
		requireArgs(rest, 1, "patch <app>")
		info, err := connector.Detect(rest[0])
		fatal(err)
		if info.Type != "electron" {
			fatalf("Not an Electron app: %s (type: %s)", rest[0], info.Type)
		}
		fatal(connector.PatchElectron(info.Exe))
		fmt.Println("Patched. Restart the app with: deskctl launch", rest[0])

	// ── Native DevTools MCP commands ──
	case "screenshot", "ss":
		c := startNdmcp()
		defer c.Close()
		a := map[string]any{}
		if v := flag(rest, "--app"); v != "" {
			a["app_name"] = v
		}
		printTool(c.Call("take_screenshot", a))

	case "windows", "lw":
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("list_windows", map[string]any{}))

	case "apps":
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("list_apps", map[string]any{}))

	case "find":
		requireArgs(rest, 1, "find <text> [--app NAME]")
		c := startNdmcp()
		defer c.Close()
		a := map[string]any{"text": rest[0]}
		if v := flag(rest, "--app"); v != "" {
			a["app_name"] = v
		}
		printTool(c.Call("find_text", a))

	case "element":
		requireArgs(rest, 2, "element <x> <y>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("element_at_point", xy(rest)))

	case "ax":
		c := startNdmcp()
		defer c.Close()
		a := map[string]any{}
		if len(rest) > 0 {
			a["app_name"] = rest[0]
		}
		printTool(c.Call("take_ax_snapshot", a))

	case "click":
		requireArgs(rest, 2, "click <x> <y>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("click", xy(rest)))

	case "type":
		requireArgs(rest, 1, "type <text>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("type_text", map[string]any{"text": strings.Join(rest, " ")}))

	case "key":
		requireArgs(rest, 1, "key <key>")
		c := startNdmcp()
		defer c.Close()
		a := parseKey(rest[0])
		printTool(c.Call("press_key", a))

	case "scroll":
		requireArgs(rest, 2, "scroll <x> <y> [--dy -3]")
		c := startNdmcp()
		defer c.Close()
		a := xy(rest)
		if v := flag(rest, "--dy"); v != "" {
			a["delta_y"], _ = strconv.Atoi(v)
		} else {
			a["delta_y"] = -3
		}
		printTool(c.Call("scroll", a))

	case "launch":
		requireArgs(rest, 1, "launch <app>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("launch_app", map[string]any{"app_name": rest[0]}))

	case "focus":
		requireArgs(rest, 1, "focus <app>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("focus_window", map[string]any{"app_name": strings.Join(rest, " ")}))

	// ── CDP commands (browsers, Electron, WebView2) ──
	case "cdp-connect":
		requireArgs(rest, 1, "cdp-connect <port>")
		c := startNdmcp()
		defer c.Close()
		port, _ := strconv.Atoi(rest[0])
		printTool(c.Call("cdp_connect", map[string]any{"port": port}))

	case "cdp-pages":
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("cdp_list_pages", map[string]any{}))

	case "cdp-select":
		requireArgs(rest, 1, "cdp-select <index>")
		c := startNdmcp()
		defer c.Close()
		idx, _ := strconv.Atoi(rest[0])
		printTool(c.Call("cdp_select_page", map[string]any{"page_idx": idx}))

	case "cdp-nav":
		requireArgs(rest, 1, "cdp-nav <url>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("cdp_navigate", map[string]any{"url": rest[0]}))

	case "cdp-snap":
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("cdp_take_snapshot", map[string]any{}))

	case "cdp-click":
		requireArgs(rest, 1, "cdp-click <selector>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("cdp_click", map[string]any{"selector": rest[0]}))

	case "cdp-fill":
		requireArgs(rest, 2, "cdp-fill <selector> <value>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("cdp_fill", map[string]any{"selector": rest[0], "value": strings.Join(rest[1:], " ")}))

	case "cdp-type":
		requireArgs(rest, 1, "cdp-type <text>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("cdp_type_text", map[string]any{"text": strings.Join(rest, " ")}))

	case "cdp-key":
		requireArgs(rest, 1, "cdp-key <key>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("cdp_press_key", map[string]any{"key": rest[0]}))

	case "cdp-eval":
		requireArgs(rest, 1, "cdp-eval <js>")
		c := startNdmcp()
		defer c.Close()
		printTool(c.Call("cdp_evaluate_script", map[string]any{"function": strings.Join(rest, " ")}))

	// ── Background native (PostMessage, no mouse, no focus) ──
	case "bg-target":
		requireArgs(rest, 1, "bg-target <window title>")
		b := startBridge()
		defer b.Close()
		r, _ := b.Send(bridge.Req{Action: "focus_window", Window: strings.Join(rest, " ")})
		printJSON(r)

	case "bg-click":
		b := startBridge()
		defer b.Close()
		req := bridge.Req{Action: "execute_action", Window: flag(rest, "--window")}
		if v := flag(rest, "--id"); v != "" {
			req.Type = "click"
			req.AutoID = v
		} else if len(rest) > 0 {
			req.Type = "click_name"
			req.Name = rest[0]
		}
		r, _ := b.Send(req)
		printJSON(r)

	case "bg-type":
		requireArgs(rest, 1, "bg-type <text>")
		b := startBridge()
		defer b.Close()
		r, _ := b.Send(bridge.Req{Action: "execute_action", Type: "type_text", Text: strings.Join(rest, " ")})
		printJSON(r)

	case "bg-key":
		requireArgs(rest, 1, "bg-key <key>")
		b := startBridge()
		defer b.Close()
		r, _ := b.Send(bridge.Req{Action: "execute_action", Type: "press_key", Key: rest[0]})
		printJSON(r)

	case "bg-scroll":
		b := startBridge()
		defer b.Close()
		dir := "down"
		amt := 3
		if len(rest) > 0 {
			dir = rest[0]
		}
		if len(rest) > 1 {
			amt, _ = strconv.Atoi(rest[1])
		}
		r, _ := b.Send(bridge.Req{Action: "execute_action", Type: "scroll", Dir: dir, Amount: amt})
		printJSON(r)

	// ── Session mode (keeps ndmcp alive for multiple commands) ──
	case "session":
		runSession()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// ── Session mode: reads commands from stdin, keeps ndmcp alive ──
func runSession() {
	c := startNdmcp()
	defer c.Close()

	var b *bridge.Client

	fmt.Fprintln(os.Stderr, "deskctl session started. Send JSON commands, one per line.")
	fmt.Fprintln(os.Stderr, `Format: {"tool": "take_screenshot", "args": {"app_name": "notepad.exe"}}`)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			break
		}

		var cmd struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			fmt.Println(`{"error":"invalid json"}`)
			continue
		}

		// Route bg_* to bridge
		if strings.HasPrefix(cmd.Tool, "bg_") {
			if b == nil {
				var err error
				b, err = startBridgeErr()
				if err != nil {
					fmt.Printf(`{"error":"%s"}`+"\n", err)
					continue
				}
				defer b.Close()
			}
			// Convert to bridge request
			req := bridge.Req{Action: "execute_action"}
			switch cmd.Tool {
			case "bg_target":
				req.Action = "focus_window"
				req.Window, _ = cmd.Args["window"].(string)
			case "bg_click":
				if id, ok := cmd.Args["automation_id"].(string); ok && id != "" {
					req.Type = "click"
					req.AutoID = id
				} else {
					req.Type = "click_name"
					req.Name, _ = cmd.Args["name"].(string)
				}
			case "bg_type":
				req.Type = "type_text"
				req.Text, _ = cmd.Args["text"].(string)
			case "bg_key":
				req.Type = "press_key"
				req.Key, _ = cmd.Args["key"].(string)
			}
			r, _ := b.Send(req)
			j, _ := json.Marshal(r)
			fmt.Println(string(j))
			continue
		}

		// Everything else goes to ndmcp
		result, err := c.Call(cmd.Tool, cmd.Args)
		if err != nil {
			fmt.Printf(`{"error":"%s"}`+"\n", err)
			continue
		}
		// Output text content only
		for _, item := range result.Content {
			if item.Type == "text" {
				fmt.Println(item.Text)
			} else if item.Type == "image" {
				fmt.Printf(`{"image_bytes":%d}`+"\n", len(item.Data))
			}
		}
	}
}

// ── Helpers ──

func startNdmcp() *ndmcp.Client {
	c, err := ndmcp.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return c
}

func startBridge() *bridge.Client {
	b, err := startBridgeErr()
	fatal(err)
	return b
}

func startBridgeErr() (*bridge.Client, error) {
	script := findBridgeScript()
	return bridge.Start("python", script)
}

func findBridgeScript() string {
	// Check relative to executable
	exe, _ := os.Executable()
	candidates := []string{
		filepath.Join(filepath.Dir(exe), "bridge", "bridge.py"),
		filepath.Join(filepath.Dir(exe), "..", "bridge", "bridge.py"),
		filepath.Join(".", "bridge", "bridge.py"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "bridge/bridge.py"
}

func printTool(r *ndmcp.ToolResult, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	for _, item := range r.Content {
		if item.Type == "text" {
			fmt.Println(item.Text)
		} else if item.Type == "image" {
			fmt.Printf("[image: %d bytes base64]\n", len(item.Data))
		}
	}
}

func printJSON(v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

func xy(args []string) map[string]any {
	x, _ := strconv.Atoi(args[0])
	y, _ := strconv.Atoi(args[1])
	return map[string]any{"x": x, "y": y}
}

func parseKey(key string) map[string]any {
	a := map[string]any{}
	parts := strings.Split(strings.ToLower(key), "+")
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "ctrl":
			a["control"] = true
		case "shift":
			a["shift"] = true
		case "alt":
			a["alt"] = true
		}
	}
	a["key"] = parts[len(parts)-1]
	return a
}

func flag(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func requireArgs(args []string, n int, usage string) {
	if len(args) < n {
		fatalf("Usage: deskctl %s", usage)
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `deskctl — Desktop control CLI for AI agents. No LLM inside.

APP DETECTION:
  deskctl detect <app>              Detect app type (electron/webview2/native)
  deskctl patch <app>               Patch Electron app to enable CDP

SCREEN (native-devtools-mcp):
  deskctl screenshot [--app NAME]   Screenshot + OCR with coordinates
  deskctl windows                   List all windows
  deskctl find "text" [--app NAME]  Find text on screen (returns coordinates)
  deskctl element <x> <y>           Inspect UI element at coordinates
  deskctl ax [app]                  Accessibility tree snapshot
  deskctl click <x> <y>             Click at screen coordinates
  deskctl type "text"               Type text
  deskctl key "ctrl+s"              Press key combo
  deskctl scroll <x> <y> [--dy -3] Scroll at position
  deskctl focus "window"            Focus a window
  deskctl launch "app"              Launch an app

CDP (browsers, Electron, WebView2):
  deskctl cdp-connect <port>        Connect to CDP debug port
  deskctl cdp-pages                 List browser tabs
  deskctl cdp-select <index>        Switch to tab
  deskctl cdp-nav <url>             Navigate to URL
  deskctl cdp-snap                  DOM accessibility tree
  deskctl cdp-click "selector"      Click DOM element (no mouse movement)
  deskctl cdp-fill "sel" "value"    Fill input field
  deskctl cdp-type "text"           Type into focused element
  deskctl cdp-key "Enter"           Press key in browser
  deskctl cdp-eval "js code"        Execute JavaScript

BACKGROUND NATIVE (PostMessage, no mouse, no focus):
  deskctl bg-target "window title"  Set target window (no focus change)
  deskctl bg-click "element" [--id ID] [--window W]  Click via PostMessage
  deskctl bg-type "text"            Type via WM_CHAR
  deskctl bg-key "ctrl+a"           Key press via PostMessage
  deskctl bg-scroll down 3          Scroll via PostMessage

SESSION (keeps connections alive):
  deskctl session                   Interactive JSON session`)
}
