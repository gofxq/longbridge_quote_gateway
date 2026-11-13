package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofxq/longbridge_quote_gateway/internal/cache"
	"github.com/gofxq/longbridge_quote_gateway/internal/config"
	"github.com/gofxq/longbridge_quote_gateway/internal/provider/longbridge"
	"github.com/gofxq/longbridge_quote_gateway/internal/server"
	"github.com/gofxq/longbridge_quote_gateway/internal/service"
)

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	provider, err := longbridge.New(ctx)
	if err != nil {
		return err
	}
	defer provider.Close()
	c := cache.New(cfg.TTL, cfg.CacheEntries, cfg.CacheBytes, cfg.CacheItemBytes)
	go c.Run(ctx)
	svc := service.New(ctx, provider, c, cfg)
	defer svc.Close()
	srv := &http.Server{Addr: cfg.Addr, Handler: server.New(cfg, svc, logger), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.HTTPTimeout, WriteTimeout: cfg.HTTPTimeout + 2*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	logger.Info("listening", "address", cfg.Addr)
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), cfg.HTTPTimeout+time.Second)
		defer stop()
		return srv.Shutdown(shutdown)
	}
	return nil
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 2 * time.Second}
		res, err := client.Get("http://127.0.0.1:12280/healthz")
		if err != nil {
			os.Exit(1)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
