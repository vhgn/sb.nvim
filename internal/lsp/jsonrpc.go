package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeInvalidParams  = -32602
	codeMethodNotFound = -32601
	codeNotInitialized = -32002
)

type conn struct {
	r  *bufio.Reader
	w  io.Writer
	mu sync.Mutex
}

func (c *conn) read() (*message, error) {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if length >= 0 {
				break
			}
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			if length, err = strconv.Atoi(strings.TrimSpace(v)); err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", v)
			}
		}
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(buf, &m); err != nil {
		return &message{}, nil // unparseable message: skip it
	}
	return &m, nil
}

func (c *conn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// Header and body go out in a single write (one syscall on stdout).
	msg := make([]byte, 0, len(b)+32)
	msg = append(msg, "Content-Length: "...)
	msg = strconv.AppendInt(msg, int64(len(b)), 10)
	msg = append(msg, "\r\n\r\n"...)
	msg = append(msg, b...)
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.w.Write(msg)
	return err
}

func (c *conn) reply(id json.RawMessage, result any, rerr *responseError) error {
	if rerr != nil {
		return c.send(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   *responseError  `json:"error"`
		}{"2.0", id, rerr})
	}
	return c.send(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{"2.0", id, result})
}
