package lifecycle

import (
	"context"
	"errors"
	"testing"
)

type fakeChecker struct {
	open bool
	err  error
}

func (f *fakeChecker) IsMarketOpen(ctx context.Context, market string) (bool, error) {
	return f.open, f.err
}

type fakeStopper struct {
	calls int
	err   error
}

func (f *fakeStopper) Stop(ctx context.Context) error {
	f.calls++
	return f.err
}

func TestSelfStopIfClosed(t *testing.T) {
	checkerErr := errors.New("개장 여부 조회 실패")
	stopErr := errors.New("정지 실패")
	for _, tc := range []struct {
		name      string
		checker   fakeChecker
		stopErr   error
		wantOpen  bool
		wantErr   error
		wantCalls int
	}{
		{name: "개장", checker: fakeChecker{open: true}, wantOpen: true},
		{name: "휴장", wantCalls: 1},
		{name: "조회 오류", checker: fakeChecker{open: true, err: checkerErr}, wantErr: checkerErr},
		{name: "휴장 시 정지 오류", stopErr: stopErr, wantErr: stopErr, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stopper := &fakeStopper{err: tc.stopErr}
			open, err := SelfStopIfClosed(context.Background(), &tc.checker, stopper, "KR")
			if open != tc.wantOpen {
				t.Errorf("개장 여부: %t, 기대: %t", open, tc.wantOpen)
			}
			if err != tc.wantErr {
				t.Errorf("오류: %v, 기대: %v", err, tc.wantErr)
			}
			if stopper.calls != tc.wantCalls {
				t.Errorf("정지 호출 수: %d, 기대: %d", stopper.calls, tc.wantCalls)
			}
		})
	}
}
