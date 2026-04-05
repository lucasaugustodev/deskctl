package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"time"

	"deskctl/pkg/bridge"
	cdpPkg "deskctl/pkg/cdp"
	"deskctl/pkg/connector"
	"deskctl/pkg/figma"
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

	// ── CDP commands (pure Go, all use stripFlags to avoid arg contamination) ──
	case "cdp-list":
		p := stripFlags(rest)
		requireArgs(p, 1, "cdp-list <port>")
		port, _ := strconv.Atoi(p[0])
		targets, err := cdpPkg.ListTargets(port)
		fatal(err)
		for i, t := range targets {
			if t.Type == "page" {
				fmt.Printf("[%d] %s — %s\n", i, t.Title, t.URL)
			}
		}

	case "cdp-eval":
		p := stripFlags(rest)
		requireArgs(p, 2, "cdp-eval <port> <js>")
		port, _ := strconv.Atoi(p[0])
		js := strings.Join(p[1:], " ")
		client, _, err := cdpPkg.ConnectToApp(port, flag(rest, "--page"))
		fatal(err)
		defer client.Close()
		result, err := client.Eval(js)
		fatal(err)
		fmt.Println(result)

	case "cdp-nav":
		p := stripFlags(rest)
		requireArgs(p, 2, "cdp-nav <port> <url>")
		port, _ := strconv.Atoi(p[0])
		client, _, err := cdpPkg.ConnectToApp(port, flag(rest, "--page"))
		fatal(err)
		defer client.Close()
		fatal(client.Navigate(p[1]))
		fmt.Println("OK")

	case "cdp-click":
		p := stripFlags(rest)
		requireArgs(p, 2, "cdp-click <port> <selector>")
		port, _ := strconv.Atoi(p[0])
		client, _, err := cdpPkg.ConnectToApp(port, flag(rest, "--page"))
		fatal(err)
		defer client.Close()
		fatal(client.ClickSelector(p[1]))
		fmt.Println("OK")

	case "cdp-fill":
		p := stripFlags(rest)
		requireArgs(p, 3, "cdp-fill <port> <selector> <value>")
		port, _ := strconv.Atoi(p[0])
		client, _, err := cdpPkg.ConnectToApp(port, flag(rest, "--page"))
		fatal(err)
		defer client.Close()
		fatal(client.Fill(p[1], strings.Join(p[2:], " ")))
		fmt.Println("OK")

	case "cdp-type":
		p := stripFlags(rest)
		requireArgs(p, 2, "cdp-type <port> <text>")
		port, _ := strconv.Atoi(p[0])
		client, _, err := cdpPkg.ConnectToApp(port, flag(rest, "--page"))
		fatal(err)
		defer client.Close()
		fatal(client.Type(strings.Join(p[1:], " ")))
		fmt.Println("OK")

	case "cdp-key":
		p := stripFlags(rest)
		requireArgs(p, 2, "cdp-key <port> <key>")
		port, _ := strconv.Atoi(p[0])
		client, _, err := cdpPkg.ConnectToApp(port, flag(rest, "--page"))
		fatal(err)
		defer client.Close()
		fatal(client.PressKey(p[1]))
		fmt.Println("OK")

	case "cdp-screenshot":
		p := stripFlags(rest)
		requireArgs(p, 1, "cdp-screenshot <port>")
		port, _ := strconv.Atoi(p[0])
		client, _, err := cdpPkg.ConnectToApp(port, flag(rest, "--page"))
		fatal(err)
		defer client.Close()
		data, err := client.Screenshot()
		fatal(err)
		fmt.Printf("[screenshot: %d bytes]\n", len(data))

	case "cdp-snap":
		p := stripFlags(rest)
		requireArgs(p, 1, "cdp-snap <port>")
		port, _ := strconv.Atoi(p[0])
		client, _, err := cdpPkg.ConnectToApp(port, flag(rest, "--page"))
		fatal(err)
		defer client.Close()
		snap, err := client.Snapshot()
		fatal(err)
		fmt.Println(snap)

	// ── CDP Session (persistent connection, JSON commands via stdin) ──
	case "cdp-session":
		p := stripFlags(rest)
		requireArgs(p, 1, "cdp-session <port> [--page MATCH]")
		port, _ := strconv.Atoi(p[0])
		runCDPSession(port, flag(rest, "--page"))

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

	// ── Figma (auto-manages daemon, requires design file open) ──
	case "figma":
		runFigma(rest)

	// ── Session mode (keeps ndmcp alive for multiple commands) ──
	case "session":
		runSession()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// ── Figma: auto-manages daemon ──
