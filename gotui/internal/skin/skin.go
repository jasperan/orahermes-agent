// Package skin models the orahermes-agent skin the gateway announces on its
// first frame.
//
// Why this package exists: this repository already owns its palette. The ink
// TUI does not define colours — it reads them from the gateway's gateway.ready
// event (ui-tui/src/theme.ts), whose payload comes from
// hermes_cli/skin_engine.py:resolve_skin(). A second front-end that painted its
// own hard-coded colours would drift from the CLI the moment a user picked a
// different skin, so this package derives every colour from the same source and
// falls back to the built-in "default" skin only when the gateway could not
// resolve one (resolve_skin() returns {} on any exception).
//
// The default values below are copied from hermes_cli/skin_engine.py's
// _BUILTIN_SKINS["default"] and its fallback chains. They are the last resort,
// not a design of this package's own.
package skin

import (
	"encoding/json"
	"strings"
)

// Colour keys, spelled as skin_engine.py spells them.
const (
	KeyBannerBorder   = "banner_border"
	KeyBannerTitle    = "banner_title"
	KeyBannerAccent   = "banner_accent"
	KeyBannerDim      = "banner_dim"
	KeyBannerText     = "banner_text"
	KeyAccent         = "ui_accent"
	KeyLabel          = "ui_label"
	KeyOK             = "ui_ok"
	KeyError          = "ui_error"
	KeyWarn           = "ui_warn"
	KeyStatusBG       = "status_bar_bg"
	KeyStatusText     = "status_bar_text"
	KeyStatusStrong   = "status_bar_strong"
	KeyStatusDim      = "status_bar_dim"
	KeySessionLabel   = "session_label"
	KeySessionBorder  = "session_border"
	KeyPrompt         = "prompt"
	KeyResponseBorder = "response_border"
)

// defaultColors are the built-in "default" skin's colours.
//
// Source: hermes_cli/skin_engine.py, _BUILTIN_SKINS["default"]["colors"]
// ("Catppuccin Mocha — cohesive dark-tech premium theme").
var defaultColors = map[string]string{
	KeyBannerBorder:   "#45475a",
	KeyBannerTitle:    "#89b4fa",
	KeyBannerAccent:   "#cba6f7",
	KeyBannerDim:      "#6c7086",
	KeyBannerText:     "#cdd6f4",
	KeyAccent:         "#cba6f7",
	KeyLabel:          "#f9e2af",
	KeyOK:             "#a6e3a1",
	KeyError:          "#f38ba8",
	KeyWarn:           "#f9e2af",
	KeyPrompt:         "#cdd6f4",
	KeyResponseBorder: "#89b4fa",
	KeyStatusBG:       "#181825",
	KeySessionLabel:   "#f9e2af",
	KeySessionBorder:  "#6c7086",
}

// defaultBranding are the built-in "default" skin's branding strings.
var defaultBranding = map[string]string{
	"agent_name":     "Hermes Agent",
	"welcome":        "Welcome to Hermes Agent! Type your message or /help for commands.",
	"goodbye":        "Goodbye! ⚕",
	"response_label": " ⚕ Hermes ",
	"prompt_symbol":  "❯",
}

// Skin is one resolved skin.
type Skin struct {
	Name       string            `json:"name"`
	Colors     map[string]string `json:"colors"`
	Branding   map[string]string `json:"branding"`
	ToolPrefix string            `json:"tool_prefix"`
	BannerLogo string            `json:"banner_logo"`
	BannerHero string            `json:"banner_hero"`
}

// Default returns the built-in skin, used when the gateway announced none.
func Default() Skin {
	colors := make(map[string]string, len(defaultColors))
	for key, value := range defaultColors {
		colors[key] = value
	}
	branding := make(map[string]string, len(defaultBranding))
	for key, value := range defaultBranding {
		branding[key] = value
	}
	return Skin{
		Name:     "default",
		Colors:   colors,
		Branding: branding,
		// skin_engine.py's SkinConfig default.
		ToolPrefix: "┊",
	}
}

