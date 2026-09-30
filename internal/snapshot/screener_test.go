package snapshot

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestScreenerStatusJSONAndHTML(t *testing.T) {
	attack := "<script>alert(1)</script>"
	s := Snapshot{
		UpdatedAt: time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC),
		Screener: &ScreenerStatus{
			Active:     []ScreenerActive{{Symbol: attack, Rank: 3, Target: 51200, Origin: attack}},
			Rejections: []ScreenerRejection{{Symbol: "LEV1", Reason: attack}},
		},
	}
	data, err := s.RenderJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got Snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Screener, s.Screener) {
		t.Errorf("JSON 왕복 결과: %+v, 기대: %+v", got.Screener, s.Screener)
	}

	html, err := s.RenderHTML()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "<script>") {
		t.Fatal("실행 가능한 스크립트가 포함되었습니다")
	}
	if got := strings.Count(string(html), "&lt;script&gt;alert(1)&lt;/script&gt;"); got != 3 {
		t.Errorf("이스케이프된 필드 수: %d, 기대: 3", got)
	}
	for _, want := range []string{"활성 종목", "51200", "LEV1", "탈락 종목"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("필수 표시 내용 누락: %s", want)
		}
	}
}

func TestScreenerAbsentWhenNil(t *testing.T) {
	data, err := (Snapshot{}).RenderJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "screener") {
		t.Errorf("nil일 때 JSON에 screener가 없어야 합니다: %s", data)
	}
	html, err := (Snapshot{}).RenderHTML()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "활성 종목") {
		t.Error("nil일 때 스크리너 섹션이 없어야 합니다")
	}
}
