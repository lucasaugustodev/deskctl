// Package cdp provides a lightweight Chrome DevTools Protocol client.
// Connects directly to page-level WebSocket URLs — works for any Electron/WebView2 app.
// No external CDP library needed.
package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

// Target represents a CDP target (page, iframe, worker, etc).
type Target struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// Client is a CDP WebSocket client connected to a single page target.
type Client struct {
	conn     *websocket.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	msgID    atomic.Int64
	pending  map[int64]chan json.RawMessage
	mu       sync.Mutex
	events   chan Event
}

// Event is a CDP event notification.
type Event struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcMsg struct {
	ID     int64           `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params any             `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ListTargets fetches all CDP targets from a debug port.
func ListTargets(port int) ([]Target, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json", port))
	if err != nil {
		return nil, fmt.Errorf("cannot reach port %d: %w", port, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var targets []Target
	json.Unmarshal(body, &targets)
	return targets, nil
}

// FindTarget finds a page target matching a title or URL substring.
func FindTarget(port int, match string) (*Target, error) {
	targets, err := ListTargets(port)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(match)
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		if strings.Contains(strings.ToLower(t.Title), lower) ||
			strings.Contains(strings.ToLower(t.URL), lower) {
			return &t, nil
		}
	}
	// If no match, return first page
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("no page target found matching %q on port %d", match, port)
}

// Connect opens a WebSocket to a specific target.
func Connect(wsURL string) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background())

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	conn.SetReadLimit(32 * 1024 * 1024) // 32MB for large DOMs

	c := &Client{
		conn:    conn,
		ctx:     ctx,
		cancel:  cancel,
		pending: make(map[int64]chan json.RawMessage),
		events:  make(chan Event, 64),
	}
	go c.readLoop()
	return c, nil
}

// ConnectToApp connects to an app by port + page title/URL match.
func ConnectToApp(port int, match string) (*Client, *Target, error) {
	target, err := FindTarget(port, match)
	if err != nil {
		return nil, nil, err
	}
	if target.WebSocketDebuggerURL == "" {
		return nil, nil, fmt.Errorf("target has no WebSocket URL")
	}
	client, err := Connect(target.WebSocketDebuggerURL)
	if err != nil {
		return nil, nil, err
	}
	return client, target, nil
}

// Send sends a CDP command and returns the result.
func (c *Client) Send(method string, params any) (json.RawMessage, error) {
	id := c.msgID.Add(1)

	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	msg := rpcMsg{ID: id, Method: method, Params: params}
	data, _ := json.Marshal(msg)

	err := c.conn.Write(c.ctx, websocket.MessageText, data)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case result := <-ch:
		return result, nil
	case <-time.After(30 * time.Second):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for response to %s", method)
	}
}

func (c *Client) readLoop() {
	for {
		_, data, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		var msg rpcMsg
		json.Unmarshal(data, &msg)

		if msg.ID > 0 {
			c.mu.Lock()
			ch, ok := c.pending[msg.ID]
			if ok {
				delete(c.pending, msg.ID)
			}
			c.mu.Unlock()
			if ok {
				if msg.Error != nil {
					ch <- json.RawMessage(fmt.Sprintf(`{"error":"%s"}`, msg.Error.Message))
				} else {
					ch <- msg.Result
				}
			}
		} else if msg.Method != "" {
			// For notifications, params are in a separate field
			var notif struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			json.Unmarshal(data, &notif)
			select {
			case c.events <- Event{Method: notif.Method, Params: notif.Params}:
			default:
			}
		}
	}
}

// ── High-level helpers ──

// Eval executes JavaScript and returns the result value as string.
func (c *Client) Eval(expression string) (string, error) {
	raw, err := c.Send("Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
	})
	if err != nil {
		return "", err
	}

	// Parse the nested result structure
	var wrapper map[string]json.RawMessage
	json.Unmarshal(raw, &wrapper)

	// Check for error
	if errRaw, ok := wrapper["error"]; ok {
		return "", fmt.Errorf("%s", string(errRaw))
	}

	// Get result.value
	if resultRaw, ok := wrapper["result"]; ok {
		var inner map[string]json.RawMessage
		json.Unmarshal(resultRaw, &inner)
		if valRaw, ok := inner["value"]; ok {
			// Try as string first
			var s string
			if json.Unmarshal(valRaw, &s) == nil {
				return s, nil
			}
			// Return raw JSON for other types
			return string(valRaw), nil
		}
		if typeRaw, ok := inner["type"]; ok {
			var t string
			json.Unmarshal(typeRaw, &t)
			if t == "undefined" {
				return "undefined", nil
			}
		}
	}

	// Fallback: return raw
	return string(raw), nil
}

