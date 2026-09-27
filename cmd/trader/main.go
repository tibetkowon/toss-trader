// Command trader is the entrypoint for the auto-trading service.
package main

import (
	"context"
	"log"
	"os"

	"github.com/tibetkowon/toss-trader/internal/lifecycle"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

type calendarAdapter struct{ client *tossapi.Client }

func (a calendarAdapter) IsMarketOpen(ctx context.Context, market string) (bool, error) {
	calendar, err := a.client.MarketCalendar(ctx, market)
	if err != nil {
		return false, err
	}
	return calendar.IsOpenToday(), nil
}

func main() {
	market := os.Getenv("MARKET")
	if market != "KR" && market != "US" {
		log.Print("필수 환경 변수 MARKET은 정확히 KR 또는 US여야 합니다")
		os.Exit(1)
	}

	clientIDSecret := os.Getenv("TOSS_CLIENT_ID_SECRET")
	if clientIDSecret == "" {
		log.Print("필수 환경 변수 TOSS_CLIENT_ID_SECRET이 비어 있습니다")
		os.Exit(1)
	}
	clientSecretSecret := os.Getenv("TOSS_CLIENT_SECRET_SECRET")
	if clientSecretSecret == "" {
		log.Print("필수 환경 변수 TOSS_CLIENT_SECRET_SECRET이 비어 있습니다")
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := tossapi.New(ctx, tossapi.Config{
		Secrets:            tossapi.NewSecretManagerProvider(nil),
		ClientIDSecret:     clientIDSecret,
		ClientSecretSecret: clientSecretSecret,
	})
	if err != nil {
		log.Printf("토스 API 클라이언트 초기화에 실패했습니다: %v", err)
		os.Exit(1)
	}

	open, err := lifecycle.SelfStopIfClosed(ctx, calendarAdapter{client}, lifecycle.NewComputeStopper(nil), market)
	if err != nil {
		log.Printf("%s 시장 개장 여부 확인 또는 인스턴스 자체 정지에 실패했습니다: %v", market, err)
		os.Exit(1)
	}
	if !open {
		log.Printf("%s 시장은 오늘 휴장입니다. 인스턴스 자체 정지 요청이 수락되었습니다", market)
		return
	}

	log.Printf("%s 시장은 오늘 개장합니다", market)
	log.Print("거래 로직은 아직 구현되지 않았습니다. 정상 종료합니다")
}