func runFigma(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, `Usage: deskctl figma <command>

Commands:
  deskctl figma status                      Check connection
  deskctl figma info                        Canvas info (page, children count)
  deskctl figma tree [--depth N]            Element tree
  deskctl figma eval "figma.currentPage.name"  Run Plugin API code
  deskctl figma create-frame <name> [--x N] [--y N] [--w N] [--h N] [--fill #hex]
  deskctl figma create-text <text> [--x N] [--y N] [--size N] [--font F] [--style S] [--fill #hex] [--parent ID]
  deskctl figma set-text <id> <text>        Edit text content
  deskctl figma move <id> [--x N] [--y N]   Move element
  deskctl figma delete <id>                 Delete element

Requires: Figma Desktop open with a design file loaded.`)
		os.Exit(1)
	}

	// Auto-start engine (checks Figma, starts daemon if needed)
	eng, err := figma.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	sub := args[0]
	rest := args[1:]
	p := stripFlags(rest)

	switch sub {
	case "status":
		fmt.Println("Connected to Figma")

	case "info":
		r, err := eng.Eval("JSON.stringify({page:figma.currentPage.name,children:figma.currentPage.children.length,fileKey:figma.fileKey})")
		fatal(err)
		fmt.Println(r)

	case "tree":
		depth := 3
		if v := flag(rest, "--depth"); v != "" {
			fmt.Sscanf(v, "%d", &depth)
		}
		r, err := eng.Eval(fmt.Sprintf(`(function(){function w(n,d,m){var o={name:n.name,type:n.type,id:n.id};if(n.x!==undefined){o.x=Math.round(n.x);o.y=Math.round(n.y)}if(n.width!==undefined){o.w=Math.round(n.width);o.h=Math.round(n.height)}if(n.characters)o.text=n.characters.slice(0,80);if(d<m&&n.children)o.children=n.children.map(function(c){return w(c,d+1,m)});return o}return JSON.stringify(w(figma.currentPage,0,%d))})()`, depth))
		fatal(err)
		fmt.Println(r)

	case "eval":
		requireArgs(p, 1, "figma eval <code>")
		r, err := eng.Eval(strings.Join(p, " "))
		fatal(err)
		fmt.Println(r)

	case "create-frame":
		name := "Frame"
		if len(p) > 0 { name = p[0] }
		x := flag(rest, "--x"); if x == "" { x = "0" }
		y := flag(rest, "--y"); if y == "" { y = "0" }
		w := flag(rest, "--w"); if w == "" { w = "1080" }
		h := flag(rest, "--h"); if h == "" { h = "1350" }
		fill := flag(rest, "--fill"); if fill == "" { fill = "#0D0D12" }
		fr := parseFloat(fill[1:3]) / 255; fg := parseFloat(fill[3:5]) / 255; fb := parseFloat(fill[5:7]) / 255
		code := fmt.Sprintf(`(function(){var f=figma.createFrame();f.name=%q;f.resize(%s,%s);f.x=%s;f.y=%s;f.fills=[{type:"SOLID",color:{r:%f,g:%f,b:%f}}];figma.currentPage.appendChild(f);return JSON.stringify({id:f.id,name:f.name,x:f.x,y:f.y})})()`,
			name, w, h, x, y, fr, fg, fb)
		r, err := eng.Eval(code)
		fatal(err)
		fmt.Println(r)

	case "create-text":
		requireArgs(p, 1, "figma create-text <text>")
		text := p[0]
		x := flag(rest, "--x"); if x == "" { x = "0" }
		y := flag(rest, "--y"); if y == "" { y = "0" }
		size := flag(rest, "--size"); if size == "" { size = "48" }
		font := flag(rest, "--font"); if font == "" { font = "Inter" }
		style := flag(rest, "--style"); if style == "" { style = "Bold" }
		fill := flag(rest, "--fill"); if fill == "" { fill = "#FFFFFF" }
		parent := flag(rest, "--parent")
		fr := parseFloat(fill[1:3]) / 255; fg := parseFloat(fill[3:5]) / 255; fb := parseFloat(fill[5:7]) / 255
		parentCode := ""
		if parent != "" {
			parentCode = fmt.Sprintf(`var p=figma.getNodeById(%q);if(p)p.appendChild(t);`, parent)
		}
		code := fmt.Sprintf(`(async()=>{await figma.loadFontAsync({family:%q,style:%q});var t=figma.createText();t.fontName={family:%q,style:%q};t.fontSize=%s;t.characters=%q;t.fills=[{type:"SOLID",color:{r:%f,g:%f,b:%f}}];t.x=%s;t.y=%s;%sreturn JSON.stringify({id:t.id,text:t.characters.slice(0,50)})})()`,
			font, style, font, style, size, text, fr, fg, fb, x, y, parentCode)
		r, err := eng.Eval(code)
		fatal(err)
		fmt.Println(r)

	case "set-text":
		requireArgs(p, 2, "figma set-text <id> <text>")
		code := fmt.Sprintf(`(async()=>{var n=figma.getNodeById(%q);if(!n)return JSON.stringify({error:"not found"});await figma.loadFontAsync(n.fontName);n.characters=%q;return JSON.stringify({id:n.id,text:n.characters.slice(0,50)})})()`, p[0], strings.Join(p[1:], " "))
		r, err := eng.Eval(code)
		fatal(err)
		fmt.Println(r)

	case "move":
		requireArgs(p, 1, "figma move <id> [--x N] [--y N]")
		xCode := ""; if v := flag(rest, "--x"); v != "" { xCode = fmt.Sprintf("n.x=%s;", v) }
		yCode := ""; if v := flag(rest, "--y"); v != "" { yCode = fmt.Sprintf("n.y=%s;", v) }
		code := fmt.Sprintf(`(function(){var n=figma.getNodeById(%q);if(!n)return JSON.stringify({error:"not found"});%s%sreturn JSON.stringify({id:n.id,x:Math.round(n.x),y:Math.round(n.y)})})()`, p[0], xCode, yCode)
		r, err := eng.Eval(code)
		fatal(err)
		fmt.Println(r)

	case "delete":
		requireArgs(p, 1, "figma delete <id>")
		code := fmt.Sprintf(`(function(){var n=figma.getNodeById(%q);if(!n)return JSON.stringify({error:"not found"});var nm=n.name;n.remove();return JSON.stringify({deleted:nm})})()`, p[0])
		r, err := eng.Eval(code)
		fatal(err)
		fmt.Println(r)

	default:
		fmt.Fprintf(os.Stderr, "Unknown figma command: %s\n", sub)
		os.Exit(1)
	}
}

