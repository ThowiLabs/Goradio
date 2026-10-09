package goradio

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// SessionState is a bounded JSON store scoped to one Gradio browser session.
// SessionHash is created by the original Gradio frontend. Do not put passwords,
// durable business records or other secrets in transient application state.
type sessionBucket struct {
	updated time.Time
	entries map[string]json.RawMessage
}
type gradioSessionKey struct{}

func withGradioSession(ctx context.Context, session string) context.Context {
	if !gradioSessionHash.MatchString(session) {
		return ctx
	}
	return context.WithValue(ctx, gradioSessionKey{}, session)
}
func sessionFromContext(ctx context.Context) string {
	v, _ := ctx.Value(gradioSessionKey{}).(string)
	return v
}

// StateGet decodes an item from the current browser session into dst.
// It returns false for missing/expired keys or requests lacking a session.
func (a *App) StateGet(ctx context.Context, key string, dst any) bool {
	session := sessionFromContext(ctx)
	if session == "" || key == "" || len(key) > 128 || dst == nil {
		return false
	}
	a.sessionMu.Lock()
	bucket := a.sessions[session]
	var raw []byte
	if bucket != nil && time.Since(bucket.updated) < 30*time.Minute {
		raw = append([]byte{}, bucket.entries[key]...)
	}
	a.sessionMu.Unlock()
	if len(raw) == 0 {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

// StateSet stores a JSON-serializable value for the current browser session.
// Limits: 64 keys and 64 KiB per item, 1024 active sessions, 30-minute TTL.
func (a *App) StateSet(ctx context.Context, key string, value any) error {
	session := sessionFromContext(ctx)
	if session == "" {
		return errors.New("no hay sesión Gradio activa")
	}
	if key == "" || len(key) > 128 {
		return errors.New("clave de estado inválida")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return errors.New("estado excede 64 KiB")
	}
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	now := time.Now()
	for id, b := range a.sessions {
		if now.Sub(b.updated) > 30*time.Minute {
			delete(a.sessions, id)
		}
	}
	b := a.sessions[session]
	if b == nil {
		if len(a.sessions) >= 1024 {
			return errors.New("demasiadas sesiones")
		}
		b = &sessionBucket{entries: map[string]json.RawMessage{}}
		a.sessions[session] = b
	}
	if _, ok := b.entries[key]; !ok && len(b.entries) >= 64 {
		return errors.New("demasiados campos de estado")
	}
	b.entries[key] = append(json.RawMessage{}, data...)
	b.updated = now
	return nil
}
