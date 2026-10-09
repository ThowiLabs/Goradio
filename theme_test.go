package goradio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestThemeConfigurationGo(t *testing.T) {
	for _, bad := range []Theme{
		{PrimaryColor: "red"}, {PrimaryColor: "#fff"}, {SecondaryColor: "#ffffff; color:red"},
		{SecondaryColor: "not-a-color"},
	} {
		if _, err := New(Config{Theme: &bad, Outputs: []Component{{Type: "textbox", Label: "Result"}}},
			func(context.Context, []any) ([]any, error) { return []any{"ok"}, nil }, ""); err == nil {
			t.Fatalf("Accepted invalid theme %+v", bad)
		}
	}
	base := Config{
		Outputs: []Component{{Type: "textbox", Label: "Result"}},
		Theme:   &Theme{PrimaryColor: "#8654cc", SecondaryColor: "#295d9e"},
	}
	app, err := New(base, func(context.Context, []any) ([]any, error) { return []any{"ok"}, nil }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	app.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/theme.css", nil))
	if rr.Code != 200 {
		t.Fatalf("theme.css: %d", rr.Code)
	}
	css := rr.Body.String()
	for _, term := range []string{"--primary-500: #8654cc", "--secondary-500: #295d9e", ":root .dark"} {
		if !strings.Contains(css, term) {
			t.Errorf("Theme lost %s", term)
		}
	}
	if strings.Contains(css, "<script") || strings.Contains(css, ".gd-theme-controls") {
		t.Fatal("Unexpected editor in CSS")
	}
}
func TestDashboardThemeIsSharedWithoutVisitorEditor(t *testing.T) {
	d, err := NewDashboard("App")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	h := func(context.Context, []any) ([]any, error) { return []any{"ok"}, nil }
	for _, path := range []string{"/", "/segunda"} {
		if err := d.Add(Window{Path: path, Title: path, Config: Config{
			Outputs: []Component{{Type: "textbox", Label: "Salida"}},
		}, Handler: h}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.SetTheme(Theme{PrimaryColor: "#cc7831"}); err != nil {
		t.Fatal(err)
	}
	router := d.Routes()
	for _, path := range []string{"/", "/segunda"} {
		r := httptest.NewRequest("GET", path, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, r)
		for _, forbidden := range []string{"gd-theme-color", "gd-theme-secondary", "gd-theme-reset"} {
			if strings.Contains(rr.Body.String(), forbidden) {
				t.Errorf("Production UI has theme editor %s", forbidden)
			}
		}
		rr = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/theme.css", nil)
		r.Header.Set("X-Goradio-Page", path)
		router.ServeHTTP(rr, r)
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), "--primary-500: #cc7831") {
			t.Fatalf("Page %s lost theme: %d", path, rr.Code)
		}
	}
}
