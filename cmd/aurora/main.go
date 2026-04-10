package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/xuri/aurora/internal/api"
	"github.com/xuri/aurora/internal/config"
	"github.com/xuri/aurora/internal/model"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(log)

	configPath := config.ParseFlags()

	cfg, err := config.Read(configPath)
	if err != nil {
		log.Error("failed to read config", "error", err)
		os.Exit(1)
	}

	// Deserialize sample jobs from config.
	var samples model.SampleJobs
	if err := json.Unmarshal([]byte(cfg.Sample.Storage), &samples); err != nil {
		log.Error("failed to parse sample jobs", "error", err)
		os.Exit(1)
	}

	handler, h, err := api.NewServer(log, cfg, configPath, samples)
	if err != nil {
		log.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    cfg.Listen,
		Handler: handler,
	}

	go func() {
		log.Info("starting server", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := context.WithCancel(context.Background())
	go h.StatisticsCollector(ctx)

	openPage(cfg)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down...")
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func openPage(cfg *config.Config) {
	addr := fmt.Sprintf("http://%s", cfg.Listen)
	fmt.Println("To view beanstalkd console open", addr, "in browser")
	if !cfg.OpenPage.Enabled {
		return
	}
	var err error
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		err = runCmd("xdg-open", addr)
	case "darwin":
		err = runCmd("open", addr)
	case "windows":
		err = runCmd("cmd", "/c", "start", strings.NewReplacer("&", "^&").Replace(addr))
	default:
		err = fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	if err != nil {
		fmt.Println(err)
	}
}

func runCmd(prog string, args ...string) error {
	return exec.Command(prog, args...).Run()
}
