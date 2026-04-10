package main

import (
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"

	"github.com/xuri/aurora/beanstalk"
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

// currentTubeJobsSummaryTable constructs a tube job table based on the given
// server and tube conf.
func currentTubeJobsSummaryTable(conf SelfConf, server, tube string) string {
	var err error
	var th, tr, td, template strings.Builder
	var conn *beanstalk.Conn
	if conn, err = dialBeanstalk(server); err != nil {
		for _, v := range conf.TubeFilters {
			th.WriteString(`<th>`)
			th.WriteString(v)
			th.WriteString(`</th>`)
		}
		if currentTubeStatisticCheck(server, tube) {
			th.WriteString(`<th> </th>`)
		}
		var buf strings.Builder
		buf.WriteString(`<section id="summaryTable"><div class="row"><div class="col-sm-12"><table class="table table-striped table-hover"><thead><tr><th>name</th>`)
		buf.WriteString(th.String())
		buf.WriteString(`</tr></thead><tbody></tbody></table></div></div></section>`)
		return buf.String()
	}
	defer conn.Close()
	tubes, _ := conn.ListTubes()
	for _, v := range conf.TubeFilters {
		th.WriteString(`<th>`)
		th.WriteString(v)
		th.WriteString(`</th>`)
	}
	if currentTubeStatisticCheck(server, tube) {
		th.WriteString(`<th> </th>`)
	}
	for _, v := range tubes {
		if v != tube {
			continue
		}
		tubeStats := newTube(conn, v)
		statsMap, err := tubeStats.Stats()
		if err != nil {
			continue
		}
		for _, stats := range conf.TubeFilters {
			td.WriteString(`<td>`)
			td.WriteString(statsMap[stats])
			td.WriteString(`</td>`)
		}
		tr.WriteString(`<tr><td>`)
		tr.WriteString(v)
		tr.WriteString(`</td>`)
		if currentTubeStatisticCheck(server, tube) {
			td.WriteString(`<td><a class="btn btn-xs btn-default" title="Statistics overview" href="./statistics?server=`)
			td.WriteString(server)
			td.WriteString(`&tube=`)
			td.WriteString(url.QueryEscape(v))
			td.WriteString(`"><span class="glyphicon glyphicon-stats"> </span></a></td>`)
		}
		tr.WriteString(td.String())
		tr.WriteString(`</tr>`)
		td.Reset()
	}
	template.WriteString(`<section id="summaryTable"><div class="row"><div class="col-sm-12"><table class="table table-striped table-hover"><thead><tr><th>name</th>`)
	template.WriteString(th.String())
	template.WriteString(`</tr></thead><tbody> `)
	template.WriteString(tr.String())
	template.WriteString(`</tbody></table></div></div></section>`)
	if tr.String() == `` {
		return ``
	}
	return template.String()
}

// currentTubeStatisticCheck reports whether statistics are available for a server/tube.
func currentTubeStatisticCheck(server, tube string) bool {
	statsConfigMu.RLock()
	collection := statsConfig.Collection
	statsConfigMu.RUnlock()
	if collection == 0 {
		return false
	}
	statisticsData.RLock()
	s, ok := statisticsData.Server[server]
	if !ok {
		statisticsData.RUnlock()
		return false
	}
	_, ok = s[tube]
	statisticsData.RUnlock()
	return ok
}

// currentTubeJobsActionsRow renders the tube actions section.
func currentTubeJobsActionsRow(conf SelfConf, server, tube string) string {
	var err error
	var conn *beanstalk.Conn
	var buf, pauseTimeLeft strings.Builder
	pause := strconv.Itoa(conf.TubePauseSeconds)
	if pause == "-1" {
		pause = "3600"
	}
	if conn, err = dialBeanstalk(server); err != nil {
		return ``
	}
	defer conn.Close()
	tubeStats := newTube(conn, tube)
	statsMap, _ := tubeStats.Stats()
	if statsMap["pause-time-left"] == "0" {
		pauseTimeLeft.WriteString(`<a class="btn btn-default btn-sm" data-method="post" href="?server=`)
		pauseTimeLeft.WriteString(server)
		pauseTimeLeft.WriteString(`&tube=`)
		pauseTimeLeft.WriteString(url.QueryEscape(tube))
		pauseTimeLeft.WriteString(`&action=pause&count=-1" title="Temporarily prevent jobs being reserved from the given tube. Pause for: `)
		pauseTimeLeft.WriteString(pause)
		pauseTimeLeft.WriteString(` seconds"><i class="glyphicon glyphicon-pause"></i> Pause tube</a>`)
	} else {
		pauseTimeLeft.WriteString(`<a class="btn btn-default btn-sm" data-method="post" href="?server=`)
		pauseTimeLeft.WriteString(server)
		pauseTimeLeft.WriteString(`&tube=`)
		pauseTimeLeft.WriteString(url.QueryEscape(tube))
		pauseTimeLeft.WriteString(`&action=pause&count=0" title="Pause seconds left: `)
		pauseTimeLeft.WriteString(statsMap["pause-time-left"])
		pauseTimeLeft.WriteString(`"><i class="glyphicon glyphicon-play"></i> Unpause tube</a>`)
	}
	buf.WriteString(`<section id="actionsRow"><b>Actions:</b> &nbsp;<a class="btn btn-default btn-sm" data-method="post" href="?server=`)
	buf.WriteString(server)
	buf.WriteString(`&tube=`)
	buf.WriteString(url.QueryEscape(tube))
	buf.WriteString(`&action=kick&count=1"><i class="glyphicon glyphicon-forward"></i> Kick 1 job</a> <form method="POST"><div class="btn-group" role="group"><button type="submit" class="btn btn-default btn-sm" style="margin-right: -2px;"><i class="glyphicon glyphicon-fast-forward"></i> Kick more </button><input type="hidden" name="server" value="`)
	buf.WriteString(server)
	buf.WriteString(`"><input type="hidden" name="tube" value="`)
	buf.WriteString(url.QueryEscape(tube))
	buf.WriteString(`"><input type="hidden" name="action" value="kick"><input type="number" value="10" name="count" min="0" step="1" size="4" class="form-control input-sm" style="padding: 5px 2px 5px 12px; text-align: center;"></div></form> `)
	buf.WriteString(pauseTimeLeft.String())
	buf.WriteString(` &nbsp;&nbsp;&nbsp;&nbsp;&nbsp; <div class="btn-group"><a data-toggle="modal" class="btn btn-success btn-sm" href="#" id="addJob"><i class="glyphicon glyphicon-plus-sign glyphicon-white"></i> Add job</a><button class="btn btn-success btn-sm dropdown-toggle" data-toggle="dropdown"><span class="caret"></span></button><ul class="dropdown-menu">`)
	buf.WriteString(currentTubeJobsActionsRowSample(server, tube))
	buf.WriteString(`</ul></div></section>`)
	return buf.String()
}

// currentTubeJobsActionsRowSample renders the sample jobs dropdown menu.
func currentTubeJobsActionsRowSample(server, tube string) string {
	var sample strings.Builder
	sampleJobsMu.RLock()
	for _, v := range sampleJobs.Tubes {
		if v.Name != tube {
			continue
		}
		if len(v.Keys) == 0 {
			continue
		}
		for _, k := range v.Keys {
			for _, j := range sampleJobs.Jobs {
				if j.Key != k {
					continue
				}
				sample.WriteString(`<li><a data-method="post" href="?server=`)
				sample.WriteString(server)
				sample.WriteString(`&tube=`)
				sample.WriteString(url.QueryEscape(tube))
				sample.WriteString(`&action=loadSample&key=`)
				sample.WriteString(j.Key)
				sample.WriteString(`">`)
				sample.WriteString(html.EscapeString(j.Name))
				sample.WriteString(`</a></li>`)
			}
		}
	}
	sampleJobsMu.RUnlock()
	if sample.String() == "" {
		return `<li><a href="javascript:void(0);">There are no sample jobs</a></li>`
	}
	sample.WriteString(`<li class="divider"></li><li><a href="./sample?action=manageSamples">Manage samples</a></li>`)
	return sample.String()
}

// currentTubeJobsShowcase return a section include three stats of job, call
// currentTubeJobsShowcaseSections function and get that return value based on
// the given server and tube config.
func currentTubeJobsShowcase(conf SelfConf, server, tube string) string {
	var buf strings.Builder
	buf.WriteString(`<section class="jobsShowcase">`)
	buf.WriteString(currentTubeJobsShowcaseSections(conf, server, tube))
	buf.WriteString(`</section>`)
	return buf.String()
}

// emptyStateHTML returns an "empty" placeholder for a job state section.
func emptyStateHTML(state string) string {
	return fmt.Sprintf(`<hr><div class="pull-left"><h3>Next job in "%s" state</h3></div><div class="clearfix"></div><i>empty</i>`, state)
}

// renderJobStatsTable renders the stats key-value table for a single job.
func renderJobStatsTable(statsJob map[string]string) string {
	var buf strings.Builder
	for _, k := range jobStatsOrder {
		fmt.Fprintf(&buf, `<tr><td>%s</td><td>%s</td></tr>`, k, statsJob[k])
	}
	return buf.String()
}

// renderJobMoveMenu renders the "Move all to" dropdown menu items.
func renderJobMoveMenu(server, tube, state string, tubes []string) string {
	var buf strings.Builder
	for _, t := range tubes {
		fmt.Fprintf(&buf, `<li><a data-method="post" href="%s&destTube=%s&state=%s">%s</a></li>`,
			tubeActionURL(server, tube, "moveJobsTo"), url.QueryEscape(t), state, html.EscapeString(t))
	}
	return buf.String()
}

// renderJobActions renders the action buttons (add sample, move, delete) for a job.
func renderJobActions(server, tube, state string, jobID uint64, tubes []string) string {
	base := tubeURL(server, tube)
	idStr := strconv.FormatUint(jobID, 10)
	moveMenu := renderJobMoveMenu(server, tube, state, tubes)

	return fmt.Sprintf(
		`<div class="pull-right"><div style="margin-bottom: 3px;">`+
			`<a class="btn btn-sm btn-info addSample" data-jobid="%s" href="%s&action=addSample">`+
			`<i class="glyphicon glyphicon-plus glyphicon-white"></i> Add to samples</a> `+
			`<div class="btn-group"> <button class="btn btn-info btn-sm dropdown-toggle" data-toggle="dropdown">`+
			`<i class="glyphicon glyphicon-arrow-right glyphicon-white"></i> Move all %s to </button>`+
			`<ul class="dropdown-menu">`+
			`<li><input class="moveJobsNewTubeName input-medium" type="text" data-href="%s&action=moveJobsTo&state=%s&destTube=" placeholder="New tube name"/></li>`+
			`%s`+
			`<li class="divider"></li>`+
			`<li><a data-method="post" href="%s&action=moveJobsTo&destState=buried&state=%s">Buried</a></li>`+
			`</ul></div> `+
			`<a class="btn btn-sm btn-danger" data-method="post" href="%s&state=%s&action=deleteAll&count=1" onclick="return confirm('This process might hang a while on tubes with lots of jobs. Are you sure you want to continue?');">`+
			`<i class="glyphicon glyphicon-trash glyphicon-white"></i> Delete all %s jobs</a> `+
			`<a class="btn btn-sm btn-danger" data-method="post" href="%s&state=%s&action=deleteJob&jobid=%s">`+
			`<i class="glyphicon glyphicon-remove glyphicon-white"></i> Delete</a></div></div>`,
		idStr, base, state, base, state, moveMenu, base, state, base, state, state, base, state, idStr)
}

// currentTubeJobsShowcaseSections renders job detail sections for ready, delayed, and buried states.
func currentTubeJobsShowcaseSections(conf SelfConf, server, tube string) string {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return emptyStateHTML("ready") + emptyStateHTML("delayed") + emptyStateHTML("buried")
	}
	defer conn.Close()

	t := newTube(conn, tube)
	peekFns := []func() (uint64, []byte, error){t.PeekReady, t.PeekDelayed, t.PeekBuried}
	states := []string{"ready", "delayed", "buried"}
	tubes, _ := conn.ListTubes()

	var buf strings.Builder
	for i, state := range states {
		jobID, jobBody, err := peekFns[i]()
		if err != nil || jobBody == nil {
			buf.WriteString(emptyStateHTML(state))
			continue
		}
		statsJob, err := conn.StatsJob(jobID)
		if err != nil {
			buf.WriteString(emptyStateHTML(state))
			continue
		}

		statsTable := renderJobStatsTable(statsJob)
		actions := renderJobActions(server, tube, state, jobID, tubes)
		jobData := preformat(conf, jobBody)

		fmt.Fprintf(&buf,
			`<hr><div class="pull-left"><h3>Next job in "%s" state</h3></div>`+
				`<div class="clearfix"></div><div class="row show-grid">`+
				`<div class="col-sm-3"><table class="table"><thead><tr><th>Stats:</th><th>&nbsp;</th></tr></thead><tbody>%s</tbody></table></div>`+
				`<div class="col-sm-9"><div class="clearfix"><div class="pull-left"><b>Job data:</b></div>%s</div>`+
				`<pre><code>%s</code></pre></div></div>`,
			state, statsTable, actions, jobData)
	}
	return buf.String()
}

