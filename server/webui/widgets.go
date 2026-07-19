package webui

import (
	"encoding/json"
	"strings"
)

// The Custom ("widgets") dashboard layout: the five dashboard panels render
// in a user-arranged order with optional hiding, stored per user in
// user_prefs under this key as {"order":[ids],"hidden":[ids]}.
const widgetPrefKey = "dashboard.widgets"

// defaultWidgetOrder is the canonical widget id set in default order. New
// widgets append here; stored configs that predate them show them at the end.
var defaultWidgetOrder = []string{"stats", "games", "activity", "devices", "pulse"}

type widgetConfig struct {
	Order  []string `json:"order"`
	Hidden []string `json:"hidden,omitempty"`
}

func validWidgetID(id string) bool {
	for _, w := range defaultWidgetOrder {
		if id == w {
			return true
		}
	}
	return false
}

// normalize drops unknown/duplicate ids and appends any missing ids in
// default order, so a stored config always covers exactly the known set.
func (c widgetConfig) normalize() widgetConfig {
	seen := map[string]bool{}
	out := widgetConfig{}
	for _, id := range c.Order {
		if validWidgetID(id) && !seen[id] {
			seen[id] = true
			out.Order = append(out.Order, id)
		}
	}
	for _, id := range defaultWidgetOrder {
		if !seen[id] {
			out.Order = append(out.Order, id)
		}
	}
	hiddenSeen := map[string]bool{}
	for _, id := range c.Hidden {
		if validWidgetID(id) && !hiddenSeen[id] {
			hiddenSeen[id] = true
			out.Hidden = append(out.Hidden, id)
		}
	}
	return out
}

// parseWidgetConfig reads a stored pref value; anything unusable falls back
// to the default arrangement (rendering must never fail on a bad pref).
func parseWidgetConfig(raw string) widgetConfig {
	if strings.TrimSpace(raw) == "" {
		return widgetConfig{Order: append([]string(nil), defaultWidgetOrder...)}
	}
	var c widgetConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return widgetConfig{Order: append([]string(nil), defaultWidgetOrder...)}
	}
	return c.normalize()
}

// visibleOrder returns the ordered ids with hidden ones removed. Hiding
// everything falls back to the full default order — an all-empty dashboard
// is never the rendered outcome of a bad save.
func (c widgetConfig) visibleOrder() []string {
	hidden := map[string]bool{}
	for _, id := range c.Hidden {
		hidden[id] = true
	}
	var out []string
	for _, id := range c.Order {
		if !hidden[id] {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), defaultWidgetOrder...)
	}
	return out
}

// widgetConfigFromForm parses the editor's comma-separated fields. Strict:
// unknown ids or duplicates reject the submission (the editor only produces
// valid state; anything else is a forged request).
func widgetConfigFromForm(orderCSV, hiddenCSV string) (widgetConfig, bool) {
	c := widgetConfig{}
	seen := map[string]bool{}
	for _, id := range splitCSV(orderCSV) {
		if !validWidgetID(id) || seen[id] {
			return widgetConfig{}, false
		}
		seen[id] = true
		c.Order = append(c.Order, id)
	}
	if len(c.Order) != len(defaultWidgetOrder) {
		return widgetConfig{}, false
	}
	hiddenSeen := map[string]bool{}
	for _, id := range splitCSV(hiddenCSV) {
		if !validWidgetID(id) || hiddenSeen[id] {
			return widgetConfig{}, false
		}
		hiddenSeen[id] = true
		c.Hidden = append(c.Hidden, id)
	}
	return c, true
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// marshal serializes for storage; the default arrangement serializes to ""
// (delete the pref) so "reset" leaves no residue.
func (c widgetConfig) marshal() string {
	n := c.normalize()
	if len(n.Hidden) == 0 {
		isDefault := true
		for i, id := range n.Order {
			if defaultWidgetOrder[i] != id {
				isDefault = false
				break
			}
		}
		if isDefault {
			return ""
		}
	}
	b, err := json.Marshal(n)
	if err != nil {
		return ""
	}
	return string(b)
}
