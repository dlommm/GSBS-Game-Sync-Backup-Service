package webui

import (
	"reflect"
	"testing"
)

func TestParseWidgetConfigFallbacks(t *testing.T) {
	if got := parseWidgetConfig("").Order; !reflect.DeepEqual(got, defaultWidgetOrder) {
		t.Fatalf("empty raw order = %v", got)
	}
	if got := parseWidgetConfig("{not json").Order; !reflect.DeepEqual(got, defaultWidgetOrder) {
		t.Fatalf("bad json order = %v", got)
	}
	// Unknown ids drop; missing ids append in default order.
	c := parseWidgetConfig(`{"order":["pulse","evil","stats"],"hidden":["devices","bogus"]}`)
	wantOrder := []string{"pulse", "stats", "games", "activity", "devices"}
	if !reflect.DeepEqual(c.Order, wantOrder) {
		t.Fatalf("order = %v, want %v", c.Order, wantOrder)
	}
	if !reflect.DeepEqual(c.Hidden, []string{"devices"}) {
		t.Fatalf("hidden = %v", c.Hidden)
	}
}

func TestVisibleOrderNeverEmpty(t *testing.T) {
	c := widgetConfig{Order: defaultWidgetOrder, Hidden: defaultWidgetOrder}
	if got := c.visibleOrder(); !reflect.DeepEqual(got, defaultWidgetOrder) {
		t.Fatalf("all-hidden must fall back to default, got %v", got)
	}
	c = parseWidgetConfig(`{"order":["games","stats","activity","devices","pulse"],"hidden":["pulse"]}`)
	want := []string{"games", "stats", "activity", "devices"}
	if got := c.visibleOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("visible = %v, want %v", got, want)
	}
}

func TestWidgetConfigFromFormStrict(t *testing.T) {
	if _, ok := widgetConfigFromForm("stats,games,activity,devices,pulse", "pulse"); !ok {
		t.Fatal("valid form rejected")
	}
	if _, ok := widgetConfigFromForm("stats,games,activity,devices", ""); ok {
		t.Fatal("incomplete order accepted")
	}
	if _, ok := widgetConfigFromForm("stats,games,activity,devices,evil", ""); ok {
		t.Fatal("unknown id accepted")
	}
	if _, ok := widgetConfigFromForm("stats,stats,games,activity,devices", ""); ok {
		t.Fatal("duplicate accepted")
	}
}

func TestWidgetMarshalRoundTripAndDefaultElision(t *testing.T) {
	c, ok := widgetConfigFromForm("pulse,stats,games,activity,devices", "devices")
	if !ok {
		t.Fatal("form parse failed")
	}
	raw := c.marshal()
	back := parseWidgetConfig(raw)
	if !reflect.DeepEqual(back.Order, c.Order) || !reflect.DeepEqual(back.Hidden, c.Hidden) {
		t.Fatalf("round trip: %+v -> %q -> %+v", c, raw, back)
	}
	// The default arrangement serializes to "" so reset deletes the pref.
	d, _ := widgetConfigFromForm("stats,games,activity,devices,pulse", "")
	if got := d.marshal(); got != "" {
		t.Fatalf("default marshal = %q, want \"\"", got)
	}
}
