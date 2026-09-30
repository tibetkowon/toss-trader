package screener

import (
	"reflect"
	"testing"
)

func ranked(pairs ...any) []Ranked {
	var out []Ranked
	for i := 0; i < len(pairs); i += 3 {
		out = append(out, Ranked{Symbol: pairs[i].(string), Rank: pairs[i+1].(int), Price: pairs[i+2].(float64)})
	}
	return out
}

func TestSelectActiveTopNByRank(t *testing.T) {
	passing := ranked("C", 3, 10.0, "A", 1, 10.0, "B", 2, 10.0, "D", 4, 10.0)
	got := SelectActive(passing, nil, 2, 0, 0, 0)
	if want := []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSelectActiveFewerThanCount(t *testing.T) {
	if got := SelectActive(ranked("A", 1, 10.0), nil, 10, 0, 0, 0); !reflect.DeepEqual(got, []string{"A"}) {
		t.Errorf("got %v", got)
	}
	if got := SelectActive(nil, []string{"X"}, 10, 20, 2, 100); len(got) != 0 {
		t.Errorf("빈 입력에서 %v", got)
	}
}

func TestSelectActiveHysteresisKeepsPrevWithinKeepRank(t *testing.T) {
	passing := ranked("A", 1, 10.0, "B", 2, 10.0, "C", 3, 10.0, "D", 15, 10.0)
	// 직전 활성이 D였고 D는 keepRank(20) 이내 -> 밀려나지 않고 유지, 남는 한 자리는 랭킹 1위
	got := SelectActive(passing, []string{"D"}, 2, 20, 0, 0)
	if want := []string{"A", "D"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// keepRank 밖으로 밀리면 유지하지 않는다
	got = SelectActive(passing, []string{"D"}, 2, 10, 0, 0)
	if want := []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// keepRank 0이면 꺼짐
	got = SelectActive(passing, []string{"D"}, 2, 0, 0, 0)
	if want := []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSelectActiveMinAffordableSwapsLowestUnaffordable(t *testing.T) {
	// maxPrice 100: A,B는 못 산다(비싸다). C는 살 수 있다.
	passing := ranked("A", 1, 500.0, "B", 2, 300.0, "C", 3, 50.0, "D", 4, 60.0)
	got := SelectActive(passing, nil, 2, 0, 1, 100)
	if want := []string{"A", "C"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("최소 1개: got %v, want %v", got, want)
	}
	got = SelectActive(passing, nil, 2, 0, 2, 100)
	if want := []string{"C", "D"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("최소 2개: got %v, want %v", got, want)
	}
}

func TestSelectActiveMinAffordableNoCandidatesOrRuleOff(t *testing.T) {
	passing := ranked("A", 1, 500.0, "B", 2, 300.0)
	if got := SelectActive(passing, nil, 2, 0, 2, 100); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Errorf("살 수 있는 종목이 없으면 그대로: %v", got)
	}
	if got := SelectActive(ranked("A", 1, 500.0, "C", 2, 50.0), nil, 1, 0, 1, 0); !reflect.DeepEqual(got, []string{"A"}) {
		t.Errorf("maxPrice 0이면 규칙 꺼짐: %v", got)
	}
}

func TestSelectActiveZeroPriceIsUnaffordable(t *testing.T) {
	passing := ranked("A", 1, 0.0, "B", 2, 50.0)
	got := SelectActive(passing, nil, 1, 0, 1, 100)
	if !reflect.DeepEqual(got, []string{"B"}) {
		t.Errorf("가격을 모르는 종목은 살 수 있다고 세지 않습니다: %v", got)
	}
}

func TestSelectActiveDoesNotMutateInput(t *testing.T) {
	passing := ranked("C", 3, 1.0, "A", 1, 1.0)
	SelectActive(passing, nil, 2, 0, 0, 0)
	if passing[0].Symbol != "C" {
		t.Error("입력 슬라이스 순서가 바뀌었습니다")
	}
}
