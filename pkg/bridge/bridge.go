package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"
)

type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
}

type Req struct {
	Action string `json:"action"`
	Type   string `json:"type,omitempty"`
	AutoID string `json:"automation_id,omitempty"`
	Name   string `json:"name,omitempty"`
	Window string `json:"window,omitempty"`
	Text   string `json:"text,omitempty"`
	Key    string `json:"key,omitempty"`
	Dir    string `json:"direction,omitempty"`
	Amount int    `json:"amount,omitempty"`
}

type Resp struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Window  string `json:"focused_window,omitempty"`
}

func Start(python, script string) (*Client, error) {
	cmd := exec.Command(python, script)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Client{cmd: cmd, stdin: stdin, reader: bufio.NewReaderSize(stdout, 1024*1024)}, nil
}

func (c *Client) Send(r Req) (*Resp, error) {
	data, _ := json.Marshal(r)
	c.stdin.Write(append(data, '\n'))

	ch := make(chan *Resp, 1)
	go func() {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			ch <- &Resp{Error: err.Error()}
			return
		}
		var resp Resp
		json.Unmarshal(line, &resp)
		ch <- &resp
	}()

	select {
	case r := <-ch:
		return r, nil
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("timeout")
	}
}

func (c *Client) Close() { c.stdin.Close(); c.cmd.Wait() }
