package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/tibetkowon/toss-trader/internal/backtest"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

func runFetch(ctx context.Context, args []string) error {
	log.Print("VM이 켜져 있으면 토큰 재발급으로 VM 토큰이 무효화됩니다")
	if len(args) != 1 || (args[0] != "KR" && args[0] != "US") {
		return fmt.Errorf("사용법: go run ./cmd/backtest fetch KR|US")
	}
	pages := 400
	if v := os.Getenv("BACKTEST_MAX_PAGES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return fmt.Errorf("BACKTEST_MAX_PAGES는 양의 정수여야 합니다")
		}
		pages = n
	}
	id, secret := os.Getenv("TOSS_CLIENT_ID"), os.Getenv("TOSS_CLIENT_SECRET")
	if id == "" || secret == "" {
		return fmt.Errorf("TOSS_CLIENT_ID/TOSS_CLIENT_SECRET 환경변수가 필요합니다")
	}
	client, err := tossapi.New(ctx, tossapi.Config{
		Secrets:        tossapi.NewMemorySecretProvider(map[string]string{"id": id, "secret": secret}),
		ClientIDSecret: "id", ClientSecretSecret: "secret",
	})
	if err != nil {
		return err
	}
	return backtest.Fetch(ctx, client, backtest.Cache{}, args[0], pages, log.Printf)
}
