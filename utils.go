package main

import (
	cryptoRand "crypto/rand"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"time"
)

// compactUnique removes empty strings and deduplicates a string slice.
func compactUnique(s []string) []string {
	result := slices.DeleteFunc(s, func(v string) bool { return v == "" })
	slices.Sort(result)
	return slices.Compact(result)
}

// runCmd executes an external command.
func runCmd(prog string, args ...string) error {
	cmd := exec.Command(prog, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// randToken generate a cryptographically secure random token.
func randToken() string {
	b := make([]byte, 16)
	_, _ = cryptoRand.Read(b)
	return fmt.Sprintf("%x", b)
}

// setHeader sets standard HTTP response headers.
func setHeader(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Server", "WebServer")
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "no-cache, private, max-age=0")
	w.Header().Set("Expires", time.Unix(0, 0).Format(http.TimeFormat))
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Accel-Expires", "0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
}