// FromReady decodes the payload of the gateway.ready event into a Skin.
//
// The payload shape is resolve_skin()'s return value:
//
//	{"name":..., "colors":{...}, "branding":{...}, "banner_logo":..., ...}
//
// An empty or unparseable payload yields Default(), which is exactly what a
// failed resolve_skin() ({} ) should mean visually.
func FromReady(payload []byte) Skin {
	result := Default()
	if len(payload) == 0 {
		return result
	}

	// The payload is resolve_skin()'s dict directly: entry.py nests it under
	// params.payload, and _emit() stores that object as-is.
	var raw struct {
		Name       string            `json:"name"`
		Colors     map[string]string `json:"colors"`
		Branding   map[string]string `json:"branding"`
		ToolPrefix string            `json:"tool_prefix"`
		BannerLogo string            `json:"banner_logo"`
		BannerHero string            `json:"banner_hero"`
		// The gateway.ready payload nests it as {"skin": <resolve_skin()>} — that
		// extra level is entry.py's, so unwrap it when present.
		Nested *struct {
			Name       string            `json:"name"`
			Colors     map[string]string `json:"colors"`
			Branding   map[string]string `json:"branding"`
			ToolPrefix string            `json:"tool_prefix"`
			BannerLogo string            `json:"banner_logo"`
			BannerHero string            `json:"banner_hero"`
		} `json:"skin"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return result
	}

	if raw.Nested != nil {
		raw.Name, raw.Colors, raw.Branding = raw.Nested.Name, raw.Nested.Colors, raw.Nested.Branding
		raw.ToolPrefix, raw.BannerLogo, raw.BannerHero = raw.Nested.ToolPrefix, raw.Nested.BannerLogo, raw.Nested.BannerHero
	}

	if strings.TrimSpace(raw.Name) != "" {
		result.Name = raw.Name
	}
	if strings.TrimSpace(raw.ToolPrefix) != "" {
		result.ToolPrefix = raw.ToolPrefix
	}
	result.BannerLogo = raw.BannerLogo
	result.BannerHero = raw.BannerHero

	// Merge rather than replace: a skin that omits a key must inherit the
	// default, not lose the colour entirely.
	for key, value := range raw.Colors {
		if strings.TrimSpace(value) != "" {
			result.Colors[key] = value
		}
	}
	for key, value := range raw.Branding {
		if strings.TrimSpace(value) != "" {
			result.Branding[key] = value
		}
	}
	return result
}

// Color returns a colour, falling back through the keys given.
//
// Multiple fallbacks are allowed because skin_engine.py does the same thing
// (e.g. status_bad falls back to banner_accent and then to warn), and a skin
// may legitimately define only the newer key.
func (s Skin) Color(keys ...string) string {
	for _, key := range keys {
		if value, ok := s.Colors[key]; ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// BrandingValue returns a branding string, falling back to the default skin and
// then to the caller's fallback.
func (s Skin) BrandingValue(key, fallback string) string {
	if value, ok := s.Branding[key]; ok && strings.TrimSpace(value) != "" {
		return value
	}
	if value, ok := defaultBranding[key]; ok {
		return value
	}
	return fallback
}

// Typed accessors. Each mirrors the fallback chain skin_engine.py uses, so the
// Go front-end and the Python CLI resolve the same colour for the same skin.

// Accent is the general UI accent (menus, selections).
func (s Skin) Accent() string { return s.Color(KeyAccent, KeyBannerAccent, KeyBannerTitle) }

// Primary is the title / response colour.
func (s Skin) Primary() string { return s.Color(KeyBannerTitle, KeyResponseBorder, KeyAccent) }

// Border is the panel border colour.
func (s Skin) Border() string { return s.Color(KeyBannerBorder, KeySessionBorder) }

// Text is the body text colour.
func (s Skin) Text() string { return s.Color(KeyBannerText) }

// Dim is the muted colour.
func (s Skin) Dim() string { return s.Color(KeyBannerDim, KeyStatusDim) }

// Label is the label colour.
func (s Skin) Label() string { return s.Color(KeyLabel, KeyBannerTitle) }

// OK is the success colour.
func (s Skin) OK() string { return s.Color(KeyOK) }

// Error is the failure colour.
func (s Skin) Error() string { return s.Color(KeyError, KeyBannerAccent) }

// Warn is the warning colour.
func (s Skin) Warn() string { return s.Color(KeyWarn) }

// StatusBG is the status bar background.
func (s Skin) StatusBG() string { return s.Color(KeyStatusBG) }

// StatusText is the status bar text colour, falling back to body text.
func (s Skin) StatusText() string { return s.Color(KeyStatusText, KeyBannerText) }

// StatusStrong is the emphasised status colour, falling back to the title.
func (s Skin) StatusStrong() string { return s.Color(KeyStatusStrong, KeyBannerTitle) }

// AgentName is the branded agent name.
func (s Skin) AgentName() string { return s.BrandingValue("agent_name", "Hermes Agent") }

// PromptSymbol is the composer prompt symbol.
func (s Skin) PromptSymbol() string { return s.BrandingValue("prompt_symbol", "❯") }
