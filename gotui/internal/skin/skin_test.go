package skin

import "testing"

// TestFromReadyUnwrapsTheNestedShape pins entry.py's gateway.ready payload, which
// nests the skin: {"skin": resolve_skin()}.
func TestFromReadyUnwrapsTheNestedShape(t *testing.T) {
	payload := []byte(`{"skin":{"name":"ares","colors":{"ui_accent":"#DD4A3A","banner_title":"#CC0000"},"branding":{"agent_name":"Ares"},"tool_prefix":"┋"}}`)
	got := FromReady(payload)

	if got.Name != "ares" {
		t.Fatalf("Name = %q", got.Name)
	}
	if got.Accent() != "#DD4A3A" {
		t.Fatalf("Accent() = %q", got.Accent())
	}
	if got.Primary() != "#CC0000" {
		t.Fatalf("Primary() = %q", got.Primary())
	}
	if got.AgentName() != "Ares" {
		t.Fatalf("AgentName() = %q", got.AgentName())
	}
	if got.ToolPrefix != "┋" {
		t.Fatalf("ToolPrefix = %q", got.ToolPrefix)
	}
}

// TestFromReadyAcceptsTheFlatShape covers resolve_skin()'s own dict, which is what
// a caller gets if the payload is ever un-nested.
func TestFromReadyAcceptsTheFlatShape(t *testing.T) {
	payload := []byte(`{"name":"silver","colors":{"ui_accent":"#aaaaaa"}}`)
	got := FromReady(payload)

	if got.Name != "silver" || got.Accent() != "#aaaaaa" {
		t.Fatalf("unexpected skin: %+v", got)
	}
}

// TestFromReadyWithEmptyPayloadIsTheDefault covers resolve_skin() returning {} on
// any exception: the UI must still render, in the built-in palette.
func TestFromReadyWithEmptyPayloadIsTheDefault(t *testing.T) {
	for name, payload := range map[string][]byte{
		"nil":         nil,
		"empty":       {},
		"empty json":  []byte(`{}`),
		"garbage":     []byte(`not json`),
		"wrong shape": []byte(`[1,2,3]`),
	} {
		got := FromReady(payload)
		if got.Name != "default" {
			t.Errorf("%s: Name = %q, want default", name, got.Name)
		}
		if got.Accent() == "" || got.Primary() == "" || got.Text() == "" {
			t.Errorf("%s: the default skin must supply every colour", name)
		}
	}
}

// TestFromReadyMergesPartialColors is the guard against a skin that defines one
// key wiping the rest: an omitted colour must inherit, not vanish.
func TestFromReadyMergesPartialColors(t *testing.T) {
	got := FromReady([]byte(`{"skin":{"name":"partial","colors":{"ui_accent":"#123456"}}}`))

	if got.Accent() != "#123456" {
		t.Fatalf("Accent() = %q", got.Accent())
	}
	// banner_title was not supplied, so the default must still be there.
	if got.Primary() != defaultColors[KeyBannerTitle] {
		t.Fatalf("Primary() = %q, want the default %q", got.Primary(), defaultColors[KeyBannerTitle])
	}
}

// TestFromReadyIgnoresBlankValues keeps an empty string from erasing a token.
func TestFromReadyIgnoresBlankValues(t *testing.T) {
	got := FromReady([]byte(`{"skin":{"name":"blank","colors":{"ui_accent":"   ","banner_title":""}}}`))

	if got.Accent() != defaultColors[KeyAccent] {
		t.Fatalf("a blank colour must not replace the default, got %q", got.Accent())
	}
	if got.Primary() != defaultColors[KeyBannerTitle] {
		t.Fatalf("a blank colour must not replace the default, got %q", got.Primary())
	}
}

// TestAccessorsFollowTheSkinEngineFallbackChains pins the chains from
// skin_engine.py so the Go front-end resolves the same colour as the Python CLI.
func TestAccessorsFollowTheSkinEngineFallbackChains(t *testing.T) {
	s := Skin{Colors: map[string]string{
		KeyBannerAccent: "#accent-from-banner",
		KeyBannerTitle:  "#title",
		KeyBannerText:   "#text",
	}}

	// Accent falls back to banner_accent when ui_accent is absent.
	if got := s.Accent(); got != "#accent-from-banner" {
		t.Fatalf("Accent() = %q", got)
	}
	// Error falls back to banner_accent when ui_error is absent.
	if got := s.Error(); got != "#accent-from-banner" {
		t.Fatalf("Error() = %q", got)
	}
	// StatusText falls back to body text.
	if got := s.StatusText(); got != "#text" {
		t.Fatalf("StatusText() = %q", got)
	}
	// StatusStrong falls back to the title.
	if got := s.StatusStrong(); got != "#title" {
		t.Fatalf("StatusStrong() = %q", got)
	}
}

func TestDefaultSkinIsComplete(t *testing.T) {
	d := Default()
	for name, value := range map[string]string{
		"Accent":       d.Accent(),
		"Primary":      d.Primary(),
		"Border":       d.Border(),
		"Text":         d.Text(),
		"Dim":          d.Dim(),
		"Label":        d.Label(),
		"OK":           d.OK(),
		"Error":        d.Error(),
		"Warn":         d.Warn(),
		"StatusBG":     d.StatusBG(),
		"StatusText":   d.StatusText(),
		"StatusStrong": d.StatusStrong(),
	} {
		if value == "" {
			t.Errorf("%s is empty in the default skin", name)
		}
	}
	if d.AgentName() == "" || d.PromptSymbol() == "" {
		t.Fatal("branding must have defaults")
	}
}

// TestDefaultIsIndependentPerCall guards against one caller's mutation leaking
// into the next, which would make skins accumulate across runs.
func TestDefaultIsIndependentPerCall(t *testing.T) {
	first := Default()
	first.Colors[KeyAccent] = "#mutated"

	second := Default()
	if second.Accent() == "#mutated" {
		t.Fatal("Default() returned a shared map; a mutation leaked between calls")
	}
}

// TestBrandingValueFallsBackToTheDefaultSkin covers a skin that renames nothing.
func TestBrandingValueFallsBackToTheDefaultSkin(t *testing.T) {
	s := Skin{Branding: map[string]string{"agent_name": "Custom"}}
	if got := s.AgentName(); got != "Custom" {
		t.Fatalf("AgentName() = %q", got)
	}
	// A key the skin omitted falls back to the built-in default skin.
	if got := s.BrandingValue("prompt_symbol", ">"); got != defaultBranding["prompt_symbol"] {
		t.Fatalf("an omitted key must use the default skin's value, got %q", got)
	}
	// A key no skin defines uses the caller's fallback.
	if got := s.BrandingValue("not_a_real_key", ">"); got != ">" {
		t.Fatalf("an unknown key must use the caller's fallback, got %q", got)
	}
}
