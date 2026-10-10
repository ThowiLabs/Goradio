package goradio

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// LayoutNode composes native Gradio 6.30 layout containers.
// Type is row, column, group, accordion, tabs, tab, or component.
// Component references the stable Key of a configured input or output.
type LayoutNode struct {
	Type      string       `json:"type"`
	Label     string       `json:"label,omitempty"`
	Component string       `json:"component,omitempty"`
	Key       string       `json:"key,omitempty"`
	Open      bool         `json:"open,omitempty"`
	Children  []LayoutNode `json:"children,omitempty"`
}

var layoutKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func validateBlocksLayout(cfg Config) error {
	if cfg.Layout == nil {
		if len(cfg.Events) > 0 {
			return errors.New("eventos dirigidos requieren un layout Blocks")
		}
		return nil
	}
	used := make(map[string]bool)
	keys := make(map[string]bool)
	buttons := make(map[string]bool)
	for _, component := range append(append([]Component{}, cfg.Inputs...), cfg.Outputs...) {
		if !layoutKey.MatchString(component.Key) {
			return fmt.Errorf("Blocks requiere clave única para cada componente: %q", component.Key)
		}
		if keys[component.Key] {
			return fmt.Errorf("clave duplicada en Blocks: %q", component.Key)
		}
		keys[component.Key] = true
	}
	count := 0
	var visit func(LayoutNode, string, int) error
	visit = func(n LayoutNode, parent string, depth int) error {
		count++
		if count > 128 || depth > 12 {
			return errors.New("layout excede límite de 128 nodos o 12 niveles")
		}
		switch n.Type {
		case "component":
			if n.Component == "" || !keys[n.Component] {
				return fmt.Errorf("referencia desconocida: %q", n.Component)
			}
			if used[n.Component] {
				return fmt.Errorf("componente duplicado en layout: %q", n.Component)
			}
			if len(n.Children) > 0 || n.Label != "" || n.Key != "" {
				return errors.New("referencia de componente no admite hijos ni etiqueta")
			}
			used[n.Component] = true
		case "button":
			if !layoutKey.MatchString(n.Key) || keys[n.Key] || buttons[n.Key] || len(n.Children) > 0 || n.Component != "" || strings.TrimSpace(n.Label) == "" {
				return errors.New("botón inválido o duplicado")
			}
			buttons[n.Key] = true
		case "row", "column", "group", "accordion", "tabs", "tab":
			if n.Component != "" || n.Key != "" {
				return errors.New("contenedores no admiten referencia ni key")
			}
			if n.Type == "tab" && parent != "tabs" {
				return errors.New("tab debe estar dentro de tabs")
			}
			if n.Type == "tabs" && parent == "tabs" {
				return errors.New("tabs anidados directamente no permitidos")
			}
			if n.Type == "accordion" || n.Type == "tab" {
				if strings.TrimSpace(n.Label) == "" || len([]rune(n.Label)) > 90 {
					return errors.New("accordion y tab requieren etiqueta corta")
				}
			}
			if len(n.Children) == 0 {
				return fmt.Errorf("%s no puede estar vacío", n.Type)
			}
			if n.Type == "tabs" {
				for _, child := range n.Children {
					if child.Type != "tab" {
						return errors.New("tabs solo admite nodos tab")
					}
				}
			}
			if n.Type != "tabs" {
				for _, child := range n.Children {
					if child.Type == "tab" {
						return errors.New("tab fuera de tabs")
					}
				}
			}
			for _, child := range n.Children {
				if err := visit(child, n.Type, depth+1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("contenedor de Blocks desconocido: %q", n.Type)
		}
		return nil
	}
	if err := visit(*cfg.Layout, "", 0); err != nil {
		return err
	}
	for key := range keys {
		if !used[key] {
			return fmt.Errorf("componente sin ubicar en layout: %q", key)
		}
	}
	for _, ev := range cfg.Events {
		if err := validateEventSpec(ev, cfg, buttons); err != nil {
			return err
		}
	}
	return nil
}

// Helpers create native Gradio containers without a parallel HTML framework.
func Row(children ...LayoutNode) LayoutNode    { return LayoutNode{Type: "row", Children: children} }
func Column(children ...LayoutNode) LayoutNode { return LayoutNode{Type: "column", Children: children} }
func Group(children ...LayoutNode) LayoutNode  { return LayoutNode{Type: "group", Children: children} }
func Tabs(children ...LayoutNode) LayoutNode   { return LayoutNode{Type: "tabs", Children: children} }
func Tab(label string, children ...LayoutNode) LayoutNode {
	return LayoutNode{Type: "tab", Label: label, Children: children}
}
func Accordion(label string, open bool, children ...LayoutNode) LayoutNode {
	return LayoutNode{Type: "accordion", Label: label, Open: open, Children: children}
}
func Ref(key string) LayoutNode { return LayoutNode{Type: "component", Component: key} }

func Button(key, label string) LayoutNode { return LayoutNode{Type: "button", Key: key, Label: label} }

// EventSpec binds one native Gradio UI event to a selection of input and output keys.
type EventSpec struct {
	Target  string   `json:"target"`
	Trigger string   `json:"trigger"`
	Inputs  []string `json:"inputs"`
	Outputs []string `json:"outputs"`
}

func validateEventSpec(ev EventSpec, cfg Config, buttons map[string]bool) error {
	if !layoutKey.MatchString(ev.Target) {
		return errors.New("target inválido")
	}
	keys := map[string]string{}
	for _, c := range cfg.Inputs {
		keys[c.Key] = "input"
	}
	for _, c := range cfg.Outputs {
		keys[c.Key] = "output"
	}
	if buttons[ev.Target] {
		if ev.Trigger != "click" {
			return errors.New("botón solo permite click")
		}
	} else {
		if keys[ev.Target] == "" {
			return fmt.Errorf("target de evento no encontrado: %q", ev.Target)
		}
		switch ev.Trigger {
		case "change", "input", "submit", "select":
		default:
			return fmt.Errorf("trigger desconocido: %q", ev.Trigger)
		}
	}
	if len(ev.Outputs) == 0 || len(ev.Inputs) > 32 || len(ev.Outputs) > 32 {
		return errors.New("evento necesita salidas válidas")
	}
	inputKeys := map[string]bool{}
	for _, c := range cfg.Inputs {
		inputKeys[c.Key] = true
	}
	outputKeys := map[string]bool{}
	for _, c := range cfg.Outputs {
		outputKeys[c.Key] = true
	}
	seen := map[string]bool{}
	for _, key := range ev.Inputs {
		if !inputKeys[key] || seen[key] {
			return fmt.Errorf("input desconocido o duplicado: %q", key)
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, key := range ev.Outputs {
		if !outputKeys[key] || seen[key] {
			return fmt.Errorf("output desconocido o duplicado: %q", key)
		}
		seen[key] = true
	}
	return nil
}