// currentTubeSearchResults constructs a search result table by given server,
// tube, search result limit and search content.
func currentTubeSearchResults(server, tube, limit, searchStr string, result []SearchResult) string {
	var buf, tr strings.Builder
	if len(result) == 0 {
		buf.WriteString(`<br/>No results found for <b>`)
		buf.WriteString(html.EscapeString(searchStr))
		buf.WriteString(`</b> in tube: <b>`)
		buf.WriteString(tube)
		buf.WriteString(`</b>`)
		return buf.String()
	}
	for k, job := range result {
		tr.WriteString(`<tr><td>`)
		tr.WriteString(strconv.Itoa(int(job.ID)))
		tr.WriteString(`</td><td>`)
		tr.WriteString(job.State)
		tr.WriteString(`</td><td class="ellipsize">`)
		tr.WriteString(html.EscapeString(job.Data))
		tr.WriteString(`</td><td><div class="dropdown btn-group-xs"><button class="btn btn-default dropdown-toggle" type="button" id="dropdownMenu`)
		tr.WriteString(strconv.Itoa(k))
		tr.WriteString(`" data-toggle="dropdown" aria-expanded="true"> Actions <span class="caret"></span></button><ul class="dropdown-menu" role="menu" aria-labelledby="dropdownMenu`)
		tr.WriteString(strconv.Itoa(k))
		tr.WriteString(`"><li role="presentation"><a role="menuitem" class="addSample" data-jobid="`)
		tr.WriteString(strconv.Itoa(int(job.ID)))
		tr.WriteString(`" href="?server=`)
		tr.WriteString(server)
		tr.WriteString(`&tube=`)
		tr.WriteString(url.QueryEscape(tube))
		tr.WriteString(`&action=addSample"><i class="glyphicon glyphicon-plus glyphicon-white"></i> Add to samples </a></li><li role="presentation"><a role="menuitem" data-method="post" href="?server=`)
		tr.WriteString(server)
		tr.WriteString(`&tube=`)
		tr.WriteString(url.QueryEscape(tube))
		tr.WriteString(`&state=`)
		tr.WriteString(job.State)
		tr.WriteString(`&action=deleteJob&jobid=`)
		tr.WriteString(strconv.Itoa(int(job.ID)))
		tr.WriteString(`"><i class="glyphicon glyphicon-remove glyphicon-white"></i> Delete</a> </li><li role="presentation"><a role="menuitem" data-method="post" href="?server=`)
		tr.WriteString(server)
		tr.WriteString(`&tube=`)
		tr.WriteString(url.QueryEscape(tube))
		tr.WriteString(`&state=`)
		tr.WriteString(job.State)
		tr.WriteString(`&action=kickJob&jobid=`)
		tr.WriteString(strconv.Itoa(int(job.ID)))
		tr.WriteString(`"><i class="glyphicon glyphicon-forward glyphicon-white"></i> Kick </a></li></ul></div></td></tr>`)
	}
	buf.WriteString(`<section id="actionsRow"><a class="btn btn-default btn-sm" href="?server=`)
	buf.WriteString(server)
	buf.WriteString(`&tube=`)
	buf.WriteString(url.QueryEscape(tube))
	buf.WriteString(`"><i class="glyphicon glyphicon-backward"></i>  &nbsp;Back to tube</a></section><section id="searchResult"><div class="row"><div class="col-sm-12"><table class="table table-striped table-hover" style="table-layout:fixed;"><thead><tr><th class="col-md-1">id</th><th class="col-md-1">state</th><th>data</th><th class="col-md-1">action</th></tr></thead><tbody>`)
	buf.WriteString(tr.String())
	buf.WriteString(`</tbody></table></div></div>First `)
	buf.WriteString(limit)
	buf.WriteString(` rows are displayed for each state.<br/><br/></section>`)
	return buf.String()
}
