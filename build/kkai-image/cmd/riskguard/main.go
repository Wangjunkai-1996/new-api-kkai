package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Wangjunkai-1996/new-api-kkai/build/kkai-image/internal/riskguard"

	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	config, err := riskguard.LoadConfig()
	if err != nil {
		logger.Error("invalid risk guard configuration", "error", err)
		os.Exit(1)
	}
	rules, err := riskguard.LoadDefaultRules()
	if err != nil {
		logger.Error("invalid embedded risk rules", "error", err)
		os.Exit(1)
	}
	redisClient := redis.NewClient(config.Redis)
	defer redisClient.Close()
	publisher, err := riskguard.NewRedisPublisher(redisClient, config.SigningSecret)
	if err != nil {
		logger.Error("invalid risk publisher configuration", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              config.ListenAddress,
		Handler:           riskguard.NewHandler(config, publisher, rules, logger),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       90 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	logger.Info("risk guard started", "listen", config.ListenAddress, "rules", rules.Version())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("risk guard stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
