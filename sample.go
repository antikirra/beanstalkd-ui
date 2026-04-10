package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// addSample adds a sample job from an existing beanstalkd job.
func addSample(conf SelfConf, server string, data url.Values, w http.ResponseWriter) {
	var err error
	key := randToken()
	var sampleName, body string
	var sampleTTR int
	var tubes []string

	sampleName, sampleTTR, body, err = sampleValidate(server, data, w)
	if err != nil {
		return
	}

	sampleJobsMu.Lock()
	for k := range data { // range over map
		switch k {
		case "action", "tube", "addsamplejobid", "addsamplename", "addsamplettr", "server":
			continue
		default:
			t := strings.TrimSuffix(strings.TrimPrefix(k, `tubes[`), `]`)
			tubes = append(tubes, t)
			addSampleTube(t, key)
		}
	}
	sampleJobs.Jobs = append(sampleJobs.Jobs, SampleJob{
		Key:   key,
		Name:  sampleName,
		Tubes: tubes,
		Data:  body,
		TTR:   sampleTTR,
	})

	err = saveSample()
	sampleJobsMu.Unlock()
	if err != nil {
		return
	}
	fmt.Fprint(w, `{"result":true}`)
}

// jsonError writes a JSON error response and returns the error for propagation.
func jsonError(w http.ResponseWriter, msg string) error {
	fmt.Fprintf(w, `{"result":false,"error":%q}`, msg)
	return fmt.Errorf("%s", msg)
}

// sampleValidate validates and retrieves sample job data from beanstalkd.
func sampleValidate(server string, data url.Values, w http.ResponseWriter) (string, int, string, error) {
	sampleName := data.Get("addsamplename")
	sampleTTR := DefaultTTR

	if sampleName == "" {
		return "", sampleTTR, "", jsonError(w, "you should give a name with this sample")
	}
	if checkSampleJobsLocked(sampleName) {
		return "", sampleTTR, "", jsonError(w, "you already have a job with this name")
	}
	rawID := data.Get("addsamplejobid")
	if rawID == "" {
		return "", sampleTTR, "", jsonError(w, "job ID for add sample is empty")
	}
	jobID, err := strconv.Atoi(rawID)
	if err != nil {
		return "", sampleTTR, "", jsonError(w, "invalid beanstalkd job ID")
	}

	conn, err := dialBeanstalk(server)
	if err != nil {
		return "", sampleTTR, "", jsonError(w, "connect to beanstalkd server failed")
	}
	defer conn.Close()

	body, err := conn.Peek(uint64(jobID))
	if err != nil {
		return "", sampleTTR, "", jsonError(w, "read beanstalkd job content failed")
	}
	jobStats, err := conn.StatsJob(uint64(jobID))
	if err != nil {
		return "", sampleTTR, "", jsonError(w, "read beanstalkd job stats failed")
	}
	if ttr, err := strconv.Atoi(jobStats["ttr"]); err == nil {
		sampleTTR = ttr
	}
	return sampleName, sampleTTR, string(body), nil
}

// addSampleTube associates a sample job key with a tube.
func addSampleTube(tube, key string) {
	for k, v := range sampleJobs.Tubes {
		if v.Name == tube {
			sampleJobs.Tubes[k].Keys = append(sampleJobs.Tubes[k].Keys, key)
			return
		}
	}
	sampleJobs.Tubes = append(sampleJobs.Tubes, SampleTube{
		Name: tube,
		Keys: []string{key},
	})
}

// checkSampleJobs check if exists of sample job by given name.
// Caller must hold sampleJobsMu (read or write).
func checkSampleJobs(name string) bool {
	for _, v := range sampleJobs.Jobs {
		if v.Name == name {
			return true
		}
	}
	return false
}

// checkSampleJobsLocked is a thread-safe wrapper around checkSampleJobs.
func checkSampleJobsLocked(name string) bool {
	sampleJobsMu.RLock()
	defer sampleJobsMu.RUnlock()
	return checkSampleJobs(name)
}