// EvalInContext executes JS in a specific execution context.
func (c *Client) EvalInContext(contextID int, expression string) (string, error) {
	raw, err := c.Send("Runtime.evaluate", map[string]any{
		"expression":    expression,
		"contextId":     contextID,
		"returnByValue": true,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
	}
	json.Unmarshal(raw, &result)
	b, _ := json.Marshal(result.Result.Value)
	return string(b), nil
}

// Navigate navigates the page to a URL.
func (c *Client) Navigate(url string) error {
	_, err := c.Send("Page.navigate", map[string]any{"url": url})
	return err
}

// Click dispatches a mouse click at coordinates (no physical mouse movement).
func (c *Client) Click(x, y float64) error {
	for _, evType := range []string{"mousePressed", "mouseReleased"} {
		_, err := c.Send("Input.dispatchMouseEvent", map[string]any{
			"type":       evType,
			"x":          x,
			"y":          y,
			"button":     "left",
			"clickCount": 1,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ClickSelector finds an element by CSS selector and clicks it.
func (c *Client) ClickSelector(selector string) error {
	// Get element coordinates via JS
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return null;
		const r = el.getBoundingClientRect();
		return JSON.stringify({x: r.x + r.width/2, y: r.y + r.height/2});
	})()`, selector)

	result, err := c.Eval(js)
	if err != nil {
		return err
	}
	if result == "null" || result == "" {
		return fmt.Errorf("element not found: %s", selector)
	}

	var coords struct{ X, Y float64 }
	json.Unmarshal([]byte(result), &coords)
	return c.Click(coords.X, coords.Y)
}

// Type types text via key events (no focus required on the page).
func (c *Client) Type(text string) error {
	for _, ch := range text {
		_, err := c.Send("Input.dispatchKeyEvent", map[string]any{
			"type": "char",
			"text": string(ch),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// PressKey sends a key press event.
func (c *Client) PressKey(key string) error {
	for _, evType := range []string{"keyDown", "keyUp"} {
		_, err := c.Send("Input.dispatchKeyEvent", map[string]any{
			"type": evType,
			"key":  key,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Fill fills an input element by selector.
func (c *Client) Fill(selector, value string) error {
	// Focus the element first
	js := fmt.Sprintf(`document.querySelector(%q)?.focus()`, selector)
	c.Eval(js)
	time.Sleep(100 * time.Millisecond)

	// Clear existing value
	c.Eval(fmt.Sprintf(`document.querySelector(%q).value = ""`, selector))

	// Type the new value
	return c.Type(value)
}

// Snapshot returns the DOM accessibility snapshot.
func (c *Client) Snapshot() (string, error) {
	raw, err := c.Send("Accessibility.getFullAXTree", nil)
	if err != nil {
		// Fallback: get simplified tree via JS
		return c.Eval(`document.body.innerText.slice(0, 5000)`)
	}
	return string(raw), nil
}

// Screenshot captures the page as base64 JPEG.
func (c *Client) Screenshot() (string, error) {
	raw, err := c.Send("Page.captureScreenshot", map[string]any{
		"format":  "jpeg",
		"quality": 50,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Data string `json:"data"`
	}
	json.Unmarshal(raw, &result)
	return result.Data, nil
}

// Close closes the WebSocket connection.
func (c *Client) Close() {
	c.cancel()
	c.conn.Close(websocket.StatusNormalClosure, "")
}

// FindFigmaContext finds the execution context with the `figma` Plugin API.
func (c *Client) FindFigmaContext() (int, error) {
	// Enable Runtime to get execution contexts
	c.Send("Runtime.enable", nil)
	time.Sleep(1 * time.Second)

	// Drain context events
	var contexts []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	// Collect contexts from events
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev := <-c.events:
			if ev.Method == "Runtime.executionContextCreated" {
				var p struct {
					Context struct {
						ID   int    `json:"id"`
						Name string `json:"name"`
					} `json:"context"`
				}
				json.Unmarshal(ev.Params, &p)
				contexts = append(contexts, p.Context)
			}
		case <-timeout:
			goto CHECK
		}
	}

CHECK:
	// Try each context for figma
	for _, ctx := range contexts {
		result, err := c.EvalInContext(ctx.ID, `typeof figma !== "undefined" ? "yes" : "no"`)
		if err == nil && strings.Contains(result, "yes") {
			return ctx.ID, nil
		}
	}
	return 0, fmt.Errorf("figma context not found in %d contexts", len(contexts))
}
