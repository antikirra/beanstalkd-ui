package pool

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/beanstalkd/go-beanstalk"
)

// PoolKind distinguishes read and write connection pools.
type PoolKind int

const (
	ReadPool  PoolKind = iota
	WritePool
)

// Config holds tuning parameters for the connection pool.
type Config struct {
	MaxIdle            int
	MaxActive          int
	IdleTimeout        time.Duration
	DialTimeout        time.Duration
	IdleHealthThreshold time.Duration
}

func (c Config) withDefaults() Config {
	if c.MaxIdle <= 0 {
		c.MaxIdle = 2
	}
	if c.MaxActive <= 0 {
		c.MaxActive = 10
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 60 * time.Second
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.IdleHealthThreshold <= 0 {
		c.IdleHealthThreshold = 5 * time.Second
	}
	return c
}

// Manager manages per-server, per-kind connection pools.
// It is safe for concurrent use from multiple goroutines.
type Manager struct {
	cfg    Config
	pools  sync.Map // poolKey → *serverPool
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type poolKey struct {
	server string
	kind   PoolKind
}

type idleConn struct {
	conn   *beanstalk.Conn
	idleAt time.Time
}

// New creates a Manager and starts the background idle-connection reaper.
func New(cfg Config) *Manager {
	cfg = cfg.withDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}
	m.wg.Add(1)
	go m.reaper()
	return m
}

// Close cancels the reaper and closes all pooled connections.
func (m *Manager) Close() {
	m.cancel()
	m.wg.Wait()
	m.pools.Range(func(key, value any) bool {
		sp := value.(*serverPool)
		sp.closeAll()
		m.pools.Delete(key)
		return true
	})
}

// WithConn gets a connection from the pool, calls fn, and returns the connection.
// If fn returns a connection-level error the connection is discarded; otherwise
// it is returned to the pool for reuse. Safe against panics in fn.
func (m *Manager) WithConn(ctx context.Context, server string, kind PoolKind, fn func(*beanstalk.Conn) error) (retErr error) {
	sp := m.pool(server, kind)
	conn, err := sp.get(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			_ = conn.Close()
			<-sp.sem
			panic(r)
		}
		sp.put(conn, retErr)
	}()
	retErr = fn(conn)
	return retErr
}

// Get retrieves a connection from the pool. The returned release function
// MUST be called when done with the connection, passing the last error
// from beanstalkd operations (or nil). The release function is bound to
// the correct pool instance, so it remains safe even if RemoveServer is
// called concurrently.
func (m *Manager) Get(ctx context.Context, server string, kind PoolKind) (*beanstalk.Conn, func(error), error) {
	sp := m.pool(server, kind)
	conn, err := sp.get(ctx)
	if err != nil {
		return nil, nil, err
	}
	return conn, func(lastErr error) { sp.put(conn, lastErr) }, nil
}

// RemoveServer drains and removes all pools (read + write) for the given server.
func (m *Manager) RemoveServer(server string) {
	for _, kind := range []PoolKind{ReadPool, WritePool} {
		key := poolKey{server, kind}
		if v, loaded := m.pools.LoadAndDelete(key); loaded {
			sp := v.(*serverPool)
			sp.closeAll()
		}
	}
}

func (m *Manager) pool(server string, kind PoolKind) *serverPool {
	key := poolKey{server, kind}
	if v, ok := m.pools.Load(key); ok {
		return v.(*serverPool)
	}
	sp := newServerPool(server, m.cfg)
	if v, loaded := m.pools.LoadOrStore(key, sp); loaded {
		return v.(*serverPool)
	}
	return sp
}

func (m *Manager) reaper() {
	defer m.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.pools.Range(func(_, value any) bool {
				sp := value.(*serverPool)
				sp.reapIdle(m.cfg.IdleTimeout)
				return true
			})
		}
	}
}

// serverPool is a pool of connections to a single beanstalkd server.
type serverPool struct {
	mu     sync.Mutex
	addr   string
	idle   []idleConn
	sem    chan struct{}
	closed bool
	cfg    Config
}

func newServerPool(addr string, cfg Config) *serverPool {
	return &serverPool{
		addr: addr,
		idle: make([]idleConn, 0, cfg.MaxIdle),
		sem:  make(chan struct{}, cfg.MaxActive),
		cfg:  cfg,
	}
}

func (sp *serverPool) get(ctx context.Context) (*beanstalk.Conn, error) {
	select {
	case sp.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	sp.mu.Lock()
	for len(sp.idle) > 0 {
		// Pop from end (LIFO — most recently used).
		ic := sp.idle[len(sp.idle)-1]
		sp.idle = sp.idle[:len(sp.idle)-1]
		sp.mu.Unlock()

		if sp.healthCheck(ic) {
			return ic.conn, nil
		}
		_ = ic.conn.Close()

		sp.mu.Lock()
	}
	sp.mu.Unlock()

	conn, err := sp.dialContext(ctx)
	if err != nil {
		<-sp.sem
		return nil, err
	}
	return conn, nil
}

func (sp *serverPool) put(conn *beanstalk.Conn, lastErr error) {
	if lastErr != nil && IsConnError(lastErr) {
		_ = conn.Close()
		<-sp.sem
		return
	}

	sp.mu.Lock()
	if sp.closed || len(sp.idle) >= sp.cfg.MaxIdle {
		sp.mu.Unlock()
		_ = conn.Close()
		<-sp.sem
		return
	}
	sp.idle = append(sp.idle, idleConn{conn: conn, idleAt: time.Now()})
	sp.mu.Unlock()
	<-sp.sem
}

func (sp *serverPool) closeAll() {
	sp.mu.Lock()
	sp.closed = true
	idle := sp.idle
	sp.idle = nil
	sp.mu.Unlock()
	for _, ic := range idle {
		_ = ic.conn.Close()
	}
}

func (sp *serverPool) reapIdle(maxAge time.Duration) {
	now := time.Now()
	sp.mu.Lock()
	var toClose []idleConn
	kept := sp.idle[:0]
	for _, ic := range sp.idle {
		if now.Sub(ic.idleAt) > maxAge {
			toClose = append(toClose, ic)
		} else {
			kept = append(kept, ic)
		}
	}
	sp.idle = kept
	sp.mu.Unlock()
	for _, ic := range toClose {
		_ = ic.conn.Close()
	}
}

func (sp *serverPool) dialContext(ctx context.Context) (*beanstalk.Conn, error) {
	d := net.Dialer{
		Timeout:   sp.cfg.DialTimeout,
		KeepAlive: beanstalk.DefaultKeepAlivePeriod,
	}
	nc, err := d.DialContext(ctx, "tcp", sp.addr)
	if err != nil {
		return nil, err
	}
	return beanstalk.NewConn(nc), nil
}

func (sp *serverPool) healthCheck(ic idleConn) bool {
	if time.Since(ic.idleAt) < sp.cfg.IdleHealthThreshold {
		return true
	}
	_, err := ic.conn.Stats()
	return err == nil
}

// IsConnError reports whether err indicates a broken connection
// (as opposed to a beanstalkd protocol error where the connection is still usable).
func IsConnError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return false
}
