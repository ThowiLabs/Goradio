package goradio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func BuiltinHandler(mode string) Handler {
	return func(_ context.Context, values []any) ([]any, error) {
		switch mode {
		case "echo":
			if len(values) == 0 {
				return []any{""}, nil
			}
			return []any{values[0]}, nil
		case "uppercase":
			if len(values) == 0 {
				return []any{""}, nil
			}
			return []any{strings.ToUpper(fmt.Sprint(values[0]))}, nil
		default:
			return nil, fmt.Errorf("modo desconocido: %s", mode)
		}
	}
}
func PythonHandler(endpoint, token string) Handler {
	client := &http.Client{Timeout: 95 * time.Second, Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		Proxy:       nil,
	}}
	return func(ctx context.Context, data []any) ([]any, error) {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("callback debe estar en loopback HTTP")
		}
		raw, err := json.Marshal(map[string]any{"data": data})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("callback: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
			return nil, fmt.Errorf("callback HTTP %d: %s", resp.StatusCode, body)
		}
		var result struct {
			Data  []any  `json:"data"`
			Error string `json:"error"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
			return nil, err
		}
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return result.Data, nil
	}
}
