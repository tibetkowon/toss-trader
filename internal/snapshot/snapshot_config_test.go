package snapshot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderJSONIncludesEffectiveConfig(t *testing.T) {
	s := Snapshot{Config: &EffectiveConfig{StopLossPct: 0.02, K: 0.5, ActiveCount: 10}}

	data, err := s.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var got struct {
		Config *EffectiveConfig `json:"config"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Config == nil || got.Config.StopLossPct != 0.02 || got.Config.K != 0.5 || got.Config.ActiveCount != 10 {
		t.Fatalf("config not round-tripped: %+v", got.Config)
	}
}

func TestRenderJSONOmitsConfigWhenUnset(t *testing.T) {
	data, err := Snapshot{}.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if strings.Contains(string(data), `"config"`) {
		t.Fatalf("config should be omitted when nil: %s", data)
	}
}
