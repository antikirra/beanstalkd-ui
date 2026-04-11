package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/antikirra/beanstalkd-ui/internal/api"
	"github.com/antikirra/beanstalkd-ui/internal/store"
)

func main() {
	listenAddr := flag.String("l", "127.0.0.1:3000", "HTTP listen address")
	dbPath := flag.String("d", "", "Path to database file (default: beanstalkd-ui.db near executable)")
	showVer := flag.Bool("v", false, "Show version and exit")
	flag.Parse()

	if *showVer {
		fmt.Printf("beanstalkd-ui version: %.1f\n", store.Version)
		os.Exit(0)
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(log)

	resolved := *dbPath
	if resolved == "" {
		selfDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			log.Error("failed to resolve executable directory", "error", err)
			os.Exit(1)
		}
		resolved = filepath.Join(selfDir, "beanstalkd-ui.db")
	}

	st, err := store.Open(resolved)
	if err != nil {
		log.Error("failed to open database", "path", resolved, "error", err)
		os.Exit(1)
	}

	samples, err := st.LoadSamples()
	if err != nil {
		log.Error("failed to load samples", "error", err)
		os.Exit(1)
	}

	password := os.Getenv("BEANSTALKD_UI_PASSWORD")

	handler, h, err := api.NewServer(log, st, samples, password)
	if err != nil {
		log.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:         *listenAddr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info("starting server", "addr", *listenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	fmt.Printf("To view beanstalkd console open http://%s in browser\n", *listenAddr)

	ctx, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { h.StatisticsCollector(ctx) })

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info("shutting down...")
	stop()
	wg.Wait()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	h.Close()
	st.Close()
}
