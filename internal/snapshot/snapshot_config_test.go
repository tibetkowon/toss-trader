package snapshot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/settings"
)

func TestRenderJSONIncludesAppliedConfig(t *testing.T) {
	applied := settings.Defaults()
	applied.StopLossPct = 0.015
	s := Snapshot{Config: &applied, ConfigVersion: 7}

	data, err := s.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var got struct {
		Config        *settings.Settings `json:"config"`
		ConfigVersion int                `json:"config_version"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Config == nil || got.Config.StopLossPct != 0.015 || got.ConfigVersion != 7 {
		t.Fatalf("설정이 왕복 후 달라졌습니다: %+v", got)
	}
}

func TestRenderJSONOmitsConfigWhenUnset(t *testing.T) {
	data, err := Snapshot{}.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if strings.Contains(string(data), `"config"`) || strings.Contains(string(data), `"config_version"`) {
		t.Fatalf("설정이 없으면 생략해야 합니다: %s", data)
	}
}
