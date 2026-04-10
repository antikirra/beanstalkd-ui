package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// authThrottle implements per-IP exponential backoff for failed auth attempts.
type authThrottle struct {
	mu      sync.Mutex
	records map[string]*authRecord
}

type authRecord struct {
	failures  int
	lastRetry time.Time
}

func newAuthThrottle() *authThrottle {
	return &authThrottle{records: make(map[string]*authRecord)}
}

func (t *authThrottle) isThrottled(ip string) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rec, ok := t.records[ip]
	if !ok || rec.failures == 0 {
		return false, 0
	}
	delay := min(100*time.Millisecond*(1<<(rec.failures-1)), 5*time.Second)
	remaining := time.Until(rec.lastRetry.Add(delay))
	if remaining <= 0 {
		return false, 0
	}
	return true, remaining
}

func (t *authThrottle) onFailure(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rec, ok := t.records[ip]
	if !ok {
		rec = &authRecord{}
		t.records[ip] = rec
	}
	rec.failures++
	rec.lastRetry = time.Now()
}

func (t *authThrottle) onSuccess(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.records, ip)
}

// authMiddleware wraps a handler with HTTP Basic authentication and throttling.
func authMiddleware(next http.Handler, username, password string, throttle *authThrottle, log *slog.Logger) http.Handler {
	userBytes := []byte(username)
	passBytes := []byte(password)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := remoteIP(r)
		if throttled, remaining := throttle.isThrottled(ip); throttled {
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", remaining.Seconds()+1))
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		auth := r.Header.Get("Authorization")
		const prefix = "Basic "
		if strings.HasPrefix(auth, prefix) {
			payload, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
			if err == nil {
				pair := bytes.SplitN(payload, []byte(":"), 2)
				if len(pair) == 2 &&
					subtle.ConstantTimeCompare(pair[0], userBytes) == 1 &&
					subtle.ConstantTimeCompare(pair[1], passBytes) == 1 {
					throttle.onSuccess(ip)
					next.ServeHTTP(w, r)
					return
				}
			}
		}
		throttle.onFailure(ip)
		log.Warn("auth failure", "ip", ip)
		w.Header().Set("WWW-Authenticate", `Basic realm="Aurora"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
}

// securityHeaders sets protective HTTP headers including CSP.
func securityHeaders(next http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"object-src 'none'",
	}, "; ")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		next.ServeHTTP(w, r)
	})
}

// noCache sets headers that prevent caching of dynamic responses.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, private, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// recoveryMiddleware catches panics and returns 500 instead of crashing.
func recoveryMiddleware(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic recovered",
					"error", rec,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func remoteIP(r *http.Request) string {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return strings.Trim(ip, "[]")
}
