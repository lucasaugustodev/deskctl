package ndmcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
	msgID  int
}

type rpcReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct{ Message string } `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
}

type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError"`
}

type ContentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

func Start() (*Client, error) {
	bin := findBinary()
	if bin == "" {
		return nil, fmt.Errorf("native-devtools-mcp not found. Run: npm install -g native-devtools-mcp")
	}

	cmd := exec.Command(bin)
	cmd.Stderr = os.Stderr
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := &Client{cmd: cmd, stdin: stdin, reader: bufio.NewReaderSize(stdout, 10*1024*1024)}
	if err := c.init(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) init() error {
	_, err := c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "deskctl", "version": "1.0.0"},
	})
	if err != nil {
		return err
	}
	notif, _ := json.Marshal(rpcReq{JSONRPC: "2.0", Method: "notifications/initialized"})
	c.stdin.Write(append(notif, '\n'))
	return nil
}

func (c *Client) Call(tool string, args map[string]any) (*ToolResult, error) {
	raw, err := c.send("tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return nil, err
	}
	var r ToolResult
	json.Unmarshal(raw, &r)
	return &r, nil
}

func (c *Client) send(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgID++
	data, _ := json.Marshal(rpcReq{JSONRPC: "2.0", ID: c.msgID, Method: method, Params: params})
	c.stdin.Write(append(data, '\n'))

	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var resp rpcResp
		json.Unmarshal(line, &resp)
		if resp.ID == 0 && resp.Method != "" {
			continue // skip notifications
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("%s", resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *Client) Close() {
	c.stdin.Close()
	done := make(chan struct{})
	go func() { c.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		c.cmd.Process.Kill()
	}
}

func findBinary() string {
	name := "native-devtools-mcp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if runtime.GOOS == "windows" {
		cache := filepath.Join(os.Getenv("LOCALAPPDATA"), "npm-cache", "_npx")
		filepath.Walk(cache, func(path string, info os.FileInfo, _ error) error {
			if info != nil && !info.IsDir() && strings.EqualFold(info.Name(), name) {
				name = path
				return filepath.SkipAll
			}
			return nil
		})
		if filepath.IsAbs(name) {
			return name
		}
	}
	if p, _ := exec.LookPath(name); p != "" {
		return p
	}
	return ""
}
