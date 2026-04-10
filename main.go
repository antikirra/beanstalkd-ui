package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed public
var staticFiles embed.FS

func main() {
	parseFlags()
	if err := readConf(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	public, _ := fs.Sub(staticFiles, "public")
	http.Handle("/", http.FileServer(http.FS(public)))
	http.HandleFunc("/public", basicAuth(handlerMain))
	http.HandleFunc("/index", basicAuth(handlerServerList))
	http.HandleFunc("/serversRemove", basicAuth(serversRemove))
	http.HandleFunc("/server", basicAuth(handlerServer))
	http.HandleFunc("/tube", basicAuth(handlerTube))
	http.HandleFunc("/sample", basicAuth(handlerSample))
	http.HandleFunc("/statistics", basicAuth(handlerStatistics))

	srv := &http.Server{Addr: pubConf.Listen}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Println("cannot start server:", err)
			os.Exit(1)
		}
	}()
	ctx, stop := context.WithCancel(context.Background())
	go statisticsCollector(ctx)

	openPage()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nshutting down...")
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// openPage opens the console URL in the system's default browser.
func openPage() {
	addr := fmt.Sprintf("http://%s", pubConf.Listen)
	fmt.Println("To view beanstalkd console open", addr, "in browser")
	if !pubConf.OpenPage.Enabled {
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

// handleSignals waits for an interrupt signal (used by tests).
func handleSignals() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}
