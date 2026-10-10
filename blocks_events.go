package goradio

import (
	"context"
	"fmt"
)

// customEventForIndex returns an explicitly bound Blocks dependency.
// Standard Submit/Clear, Live, Timer, and autosave keep their existing indices.
func (a *App) customEventForIndex(index int) (EventSpec, Handler, bool) {
	base := 2
	if a.Config.Load {
		base++
	}
	if a.Config.Live {
		base += 2 * len(a.Config.Inputs)
	}
	if a.Config.EverySeconds > 0 {
		base++
	}
	if a.security != nil && a.security.cfg.PersistInputs {
		base += 2 * len(a.Config.Inputs)
	}
	idx := index - base
	if idx < 0 || idx >= len(a.Config.Events) {
		return EventSpec{}, nil, false
	}
	return a.Config.Events[idx], a.eventHandlers[idx], true
}

func (a *App) customEventInputComponents(spec EventSpec) []Component {
	byKey := map[string]Component{}
	for _, c := range a.Config.Inputs {
		byKey[c.Key] = c
	}
	components := make([]Component, 0, len(spec.Inputs))
	for _, key := range spec.Inputs {
		components = append(components, byKey[key])
	}
	return components
}

func (a *App) normalizeCustomEventInputs(index int, raw []any) ([]any, error) {
	spec, _, ok := a.customEventForIndex(index)
	if !ok {
		return nil, failPrediction(404, "EVENTO", "Evento desconocido")
	}
	inputs := a.customEventInputComponents(spec)
	if len(raw) != len(inputs) {
		return nil, failPrediction(400, "ENTRADAS", "Cantidad incorrecta de entradas del evento")
	}
	normalized := make([]any, len(raw))
	for i, c := range inputs {
		v, err := a.validate(c, raw[i])
		if err != nil {
			return nil, failPrediction(400, "VALIDACION", fmt.Sprintf("%s: %v", c.Key, err))
		}
		normalized[i] = v
	}
	return normalized, nil
}

func (a *App) prepareCustomEventOutputs(ctx context.Context, spec EventSpec, values []any) ([]any, error) {
	if len(values) != len(spec.Outputs) {
		return nil, failPrediction(500, "SALIDAS", "Número incorrecto de salidas del evento")
	}
	indexFor := map[string]int{}
	for i, c := range a.Config.Outputs {
		indexFor[c.Key] = i
	}
	full := make([]any, len(a.Config.Outputs))
	for i, key := range spec.Outputs {
		full[indexFor[key]] = values[i]
	}
	prepared, err := a.prepareGradioOutputs(ctx, full)
	if err != nil {
		return nil, err
	}
	result := make([]any, len(spec.Outputs))
	for i, key := range spec.Outputs {
		result[i] = prepared[indexFor[key]]
	}
	return result, nil
}

func (a *App) executeCustomEvent(ctx context.Context, index int, raw []any) ([]any, error) {
	spec, fn, ok := a.customEventForIndex(index)
	if !ok {
		return nil, failPrediction(404, "EVENTO", "Evento desconocido")
	}
	if fn == nil {
		return nil, failPrediction(503, "EVENTO", "El evento no tiene función vinculada")
	}
	normalized, err := a.normalizeCustomEventInputs(index, raw)
	if err != nil {
		return nil, err
	}
	values, err := fn(ctx, normalized)
	if err != nil {
		return nil, err
	}
	return a.prepareCustomEventOutputs(ctx, spec, values)
}

func (a *App) hasStreamingEvents() bool {
	for _, ev := range a.Config.Events {
		if ev.Streaming {
			return true
		}
	}
	return false
}

func (a *App) customStreamForIndex(index int) (EventSpec, StreamHandler, bool) {
	spec, _, ok := a.customEventForIndex(index)
	if !ok || !spec.Streaming {
		return EventSpec{}, nil, false
	}
	base := len(a.Config.Events)
	for i := 0; i < base; i++ {
		if a.Config.Events[i].Target == spec.Target && a.Config.Events[i].Trigger == spec.Trigger {
			return spec, a.eventStreams[i], true
		}
	}
	return EventSpec{}, nil, false
}
