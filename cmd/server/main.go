package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/syukronhidayat/nearby-drivers/internal/cleanup"
	"github.com/syukronhidayat/nearby-drivers/internal/config"
	"github.com/syukronhidayat/nearby-drivers/internal/httpapi"
	"github.com/syukronhidayat/nearby-drivers/internal/pubsub"
	"github.com/syukronhidayat/nearby-drivers/internal/store/redisstore"
	"github.com/syukronhidayat/nearby-drivers/internal/throttle"
	"github.com/syukronhidayat/nearby-drivers/internal/ws"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	driverStore := &redisstore.Store{RDB: rdb}

	hub := ws.NewHub(driverStore)
	pub := &pubsub.DriverUpdatesConsumer{RDB: rdb, Hub: hub}

	th := throttle.NewInMem(500 * time.Millisecond)

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewRouter(cfg, httpapi.Deps{
			DriverStore: driverStore,
			Throttler:   th,
			Hub:         hub,
		}),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s env=%s", cfg.HTTPAddr, cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go hub.Run(ctx)
	go func() {
		if err := pub.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("pubsub consumer stopper: %v", err)
		}
	}()

	cleaner := &cleanup.RedisCleanup{
		RDB:       rdb,
		Every:     60 * time.Second,
		MaxAge:    10 * time.Minute,
		BatchSize: 1000,
	}
	go func() {
		if err := cleaner.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("redis cleanup stopper: %v", err)
		}
	}()

	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received")
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	start := time.Now()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	} else {
		log.Printf("shutdown complete in %s", time.Since(start))
	}
}
