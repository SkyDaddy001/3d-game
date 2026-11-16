package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter counts actions per key in fixed windows. It is deliberately simple: one server process,
// counts in memory, reset on restart.
type limiter struct {
	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	count int
	reset time.Time
}

func newLimiter() *limiter { return &limiter{windows: map[string]*window{}} }

// allow records one action for key and reports whether it is within max per period.
func (l *limiter) allow(key string, max int, per time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.windows[key]
	if w == nil || now.After(w.reset) {
		w = &window{reset: now.Add(per)}
		l.windows[key] = w
	}
	w.count++
	return w.count <= max
}

// sweep drops windows that have ended.
func (l *limiter) sweep() {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, w := range l.windows {
		if now.After(w.reset) {
			delete(l.windows, k)
		}
	}
}

// clientIP returns the caller's address. Behind a trusted reverse proxy it is the last entry the
// proxy added to X-Forwarded-For; otherwise it is the connection's own address.
func (s *Server) clientIP(r *http.Request) string {
	if s.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limited answers 429 and returns true when the caller has done action too often. Each device may
// do it max times per period, and each address four times that (several players can share one).
func (s *Server) limited(w http.ResponseWriter, r *http.Request, action string, max int, per time.Duration) bool {
	if s.limits == nil {
		return false
	}
	okDevice := s.limits.allow(action+"|d|"+hashToken(sessionFrom(r).token), max, per)
	okIP := s.limits.allow(action+"|ip|"+s.clientIP(r), max*4, per)
	if okDevice && okIP {
		return false
	}
	w.Header().Set("Retry-After", "60")
	http.Error(w, "too many requests, slow down and try again in a minute", http.StatusTooManyRequests)
	return true
}
