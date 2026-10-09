package goradio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PythonStreamHandler bridges a Python generator to the Gradio SSE queue.
// The callback address is restricted to loopback and authenticated by token.
func PythonStreamHandler(endpoint, token string) StreamHandler {
	client := &http.Client{Timeout: 95 * time.Second, Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		Proxy:       nil,
	}}
	return func(ctx context.Context, values []any, emit func([]any) error) error {
		u, err := url.Parse(endpoint)
		if err != nil {
			return err
		}
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return errors.New("callback streaming solo en HTTP loopback")
		}
		encoded, err := json.Marshal(map[string]any{"data": values})
		if err != nil {
			return err
		}
		streamURL := strings.TrimSuffix(endpoint, "/") + "/stream"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, streamURL, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if session:=sessionFromContext(ctx);session!="" {req.Header.Set("X-Goradio-Session",session)}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("stream Python: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("callback de streaming devolvió HTTP %d", resp.StatusCode)
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		steps := 0
		for scanner.Scan() {
			steps++
			if steps > 10000 {
				return errors.New("demasiadas actualizaciones de streaming")
			}
			var message struct {
				Data  []any  `json:"data"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
				return err
			}
			if message.Error != "" {
				return errors.New(message.Error)
			}
			if message.Data == nil {
				return errors.New("streaming Python devolvió datos vacíos")
			}
			if err := emit(message.Data); err != nil {
				return err
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		if steps == 0 {
			return errors.New("streaming Python no produjo resultados")
		}
		return nil
	}
}
