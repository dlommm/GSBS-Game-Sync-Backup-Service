package pcgw

import (
	"reflect"
	"testing"
)

func TestParseSteamAppIDList(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"1057090"}, []string{"1057090"}},
		{[]string{"4540,4550"}, []string{"4540", "4550"}},
		{[]string{"4540, 4550 4560"}, []string{"4540", "4550", "4560"}},
		{[]string{"1057090", "1057090"}, []string{"1057090"}}, // de-dupe
		{[]string{"n/a"}, nil},
		{[]string{""}, nil},
		{[]string{"730abc"}, nil}, // non-numeric token rejected
	}
	for _, c := range cases {
		if got := ParseSteamAppIDList(c.in...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseSteamAppIDList(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestSteamAppIDsFromInfobox(t *testing.T) {
	// Mirrors the real Ori data: Cargo gave nothing, infobox has the ID.
	got := SteamAppIDsFromInfobox(map[string]string{
		"steam appid":      "1057090",
		"steam appid side": "",
		"developers":       "Moon Studios",
	})
	if !reflect.DeepEqual(got, []string{"1057090"}) {
		t.Fatalf("got %v", got)
	}
	// Case-insensitive key + side edition.
	got = SteamAppIDsFromInfobox(map[string]string{"Steam AppID": "100", "Steam AppID side": "200"})
	if !reflect.DeepEqual(got, []string{"100", "200"}) {
		t.Fatalf("got %v", got)
	}
	if SteamAppIDsFromInfobox(nil) != nil {
		t.Fatal("nil infobox should yield nil")
	}
}

func TestSteamAppIDsFromInfoboxAny(t *testing.T) {
	got := SteamAppIDsFromInfoboxAny(map[string]interface{}{
		"steam appid": "1057090",
		"hltb":        "46428",
		"ignored":     42, // non-string values are skipped
	})
	if !reflect.DeepEqual(got, []string{"1057090"}) {
		t.Fatalf("got %v", got)
	}
}
