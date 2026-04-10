package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/aurora/beanstalk"
)

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
