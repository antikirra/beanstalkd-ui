package api

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/beanstalkd/go-beanstalk"

	"github.com/antikirra/beanstalkd-ui/internal/model"
	"github.com/antikirra/beanstalkd-ui/internal/pool"
)

func newTube(conn *beanstalk.Conn, name string) *beanstalk.Tube {
	return &beanstalk.Tube{Conn: conn, Name: name}
}

func (h *Handlers) addJob(ctx context.Context, server, tube, data, priority, delay, ttr string) error {
	pri, err := strconv.ParseUint(priority, 10, 32)
	if err != nil {
		pri = uint64(model.DefaultPriority)
	}
	d, err := strconv.Atoi(delay)
	if err != nil {
		d = model.DefaultDelay
	}
	t, err := strconv.Atoi(ttr)
	if err != nil {
		t = model.DefaultTTR
	}

	return h.pool.WithConn(ctx, server, pool.WritePool, func(conn *beanstalk.Conn) error {
		_, err := newTube(conn, tube).Put(
			[]byte(data),
			uint32(pri),
			time.Duration(d)*time.Second,
			time.Duration(t)*time.Second,
		)
		return err
	})
}

func (h *Handlers) deleteJob(ctx context.Context, server, jobID string) error {
	id, err := strconv.Atoi(jobID)
	if err != nil {
		return fmt.Errorf("invalid job ID: %w", err)
	}
	return h.pool.WithConn(ctx, server, pool.WritePool, func(conn *beanstalk.Conn) error {
		return conn.Delete(uint64(id))
	})
}

func (h *Handlers) deleteAll(ctx context.Context, server, tube, state string) error {
	conn, release, err := h.pool.Get(ctx, server, pool.WritePool)
	if err != nil {
		return err
	}
	var opErr error
	defer func() { release(opErr) }()

	t := newTube(conn, tube)
	switch state {
	case "ready":
		opErr = drainTube(ctx, conn, t.PeekReady)
	case "delayed":
		opErr = drainTube(ctx, conn, t.PeekDelayed)
	case "buried":
		opErr = drainTube(ctx, conn, t.PeekBuried)
	default:
		opErr = drainTube(ctx, conn, t.PeekReady)
		if opErr != nil {
			return opErr
		}
		opErr = drainTube(ctx, conn, t.PeekBuried)
		if opErr != nil {
			return opErr
		}
		opErr = drainTube(ctx, conn, t.PeekDelayed)
	}
	return opErr
}

func drainTube(ctx context.Context, conn *beanstalk.Conn, peek func() (uint64, []byte, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		id, _, err := peek()
		if err != nil {
			if pool.IsConnError(err) {
				return err
			}
			return nil
		}
		if err := conn.Delete(id); err != nil {
			return err
		}
	}
}

func (h *Handlers) kick(ctx context.Context, server, tube, count string) error {
	bound, err := strconv.Atoi(count)
	if err != nil {
		bound = 0
	}
	return h.pool.WithConn(ctx, server, pool.WritePool, func(conn *beanstalk.Conn) error {
		_, err := newTube(conn, tube).Kick(bound)
		return err
	})
}

func (h *Handlers) kickJob(ctx context.Context, server, id string) error {
	jobID, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("invalid job ID: %w", err)
	}
	return h.pool.WithConn(ctx, server, pool.WritePool, func(conn *beanstalk.Conn) error {
		return conn.KickJob(uint64(jobID))
	})
}

func (h *Handlers) pause(ctx context.Context, server, tube, count string, pauseSeconds int) error {
	return h.pool.WithConn(ctx, server, pool.WritePool, func(conn *beanstalk.Conn) error {
		t := newTube(conn, tube)
		switch count {
		case "-1":
			dur := time.Duration(pauseSeconds) * time.Second
			if pauseSeconds == -1 {
				dur = model.DefaultTubePauseSeconds * time.Second
			}
			return t.Pause(dur)
		case "0":
			return t.Pause(0)
		}
		return nil
	})
}

func (h *Handlers) moveJobsTo(ctx context.Context, server, tube, destTube, state, destState string) error {
	conn, release, err := h.pool.Get(ctx, server, pool.WritePool)
	if err != nil {
		return err
	}
	var opErr error
	defer func() { release(opErr) }()

	// Special case: ready → buried (requires reserve+bury).
	if state == "ready" && destState == "buried" {
		ts := beanstalk.NewTubeSet(conn, tube)
		for {
			if ctx.Err() != nil {
				return nil
			}
			id, _, err := ts.Reserve(time.Second)
			if err != nil {
				opErr = err
				return opErr
			}
			if err := conn.Bury(id, model.DefaultPriority); err != nil {
				opErr = err
				return opErr
			}
		}
	}

	// General case: peek → put to dest → delete from src.
	if tube == destTube {
		return nil
	}
	src := newTube(conn, tube)
	dst := newTube(conn, destTube)
	peek := src.PeekReady
	if state == "buried" {
		peek = src.PeekBuried
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		id, body, err := peek()
		if err != nil {
			if pool.IsConnError(err) {
				opErr = err
				return opErr
			}
			return nil
		}
		if _, err := dst.Put(body, model.DefaultPriority, model.DefaultDelay, model.DefaultTTR); err != nil {
			opErr = err
			return opErr
		}
		if err := conn.Delete(id); err != nil {
			opErr = err
			return opErr
		}
	}
}

func (h *Handlers) clearTubes(ctx context.Context, server string, data url.Values) error {
	for _, tube := range parseTubesFromForm(data) {
		if err := h.deleteAll(ctx, server, tube, ""); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handlers) searchTube(ctx context.Context, server, tube string, limit int, searchStr string) []model.SearchResult {
	conn, release, err := h.pool.Get(ctx, server, pool.ReadPool)
	if err != nil {
		return nil
	}
	var opErr error
	defer func() { release(opErr) }()

	stats, err := conn.Stats()
	if err != nil {
		opErr = err
		return nil
	}
	totalJobs, err := strconv.ParseUint(stats["total-jobs"], 10, 64)
	if err != nil {
		return nil
	}

	var result []model.SearchResult
	for _, state := range []string{"ready", "delayed", "buried"} {
		cnt := 0
		for id := totalJobs; id > 0 && cnt < limit; id-- {
			r, err := matchJob(conn, tube, searchStr, state, id)
			if err != nil {
				opErr = err
				return result
			}
			if r != nil {
				result = append(result, *r)
				cnt++
			}
		}
	}
	return result
}

func matchJob(conn *beanstalk.Conn, tube, searchStr, state string, id uint64) (*model.SearchResult, error) {
	stats, err := conn.StatsJob(id)
	if err != nil {
		if pool.IsConnError(err) {
			return nil, err
		}
		return nil, nil
	}
	if stats["tube"] != tube || stats["state"] != state {
		return nil, nil
	}
	body, err := conn.Peek(id)
	if err != nil {
		if pool.IsConnError(err) {
			return nil, err
		}
		return nil, nil
	}
	data := string(body)
	if !strings.Contains(data, searchStr) {
		return nil, nil
	}
	return &model.SearchResult{ID: id, State: state, Data: data}, nil
}

func (h *Handlers) listTubesSorted(ctx context.Context, server string) []string {
	var tubes []string
	_ = h.pool.WithConn(ctx, server, pool.ReadPool, func(conn *beanstalk.Conn) error {
		var err error
		tubes, err = conn.ListTubes()
		return err
	})
	slices.Sort(tubes)
	return tubes
}
