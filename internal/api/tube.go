package api

import (
	"math"
	"net/url"
	"strconv"
	"time"

	"github.com/xuri/aurora/beanstalk"
	"github.com/xuri/aurora/internal/model"
)

func dialBeanstalk(server string) (*beanstalk.Conn, error) {
	return beanstalk.Dial("tcp", server)
}

func newTube(conn *beanstalk.Conn, name string) *beanstalk.Tube {
	return &beanstalk.Tube{Conn: conn, Name: name}
}

func addJob(server, tube, data, priority, delay, ttr string) {
	pri, err := strconv.ParseUint(priority, 10, 32)
	if err != nil || pri > math.MaxUint32 {
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

func deleteJob(server, tube, jobID string) {
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

func deleteAll(server, tube string) {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()

	t := newTube(conn, tube)
	drainTube(conn, t.PeekReady)
	drainTube(conn, t.PeekBuried)
	drainTube(conn, t.PeekDelayed)
}

func drainTube(conn *beanstalk.Conn, peek func() (uint64, []byte, error)) {
	for {
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

func kickJob(server, tube, id string) {
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

func pause(conf model.SelfConf, server, tube, count string) {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()

	t := newTube(conn, tube)
	switch count {
	case "-1":
		dur := time.Duration(conf.TubePauseSeconds) * time.Second
		if conf.TubePauseSeconds == -1 {
			dur = model.DefaultTubePauseSeconds * time.Second
		}
		_ = t.Pause(dur)
	case "0":
		_ = t.Pause(0)
	}
}

func moveJobsTo(server, tube, destTube, state, destState string) {
	switch state {
	case "ready":
		moveReadyJobsTo(server, tube, destTube, destState)
	case "buried":
		moveBuriedJobsTo(server, tube, destTube, destState)
	}
}

func moveReadyJobsTo(server, tube, destTube, destState string) {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()

	switch destState {
	case "buried":
		ts := beanstalk.NewTubeSet(conn, tube)
		for {
			id, _, err := ts.Reserve(time.Second)
			if err != nil {
				return
			}
			if err := conn.Bury(id, model.DefaultPriority); err != nil {
				return
			}
		}
	default:
		if tube == destTube {
			return
		}
		src := newTube(conn, tube)
		dst := newTube(conn, destTube)
		for {
			id, body, err := src.PeekReady()
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
}

func moveBuriedJobsTo(server, tube, destTube, destState string) {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()

	src := newTube(conn, tube)
	dst := newTube(conn, destTube)
	for {
		id, body, err := src.PeekBuried()
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

func clearTubes(server string, data url.Values) {
	for tube := range data {
		deleteAll(server, tube)
	}
}

func listTubesSorted(server string) []string {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return nil
	}
	defer conn.Close()
	tubes, _ := conn.ListTubes()
	return tubes
}