func parseFloat(hexPair string) float64 {
	var n int
	fmt.Sscanf(hexPair, "%x", &n)
	return float64(n)
}

// ── CDP Session: persistent connection, JSON commands via stdin ──
// Input: {"cmd":"eval","js":"document.title"}
// Input: {"cmd":"nav","url":"https://..."}
// Input: {"cmd":"click","selector":"button.submit"}
// Input: {"cmd":"click_text","text":"Comentar"}
// Input: {"cmd":"type","text":"hello world"}
// Input: {"cmd":"key","key":"Enter"}
// Input: {"cmd":"fill","selector":"input","value":"text"}
// Input: {"cmd":"screenshot"}
// Input: {"cmd":"wait","ms":2000}
// Input: {"cmd":"verify","js":"document.querySelector('.editor') !== null"}
// Output: {"ok":true,"result":"..."} or {"ok":false,"error":"..."}
func runCDPSession(port int, pageMatch string) {
	client, target, err := cdpPkg.ConnectToApp(port, pageMatch)
	if err != nil {
		fmt.Printf(`{"ok":false,"error":"connect: %s"}`+"\n", err)
		os.Exit(1)
	}
	defer client.Close()
	fmt.Fprintf(os.Stderr, "cdp-session: connected to %s (%s)\n", target.Title, target.URL)

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			break
		}

		var cmd struct {
			Cmd      string `json:"cmd"`
			JS       string `json:"js,omitempty"`
			URL      string `json:"url,omitempty"`
			Selector string `json:"selector,omitempty"`
			Text     string `json:"text,omitempty"`
			Value    string `json:"value,omitempty"`
			Key      string `json:"key,omitempty"`
			Ms       int    `json:"ms,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			fmt.Printf(`{"ok":false,"error":"bad json: %s"}`+"\n", err)
			continue
		}

		var result string
		var cmdErr error

		switch cmd.Cmd {
		case "eval":
			result, cmdErr = client.Eval(cmd.JS)

		case "nav", "navigate":
			cmdErr = client.Navigate(cmd.URL)
			if cmdErr == nil {
				time.Sleep(1 * time.Second)
				result, _ = client.Eval("document.title")
			}

		case "click":
			cmdErr = client.ClickSelector(cmd.Selector)

		case "click_text":
			// Click first visible element containing text
			js := fmt.Sprintf(`(function(){
				var els=document.querySelectorAll('button,a,span,[role=button]');
				for(var i=0;i<els.length;i++){
					if(els[i].offsetHeight>0 && (els[i].innerText||'').trim()===%q){
						var r=els[i].getBoundingClientRect();
						return JSON.stringify({x:r.x+r.width/2,y:r.y+r.height/2});
					}
				}
				return 'null';
			})()`, cmd.Text)
			var coordStr string
			coordStr, cmdErr = client.Eval(js)
			if cmdErr == nil && coordStr != "null" && coordStr != "" {
				var coords struct{ X, Y float64 }
				json.Unmarshal([]byte(coordStr), &coords)
				cmdErr = client.Click(coords.X, coords.Y)
			} else if cmdErr == nil {
				cmdErr = fmt.Errorf("element with text %q not found", cmd.Text)
			}

		case "type":
			cmdErr = client.Type(cmd.Text)

		case "key":
			cmdErr = client.PressKey(cmd.Key)

		case "fill":
			cmdErr = client.Fill(cmd.Selector, cmd.Value)

		case "screenshot":
			var data string
			data, cmdErr = client.Screenshot()
			if cmdErr == nil {
				result = fmt.Sprintf("%d bytes", len(data))
			}

		case "wait":
			ms := cmd.Ms
			if ms <= 0 { ms = 1000 }
			time.Sleep(time.Duration(ms) * time.Millisecond)
			result = fmt.Sprintf("waited %dms", ms)

		case "verify":
			// Eval JS expression and check if result is truthy
			result, cmdErr = client.Eval(cmd.JS)
			if cmdErr == nil && (result == "false" || result == "null" || result == "undefined" || result == "0" || result == "") {
				cmdErr = fmt.Errorf("verification failed: %s returned %q", cmd.JS, result)
			}

		default:
			cmdErr = fmt.Errorf("unknown cmd: %s", cmd.Cmd)
		}

		if cmdErr != nil {
			errMsg := strings.ReplaceAll(cmdErr.Error(), `"`, `'`)
			fmt.Printf(`{"ok":false,"error":"%s"}`+"\n", errMsg)
		} else {
			if result == "" {
				result = "true"
			}
			resultJSON, _ := json.Marshal(result)
			fmt.Printf(`{"ok":true,"result":%s}`+"\n", string(resultJSON))
		}
		os.Stdout.Sync()
	}
}

// ── Session mode: keeps ndmcp alive for multiple commands ──
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

// stripFlags removes --flag value pairs from args, returns remaining positional args.
func stripFlags(args []string) []string {
	var result []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") && i+1 < len(args) {
			i++ // skip flag value
		} else {
			result = append(result, args[i])
		}
	}
	return result
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
