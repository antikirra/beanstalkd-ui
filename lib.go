package main

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/aurora/beanstalk"
)

// dialBeanstalk opens a new connection to the given beanstalkd server.
func dialBeanstalk(server string) (*beanstalk.Conn, error) {
	return beanstalk.Dial("tcp", server)
}

// newTube creates a Tube handle on an existing connection.
func newTube(conn *beanstalk.Conn, name string) *beanstalk.Tube {
	return &beanstalk.Tube{Conn: conn, Name: name}
}

// addJob puts a job into the specified tube.
func addJob(server, tube, data, priority, delay, ttr string) {
	pri, err := strconv.ParseUint(priority, 10, 32)
	if err != nil || pri > math.MaxUint32 {
		pri = uint64(DefaultPriority)
	}
	d, err := strconv.Atoi(delay)
	if err != nil {
		d = DefaultDelay
	}
	t, err := strconv.Atoi(ttr)
	if err != nil {
		t = DefaultTTR
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

// deleteJob deletes a single job by ID.
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

// deleteAll removes all jobs (ready, buried, delayed) from a tube.
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

// drainTube repeatedly peeks and deletes jobs until the peek function returns an error.
func drainTube(conn *beanstalk.Conn, peek func() (uint64, []byte, error)) {
	for {
		id, _, err := peek()
		if err != nil {
			return
		}
		_ = conn.Delete(id)
	}
}

// kick moves up to count buried jobs back to the ready queue.
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

// kickJob kicks a single job by its ID.
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

// pause pauses or unpauses a tube based on the count parameter.
// count="-1" pauses for the configured duration, count="0" unpauses.
func pause(conf SelfConf, server, tube, count string) {
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
			dur = DefaultTubePauseSeconds * time.Second
		}
		_ = t.Pause(dur)
	case "0":
		_ = t.Pause(0)
	}
}

// moveJobsTo dispatches job movement based on the source state.
func moveJobsTo(server, tube, destTube, state, destState string) {
	switch state {
	case "ready":
		moveReadyJobsTo(server, tube, destTube, destState)
	case "buried":
		moveBuriedJobsTo(server, tube, destTube, destState)
	}
}

// moveReadyJobsTo moves ready jobs to another tube or buries them.
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
			if err := conn.Bury(id, DefaultPriority); err != nil {
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
			if _, err := dst.Put(body, DefaultPriority, DefaultDelay, DefaultTTR); err != nil {
				return
			}
			if err := conn.Delete(id); err != nil {
				return
			}
		}
	}
}

// moveBuriedJobsTo moves buried jobs from one tube to another.
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
		if _, err := dst.Put(body, DefaultPriority, DefaultDelay, DefaultTTR); err != nil {
			return
		}
		if err := conn.Delete(id); err != nil {
			return
		}
	}
}

// clearTubes deletes all jobs from every tube listed in data.
func clearTubes(server string, data url.Values) {
	for tube := range data {
		deleteAll(server, tube)
	}
}

// searchTube searches for jobs containing searchStr across ready, delayed, and buried states.
func searchTube(conf SelfConf, server, tube, limit, searchStr string) string {
	table := currentTubeJobsSummaryTable(conf, server, tube)
	if table == "" {
		return fmt.Sprintf(`Tube %q not found or it is empty <br><br><a href="./server?server=%s"> &lt;&lt; back </a>`, tube, server)
	}

	searchLimit, err := strconv.Atoi(limit)
	if err != nil {
		return table
	}

	conn, err := dialBeanstalk(server)
	if err != nil {
		return table
	}
	defer conn.Close()

	stats, err := conn.Stats()
	if err != nil {
		return table
	}
	totalJobs, err := strconv.ParseUint(stats["total-jobs"], 10, 64)
	if err != nil {
		return table
	}

	var result []SearchResult
	for _, state := range []string{"ready", "delayed", "buried"} {
		cnt := 0
		for id := totalJobs; id > 0 && cnt < searchLimit; id-- {
			if r := matchJob(conn, tube, searchStr, state, id); r != nil {
				result = append(result, *r)
				cnt++
			}
		}
	}
	return table + currentTubeSearchResults(server, tube, limit, searchStr, result)
}

// matchJob checks whether a single job matches the search criteria.
func matchJob(conn *beanstalk.Conn, tube, searchStr, state string, id uint64) *SearchResult {
	jobStats, err := conn.StatsJob(id)
	if err != nil {
		return nil
	}
	if jobStats["tube"] != tube || jobStats["state"] != state {
		return nil
	}
	body, err := conn.Peek(id)
	if err != nil {
		return nil
	}
	if !strings.Contains(string(body), searchStr) {
		return nil
	}
	return &SearchResult{ID: id, State: state, Data: string(body)}
}