// saveSample persists the sample jobs to the config file.
func saveSample() error {
	sampleJobsTOML, err := json.Marshal(sampleJobs)
	if err != nil {
		return err
	}
	pubConf.Sample.Storage = string(sampleJobsTOML)
	buf := new(bytes.Buffer)
	if err := toml.NewEncoder(buf).Encode(pubConf); err != nil {
		return err
	}

	file, err := os.OpenFile(configFile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = buf.WriteTo(file)
	return err
}

// findSampleJob returns a pointer to the sample job with the given key, or nil.
// Caller must hold sampleJobsMu (read or write).
func findSampleJob(key string) *SampleJob {
	for i := range sampleJobs.Jobs {
		if sampleJobs.Jobs[i].Key == key {
			return &sampleJobs.Jobs[i]
		}
	}
	return nil
}

// findSampleJobLocked is a thread-safe wrapper around findSampleJob.
func findSampleJobLocked(key string) *SampleJob {
	sampleJobsMu.RLock()
	defer sampleJobsMu.RUnlock()
	j := findSampleJob(key)
	if j == nil {
		return nil
	}
	cp := *j
	return &cp
}

// deleteSamples drop sample job by given key.
func deleteSamples(key string) {
	if key == "" {
		return
	}

	sampleJobsMu.Lock()
	// Remove the job with the matching key.
	filtered := make([]SampleJob, 0, len(sampleJobs.Jobs))
	for _, j := range sampleJobs.Jobs {
		if j.Key != key {
			filtered = append(filtered, j)
		}
	}
	sampleJobs.Jobs = filtered
	// Remove the key from all tube associations.
	for k, v := range sampleJobs.Tubes {
		filteredKeys := make([]string, 0, len(v.Keys))
		for _, t := range v.Keys {
			if t != key {
				filteredKeys = append(filteredKeys, t)
			}
		}
		sampleJobs.Tubes[k].Keys = filteredKeys
	}
	_ = saveSample()
	sampleJobsMu.Unlock()
}

// loadSample puts a job into tube by given sample job key.
func loadSample(server, tube, key string) {
	job := findSampleJobLocked(key)
	if job == nil || job.Data == "" {
		return
	}
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = newTube(conn, tube).Put(
		[]byte(job.Data), DefaultPriority,
		time.Duration(DefaultDelay)*time.Second,
		time.Duration(job.TTR)*time.Second,
	)
}

// newSample creates a new sample job from form input.
func newSample(conf SelfConf, server string, f url.Values, w http.ResponseWriter, r *http.Request) {
	upsertSample(conf, server, "", f, w, r)
}

// upsertSample creates or updates a sample job. If existingKey is non-empty, it reuses the key.
func upsertSample(conf SelfConf, server, existingKey string, f url.Values, w http.ResponseWriter, r *http.Request) {
	var err error
	key := existingKey
	if key == "" {
		key = randToken()
	}
	var name, body, ttr string
	var sampleTTR int
	var tubes []string
	alert := alertHTML("sjsa", "danger", " Required fields are not set")
	for k, v := range f {
		switch k {
		case "jobdata":
			body = v[0]
		case "name":
			name = v[0]
		case "ttr":
			ttr = v[0]
		case "action", "key":
			continue
		default:
			t := strings.TrimSuffix(strings.TrimPrefix(k, `tubes[`), `]`)
			tubes = append(tubes, t)
		}
	}
	if len(tubes) == 0 || name == "" || body == "" || ttr == "" {
		fmt.Fprint(w, tplSampleJobsManage(conf, tplSampleJobEdit(conf,"", alert), server))
		return
	}
	sampleTTR, err = strconv.Atoi(ttr)
	if err != nil {
		fmt.Fprint(w, tplSampleJobsManage(conf, tplSampleJobEdit(conf, "", alertHTML("sjsa", "danger", " You should give a correct TTR with this sample")), server))
		return
	}
	sampleJobsMu.Lock()
	if checkSampleJobs(name) {
		sampleJobsMu.Unlock()
		fmt.Fprint(w, tplSampleJobsManage(conf, tplSampleJobEdit(conf, "", alertHTML("sjsa", "danger", " You already have a job with this name")), server))
		return
	}
	for _, t := range tubes {
		addSampleTube(t, key)
	}
	sampleJobs.Jobs = append(sampleJobs.Jobs, SampleJob{
		Key:   key,
		Name:  name,
		Tubes: tubes,
		Data:  body,
		TTR:   sampleTTR,
	})
	err = saveSample()
	sampleJobsMu.Unlock()
	if err != nil {
		fmt.Fprint(w, tplSampleJobsManage(conf, tplSampleJobEdit(conf, "", alertHTML("sjsa", "danger", " Save sample job error")), server))
		return
	}
	w.Header().Set("Location", "./sample?action=manageSamples")
	w.WriteHeader(307)
}

// editSample updates an existing sample job.
func editSample(conf SelfConf, server string, f url.Values, key string, w http.ResponseWriter, r *http.Request) {
	deleteSamples(key)
	upsertSample(conf, server, key, f, w, r)
}

// getSampleJobList renders the sample jobs management table.
func getSampleJobList(conf SelfConf) string {
	sampleJobsMu.RLock()
	defer sampleJobsMu.RUnlock()
	if len(sampleJobs.Jobs) == 0 {
		return `<div class="clearfix"><div class="pull-left">There are no saved jobs.</div><div class="pull-right"><a href="?action=newSample" class="btn btn-default btn-sm"><i class="glyphicon glyphicon-plus"></i> Add job to samples</a></div></div>`
	}
	var tr, td, serverList, buf strings.Builder
	for _, j := range sampleJobs.Jobs {
		for _, v := range j.Tubes {
			for _, s := range conf.Servers {
				serverList.Reset()
				serverList.WriteString(`<li><a data-method="post" href="./tube?server=`)
				serverList.WriteString(s)
				serverList.WriteString(`&tube=`)
				serverList.WriteString(v)
				serverList.WriteString(`&action=loadSample&key=`)
				serverList.WriteString(j.Key)
				serverList.WriteString(`&redirect=`)
				serverList.WriteString(url.QueryEscape(`tube?action=manageSamples`))
				serverList.WriteString(`">`)
				serverList.WriteString(s)
				serverList.WriteString(`</a></li>`)
			}
			td.WriteString(` <div class="btn-group"><a class="btn btn-default btn-sm" href="#" data-toggle="dropdown"><i class="glyphicon glyphicon-forward"></i> `)
			td.WriteString(html.EscapeString(v))
			td.WriteString(`</a><button class="btn btn-default btn-sm dropdown-toggle" data-toggle="dropdown"><span class="caret"></span></button><ul class="dropdown-menu">`)
			td.WriteString(serverList.String())
			td.WriteString(`</ul></div>`)
		}
		tr.WriteString(`<tr><td style="line-height: 25px !important;"><a href="?action=editSample&key=`)
		tr.WriteString(j.Key)
		tr.WriteString(`">`)
		tr.WriteString(html.EscapeString(j.Name))
		tr.WriteString(`</a></td><td>`)
		tr.WriteString(td.String())
		tr.WriteString(`</td><td><div class="pull-right"><a class="btn btn-default btn-sm" href="?action=editSample&key=`)
		tr.WriteString(j.Key)
		tr.WriteString(`"><i class="glyphicon glyphicon-pencil"></i> Edit</a> <a class="btn btn-default btn-sm" data-method="post" href="?action=deleteSample&key=`)
		tr.WriteString(j.Key)
		tr.WriteString(`"><i class="glyphicon glyphicon-trash"></i> Delete</a></div></td></tr>`)
		td.Reset()
	}
	buf.WriteString(`<div class="clearfix"><div class="pull-right"><a href="?action=newSample" class="btn btn-default btn-sm"><i class="glyphicon glyphicon-plus"></i> Add job to samples</a></div></div><section id="summaryTable"><table class="table table-striped table-hover"><thead><tr><th>Name</th><th>Kick job to tubes</th><th></th></tr></thead><tbody>`)
	buf.WriteString(tr.String())
	buf.WriteString(`</tbody></table></section>`)
	return buf.String()
}
