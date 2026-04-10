package main

import (
	"fmt"
	"strings"
)

// currentTubeJobs assembles the full tube detail view: summary table, actions row, and job showcase.
func currentTubeJobs(conf SelfConf, server, tube string) string {
	table := currentTubeJobsSummaryTable(conf, server, tube)
	if table == "" {
		return fmt.Sprintf(`Tube %q not found or it is empty <br><br><a href="./server?server=%s"> &lt;&lt; back </a>`, tube, server)
	}

	var buf strings.Builder
	buf.WriteString(table)
	buf.WriteString(currentTubeJobsActionsRow(conf, server, tube))
	buf.WriteString(currentTubeJobsShowcase(conf, server, tube))
	return buf.String()
}
