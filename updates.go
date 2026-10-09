package goradio

// Update changes one or more properties of a Gradio output component without
// replacing it. Pass nil for properties to leave their current state unchanged.
// Example: Update("Listo", nil, nil) or Update(nil, boolPtr(false), nil).
func Update(value any, visible, interactive *bool) map[string]any {
	patch := map[string]any{"__type__": "update"}
	if value != nil {
		patch["value"] = value
	}
	if visible != nil {
		patch["visible"] = *visible
	}
	if interactive != nil {
		patch["interactive"] = *interactive
	}
	return patch
}

// A State is user-controlled application state stored by a Go handler. It is
// intentionally not shared across requests automatically. Keep per-session
// state in your own authenticated store until session-backed gr.State lands.
type State[T any] struct{ Value T }

func NewState[T any](value T) *State[T] { return &State[T]{Value: value} }
