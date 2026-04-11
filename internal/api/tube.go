package api

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/beanstalkd/go-beanstalk"
	"github.com/antikirra/beanstalkd-ui/internal/model"
)

func dialBeanstalk(server string) (*beanstalk.Conn, error) {
	return beanstalk.Dial("tcp", server)
}

func newTube(conn *beanstalk.Conn, name string) *beanstalk.Tube {
	return &beanstalk.Tube{Conn: conn, Name: name}
}

func addJob(server, tube, data, priority, delay, ttr string) {
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

	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()

	_, _ = newTube(conn, tube).Put(
		[]byte(data),
		uint32(pri),
		time.Duration(d)*time.Second,
		time.Duration(t)*time.Second,
	)
}

func deleteJob(server, jobID string) {
	id, err := strconv.Atoi(jobID)
	if err != nil {
		return
	}
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.Delete(uint64(id))
}

func deleteAll(ctx context.Context, server, tube, state string) {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()

	t := newTube(conn, tube)
	switch state {
	case "ready":
		drainTube(ctx, conn, t.PeekReady)
	case "delayed":
		drainTube(ctx, conn, t.PeekDelayed)
	case "buried":
		drainTube(ctx, conn, t.PeekBuried)
	default:
		drainTube(ctx, conn, t.PeekReady)
		drainTube(ctx, conn, t.PeekBuried)
		drainTube(ctx, conn, t.PeekDelayed)
	}
}

func drainTube(ctx context.Context, conn *beanstalk.Conn, peek func() (uint64, []byte, error)) {
	for {
		if ctx.Err() != nil {
			return
		}
		id, _, err := peek()
		if err != nil {
			return
		}
		_ = conn.Delete(id)
	}
}

func kick(server, tube, count string) {
	bound, err := strconv.Atoi(count)
	if err != nil {
		bound = 0
	}
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = newTube(conn, tube).Kick(bound)
}

func kickJob(server, id string) {
	jobID, err := strconv.Atoi(id)
	if err != nil {
		return
	}
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.KickJob(uint64(jobID))
}

func pause(server, tube, count string, pauseSeconds int) {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()

	t := newTube(conn, tube)
	switch count {
	case "-1":
		dur := time.Duration(pauseSeconds) * time.Second
		if pauseSeconds == -1 {
			dur = model.DefaultTubePauseSeconds * time.Second
		}
		_ = t.Pause(dur)
	case "0":
		_ = t.Pause(0)
	}
}

func moveJobsTo(ctx context.Context, server, tube, destTube, state, destState string) {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()

	// Special case: ready → buried (requires reserve+bury).
	if state == "ready" && destState == "buried" {
		ts := beanstalk.NewTubeSet(conn, tube)
		for {
			if ctx.Err() != nil {
				return
			}
			id, _, err := ts.Reserve(time.Second)
			if err != nil {
				return
			}
			if err := conn.Bury(id, model.DefaultPriority); err != nil {
				return
			}
		}
	}

	// General case: peek → put to dest → delete from src.
	if tube == destTube {
		return
	}
	src := newTube(conn, tube)
	dst := newTube(conn, destTube)
	peek := src.PeekReady
	if state == "buried" {
		peek = src.PeekBuried
	}
	for {
		if ctx.Err() != nil {
			return
		}
		id, body, err := peek()
		if err != nil {
			return
		}
		if _, err := dst.Put(body, model.DefaultPriority, model.DefaultDelay, model.DefaultTTR); err != nil {
			return
		}
		if err := conn.Delete(id); err != nil {
			return
		}
	}
}

func clearTubes(ctx context.Context, server string, data url.Values) {
	for tube := range data {
		deleteAll(ctx, server, tube, "")
	}
}

func searchTube(server, tube string, limit int, searchStr string) []model.SearchResult {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return nil
	}
	defer conn.Close()

	stats, err := conn.Stats()
	if err != nil {
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
			r := matchJob(conn, tube, searchStr, state, id)
			if r != nil {
				result = append(result, *r)
				cnt++
			}
		}
	}
	return result
}

func matchJob(conn *beanstalk.Conn, tube, searchStr, state string, id uint64) *model.SearchResult {
	stats, err := conn.StatsJob(id)
	if err != nil {
		return nil
	}
	if stats["tube"] != tube || stats["state"] != state {
		return nil
	}
	body, err := conn.Peek(id)
	if err != nil {
		return nil
	}
	data := string(body)
	if !strings.Contains(data, searchStr) {
		return nil
	}
	return &model.SearchResult{ID: id, State: state, Data: data}
}

func listTubesSorted(server string) []string {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return nil
	}
	defer conn.Close()
	tubes, _ := conn.ListTubes()
	slices.Sort(tubes)
	return tubes
}
