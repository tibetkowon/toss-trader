package lifecycle

import "context"

// CalendarChecker reports whether a market is open.
type CalendarChecker interface {
	IsMarketOpen(ctx context.Context, market string) (bool, error)
}

// SelfStopIfClosed stops the current instance if the market is closed.
func SelfStopIfClosed(ctx context.Context, checker CalendarChecker, stopper Stopper, market string) (open bool, err error) {
	open, err = checker.IsMarketOpen(ctx, market)
	if err != nil {
		return false, err
	}
	if open {
		return true, nil
	}
	return false, stopper.Stop(ctx)
}
