package api

import (
	"container/list"
	"context"
	cryptoRand "crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beanstalkd/go-beanstalk"
	"github.com/xuri/aurora/internal/config"
	"github.com/xuri/aurora/internal/model"
)

// Handlers holds all dependencies and mutable state for HTTP handlers.
type Handlers struct {
	log        *slog.Logger
	tmpl       *templateSet
	cfg        *config.Config
	configPath string

	sampleJobs   model.SampleJobs
	sampleJobsMu sync.RWMutex

	statsConfig   model.StatsConfig
	statsConfigMu sync.RWMutex

	statsData model.StatisticsData
	notify    chan bool

	updateInfo string
	updateOnce sync.Once
}

// NewHandlers creates a Handlers instance with parsed templates and initial state.
func NewHandlers(log *slog.Logger, cfg *config.Config, configPath string, tmplFS fs.FS, samples model.SampleJobs) (*Handlers, error) {
	tmpl, err := parseTemplates(tmplFS)
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Handlers{
		log:        log,
		tmpl:       tmpl,
		cfg:        cfg,
		configPath: configPath,
		sampleJobs: samples,
		statsData: model.StatisticsData{
			RWMutex: new(sync.RWMutex),
			Server:  make(map[string]map[string]map[string]*list.List),
		},
		notify: make(chan bool, 1),
	}, nil
}

// --- Cookie helpers ---

func readCookies(r *http.Request, cfg *config.Config) model.SelfConf {
	var conf model.SelfConf

	conf.Servers = append(conf.Servers, cfg.Servers...)
	if c := cookieValue(r, "beansServers"); c != "" {
		for _, v := range strings.Split(c, ";") {
			if isValidServer(v) {
				conf.Servers = append(conf.Servers, v)
			}
		}
	}
	conf.Servers = compactUnique(conf.Servers)

	if c := cookieValue(r, "filter"); c != "" {
		conf.Filter = compactUnique(strings.Split(c, ","))
	} else {
		conf.Filter = []string{
			"current-connections", "current-jobs-buried", "current-jobs-delayed",
			"current-jobs-ready", "current-jobs-reserved", "current-jobs-urgent", "current-tubes",
		}
	}

	if c := cookieValue(r, "tubefilter"); c != "" {
		conf.TubeFilters = compactUnique(strings.Split(c, ","))
	} else {
		conf.TubeFilters = []string{
			"current-jobs-urgent", "current-jobs-ready", "current-jobs-reserved",
			"current-jobs-delayed", "current-jobs-buried", "total-jobs",
		}
	}

	conf.TubeSelector = rawCookieValue(r, "tubeSelector")
	conf.TubePauseSeconds = readIntCookie(r, "tubePauseSeconds", -1)
	conf.AutoRefreshTimeoutMs = readIntCookie(r, "autoRefreshTimeoutMs", 500)
	conf.SearchResultLimit = readIntCookie(r, "searchResultLimit", 25)
	conf.DisableJSONDecode = readBoolCookie(r, "isDisabledJsonDecode")
	conf.DisableJobDataHighlight = readBoolCookie(r, "isDisabledJobDataHighlight")
	conf.EnableBase64Decode = readBoolCookie(r, "isEnabledBase64Decode")

	return conf
}

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	v, _ := url.QueryUnescape(c.Value)
	return v
}

func rawCookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func readIntCookie(r *http.Request, name string, defaultValue int) int {
	c, err := r.Cookie(name)
	if err != nil {
		return defaultValue
	}
	v, err := strconv.Atoi(c.Value)
	if err != nil {
		return defaultValue
	}
	return v
}

func readBoolCookie(r *http.Request, name string) bool {
	c, err := r.Cookie(name)
	if err != nil {
		return false
	}
	return c.Value == "1"
}

func removeServerInCookie(conf model.SelfConf, server string, w http.ResponseWriter) {
	var buf strings.Builder
	for _, v := range conf.Servers {
		if v != server {
			buf.WriteString(v)
			buf.WriteByte(';')
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "beansServers",
		Value:    url.QueryEscape(buf.String()),
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
}

func isValidServer(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	_, err = strconv.Atoi(port)
	return err == nil
}

func compactUnique(s []string) []string {
	result := slices.DeleteFunc(s, func(v string) bool { return v == "" })
	slices.Sort(result)
	return slices.Compact(result)
}

// --- Handlers ---

func (h *Handlers) handleServers(w http.ResponseWriter, r *http.Request) {
	conf := readCookies(r, h.cfg)
	h.render(w, r, "servers.html", &pageData{
		PageTitle:          "Servers",
		ServerStats:        h.getServerStats(conf),
		Filter:             conf.Filter,
		BinlogStatsGroups:  model.BinlogStatsGroups,
		CmdStatsGroups:     model.CmdStatsGroups,
		CurrentStatsGroups: model.CurrentStatsGroups,
		OtherStatsGroups:   model.OtherStatsGroups,
	})
}

func (h *Handlers) handleSettings(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "settings.html", &pageData{PageTitle: "Settings"})
}

func (h *Handlers) handleServersReload(w http.ResponseWriter, r *http.Request) {
	conf := readCookies(r, h.cfg)
	h.renderFragment(w, r, "server_table_inner", &pageData{
		ServerStats: h.getServerStats(conf),
		Filter:      conf.Filter,
		Conf:        conf,
	})
}

func (h *Handlers) handleServerRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	conf := readCookies(r, h.cfg)
	server := r.URL.Query().Get("server")
	removeServerInCookie(conf, server, w)
	h.cfg.RemoveServer(server)
	_ = config.Save(h.configPath, h.cfg)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) handleServer(w http.ResponseWriter, r *http.Request) {
	conf := readCookies(r, h.cfg)
	server := r.URL.Query().Get("server")
	action := r.URL.Query().Get("action")

	switch action {
	case "reloader":
		h.renderFragment(w, r, "tube_table_inner", &pageData{
			TubeStats:     h.getTubeStats(conf, server),
			TubeFilters:   conf.TubeFilters,
			CurrentServer: server,
			Conf:          conf,
		})
	case "clearTubes":
		_ = r.ParseForm()
		clearTubes(server, r.Form)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"result":true}`)
	default:
		h.render(w, r, "server.html", &pageData{
			PageTitle:     server,
			CurrentServer: server,
			TubeStats:     h.getTubeStats(conf, server),
			TubeFilters:   conf.TubeFilters,
			Tubes:         listTubesSorted(server),
			TubeStatFields: model.TubeStatFields,
		})
	}
}

func (h *Handlers) handleTube(w http.ResponseWriter, r *http.Request) {
	conf := readCookies(r, h.cfg)
	q := r.URL.Query()
	server := q.Get("server")
	tube := q.Get("tube")
	action := q.Get("action")

	switch action {
	case "addjob":
		addJob(server,
			r.PostFormValue("tubeName"), r.PostFormValue("tubeData"),
			r.PostFormValue("tubePriority"), r.PostFormValue("tubeDelay"), r.PostFormValue("tubeTtr"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"result":true}`)
	case "search":
		searchResults, totalJobs := h.searchTube(conf, server, tube, q.Get("limit"), q.Get("searchStr"))
		_ = totalJobs
		h.render(w, r, "tube.html", h.buildTubeData(conf, server, tube, searchResults, q.Get("searchStr"), q.Get("limit")))
	case "addSample":
		_ = r.ParseForm()
		h.addSampleFromJob(conf, server, r.Form, w)
	case "kick":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		kick(server, tube, q.Get("count"))
		setFlash(w, "success", "Jobs kicked")
		h.redirectToTube(w, r, server, tube)
	case "kickJob":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		kickJob(server, tube, q.Get("jobid"))
		setFlash(w, "success", "Job kicked")
		h.redirectToTube(w, r, server, tube)
	case "pause":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		pause(conf, server, tube, q.Get("count"))
		setFlash(w, "success", "Tube pause updated")
		h.redirectToTube(w, r, server, tube)
	case "moveJobsTo":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		destTube := q.Get("destTube")
		if destTube == "" {
			destTube = tube
		}
		moveJobsTo(server, tube, destTube, q.Get("state"), q.Get("destState"))
		setFlash(w, "success", "Jobs moved")
		h.redirectToTube(w, r, server, destTube)
	case "deleteAll":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		deleteAll(server, tube)
		setFlash(w, "success", "All jobs deleted")
		h.redirectToTube(w, r, server, tube)
	case "deleteJob":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		deleteJob(server, tube, q.Get("jobid"))
		setFlash(w, "success", "Job deleted")
		h.redirectToTube(w, r, server, tube)
	case "loadSample":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		loadSample(server, tube, q.Get("key"), h)
		setFlash(w, "success", "Sample loaded")
		h.redirectToTube(w, r, server, tube)
	case "reloader":
		td := h.buildTubeData(conf, server, tube, nil, "", "")
		td.Conf = conf
		h.renderFragment(w, r, "tube_content_inner", td)
	default:
		h.render(w, r, "tube.html", h.buildTubeData(conf, server, tube, nil, "", ""))
	}
}

func (h *Handlers) redirectToTube(w http.ResponseWriter, r *http.Request, server, tube string) {
	target := fmt.Sprintf("/tube?server=%s&tube=%s", url.QueryEscape(server), url.QueryEscape(tube))
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *Handlers) buildTubeData(conf model.SelfConf, server, tube string, searchResults []model.SearchResult, searchStr, searchLimit string) *pageData {
	data := &pageData{
		PageTitle:     tube + " — " + server,
		CurrentServer: server,
		CurrentTube:   tube,
		TubeFilters:   conf.TubeFilters,
		HasStats:      h.tubeHasStatistics(server, tube),
		SearchResults: searchResults,
		SearchStr:     searchStr,
		SearchLimit:   searchLimit,
	}

	conn, err := dialBeanstalk(server)
	if err != nil {
		return data
	}
	defer conn.Close()

	t := newTube(conn, tube)
	data.TubeInfo, _ = t.Stats()
	data.Tubes, _ = conn.ListTubes()
	sort.Strings(data.Tubes)

	if data.TubeInfo != nil {
		data.PauseSeconds = data.TubeInfo["pause-time-left"]
	}

	// Peek jobs for showcase.
	peekFns := []func() (uint64, []byte, error){t.PeekReady, t.PeekDelayed, t.PeekBuried}
	targets := []*jobData{nil, nil, nil}
	for i, peekFn := range peekFns {
		jobID, jobBody, err := peekFn()
		if err != nil || jobBody == nil {
			continue
		}
		statsJob, err := conn.StatsJob(jobID)
		if err != nil {
			continue
		}
		targets[i] = &jobData{
			ID:    jobID,
			Data:  string(jobBody),
			Stats: statsJob,
		}
	}
	data.ReadyJob = targets[0]
	data.DelayedJob = targets[1]
	data.BuriedJob = targets[2]

	// Sample jobs for this tube.
	h.sampleJobsMu.RLock()
	for _, st := range h.sampleJobs.Tubes {
		if st.Name != tube {
			continue
		}
		var samples []sampleForTube
		for _, key := range st.Keys {
			for _, j := range h.sampleJobs.Jobs {
				if j.Key == key {
					samples = append(samples, sampleForTube{Key: key, Name: j.Name})
				}
			}
		}
		if data.SampleTubeMap == nil {
			data.SampleTubeMap = make(map[string][]sampleForTube)
		}
		data.SampleTubeMap[tube] = samples
	}
	h.sampleJobsMu.RUnlock()

	return data
}

// --- Sample handlers ---

func (h *Handlers) handleSamples(w http.ResponseWriter, r *http.Request) {
	conf := readCookies(r, h.cfg)
	q := r.URL.Query()
	server := q.Get("server")

	switch q.Get("action") {
	case "manageSamples":
		h.sampleJobsMu.RLock()
		jobs := make([]model.SampleJob, len(h.sampleJobs.Jobs))
		copy(jobs, h.sampleJobs.Jobs)
		h.sampleJobsMu.RUnlock()
		h.render(w, r, "samples.html", &pageData{
			PageTitle:     "Samples",
			CurrentServer: server,
			SampleJobs:    jobs,
		})
	case "newSample":
		h.render(w, r, "sample_edit.html", &pageData{
			PageTitle:     "New Sample",
			CurrentServer: server,
			Servers:       conf.Servers,
			ServerTubes:   h.getServerTubesMap(conf),
		})
	case "editSample":
		key := q.Get("key")
		job := h.findSampleJobLocked(key)
		title := "Edit Sample"
		if job != nil {
			title = "Edit: " + job.Name
		}
		h.render(w, r, "sample_edit.html", &pageData{
			PageTitle:     title,
			CurrentServer: server,
			SampleJob:     job,
			Servers:       conf.Servers,
			ServerTubes:   h.getServerTubesMap(conf),
		})
	case "actionNewSample":
		_ = r.ParseForm()
		h.newSample(conf, server, r.Form, w, r)
	case "actionEditSample":
		_ = r.ParseForm()
		h.editSample(conf, server, r.Form, q.Get("key"), w, r)
	case "deleteSample":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h.deleteSamples(q.Get("key"))
		setFlash(w, "success", "Sample deleted")
		http.Redirect(w, r, "/sample?action=manageSamples", http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/sample?action=manageSamples", http.StatusSeeOther)
	}
}

// --- Statistics handlers ---

func (h *Handlers) handleStatistics(w http.ResponseWriter, r *http.Request) {
	conf := readCookies(r, h.cfg)
	q := r.URL.Query()
	server := q.Get("server")
	tube := q.Get("tube")

	switch q.Get("action") {
	case "preference":
		data := h.buildStatsPrefData(conf)
		data.PageTitle = "Statistics Preference"
		h.render(w, r, "statistics_pref.html", data)
	case "save":
		_ = r.ParseForm()
		h.statisticPreferenceSave(conf, r.Form, w, r)
	case "reloader":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, h.statisticsJSON(server, tube))
	default:
		h.render(w, r, "statistics.html", &pageData{
			PageTitle:   "Statistics",
			StatsServer: server,
			StatsTube:   tube,
		})
	}
}

// --- Data fetching helpers ---

func (h *Handlers) getServerStats(conf model.SelfConf) []serverStat {
	stats := make([]serverStat, 0, len(conf.Servers))
	for _, addr := range conf.Servers {
		ss := serverStat{Addr: addr}
		conn, err := dialBeanstalk(addr)
		if err != nil {
			stats = append(stats, ss)
			continue
		}
		ss.Online = true
		ss.Stats, _ = conn.Stats()
		conn.Close()
		stats = append(stats, ss)
	}
	return stats
}

func (h *Handlers) getTubeStats(conf model.SelfConf, server string) []tubeStat {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return nil
	}
	defer conn.Close()

	tubes, _ := conn.ListTubes()
	sort.Strings(tubes)

	stats := make([]tubeStat, 0, len(tubes))
	for _, name := range tubes {
		ts := tubeStat{Name: name}
		t := newTube(conn, name)
		ts.Stats, _ = t.Stats()
		stats = append(stats, ts)
	}
	return stats
}

func (h *Handlers) getServerTubesMap(conf model.SelfConf) map[string][]string {
	result := make(map[string][]string, len(conf.Servers))
	for _, server := range conf.Servers {
		tubes := listTubesSorted(server)
		sort.Strings(tubes)
		result[server] = tubes
	}
	return result
}

func (h *Handlers) tubeHasStatistics(server, tube string) bool {
	h.statsConfigMu.RLock()
	collection := h.statsConfig.Collection
	h.statsConfigMu.RUnlock()
	if collection == 0 {
		return false
	}
	h.statsData.RLock()
	defer h.statsData.RUnlock()
	s, ok := h.statsData.Server[server]
	if !ok {
		return false
	}
	_, ok = s[tube]
	return ok
}

// --- Search ---

func (h *Handlers) searchTube(conf model.SelfConf, server, tube, limit, searchStr string) ([]model.SearchResult, uint64) {
	searchLimit, err := strconv.Atoi(limit)
	if err != nil {
		return nil, 0
	}
	conn, err := dialBeanstalk(server)
	if err != nil {
		return nil, 0
	}
	defer conn.Close()

	stats, err := conn.Stats()
	if err != nil {
		return nil, 0
	}
	totalJobs, err := strconv.ParseUint(stats["total-jobs"], 10, 64)
	if err != nil {
		return nil, 0
	}

	var result []model.SearchResult
	for _, state := range []string{"ready", "delayed", "buried"} {
		cnt := 0
		for id := totalJobs; id > 0 && cnt < searchLimit; id-- {
			if r := matchJob(conn, tube, searchStr, state, id); r != nil {
				result = append(result, *r)
				cnt++
			}
		}
	}
	return result, totalJobs
}

func matchJob(conn *beanstalk.Conn, tube, searchStr, state string, id uint64) *model.SearchResult {
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
	return &model.SearchResult{ID: id, State: state, Data: string(body)}
}

// --- Sample CRUD ---

func (h *Handlers) findSampleJobLocked(key string) *model.SampleJob {
	h.sampleJobsMu.RLock()
	defer h.sampleJobsMu.RUnlock()
	for i := range h.sampleJobs.Jobs {
		if h.sampleJobs.Jobs[i].Key == key {
			cp := h.sampleJobs.Jobs[i]
			return &cp
		}
	}
	return nil
}

func (h *Handlers) addSampleFromJob(conf model.SelfConf, server string, data url.Values, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")

	sampleName := data.Get("addsamplename")
	if sampleName == "" {
		fmt.Fprint(w, `{"result":false,"error":"sample name required"}`)
		return
	}

	h.sampleJobsMu.RLock()
	nameExists := h.checkSampleJobName(sampleName)
	h.sampleJobsMu.RUnlock()
	if nameExists {
		fmt.Fprint(w, `{"result":false,"error":"sample with this name already exists"}`)
		return
	}

	rawID := data.Get("addsamplejobid")
	jobID, err := strconv.Atoi(rawID)
	if err != nil {
		fmt.Fprint(w, `{"result":false,"error":"invalid job ID"}`)
		return
	}

	conn, err := dialBeanstalk(server)
	if err != nil {
		fmt.Fprint(w, `{"result":false,"error":"connect to beanstalkd failed"}`)
		return
	}
	defer conn.Close()

	body, err := conn.Peek(uint64(jobID))
	if err != nil {
		fmt.Fprint(w, `{"result":false,"error":"read job content failed"}`)
		return
	}
	jobStats, err := conn.StatsJob(uint64(jobID))
	if err != nil {
		fmt.Fprint(w, `{"result":false,"error":"read job stats failed"}`)
		return
	}
	sampleTTR := model.DefaultTTR
	if ttr, err := strconv.Atoi(jobStats["ttr"]); err == nil {
		sampleTTR = ttr
	}

	key := randToken()
	var tubes []string
	h.sampleJobsMu.Lock()
	for k := range data {
		switch k {
		case "action", "tube", "addsamplejobid", "addsamplename", "addsamplettr", "server":
			continue
		default:
			t := strings.TrimSuffix(strings.TrimPrefix(k, "tubes["), "]")
			tubes = append(tubes, t)
			h.addSampleTube(t, key)
		}
	}
	h.sampleJobs.Jobs = append(h.sampleJobs.Jobs, model.SampleJob{
		Key:   key,
		Name:  sampleName,
		Tubes: tubes,
		Data:  string(body),
		TTR:   sampleTTR,
	})
	err = h.saveSample()
	h.sampleJobsMu.Unlock()
	if err != nil {
		return
	}
	fmt.Fprint(w, `{"result":true}`)
}

func (h *Handlers) checkSampleJobName(name string) bool {
	for _, v := range h.sampleJobs.Jobs {
		if v.Name == name {
			return true
		}
	}
	return false
}

func (h *Handlers) addSampleTube(tube, key string) {
	for k, v := range h.sampleJobs.Tubes {
		if v.Name == tube {
			h.sampleJobs.Tubes[k].Keys = append(h.sampleJobs.Tubes[k].Keys, key)
			return
		}
	}
	h.sampleJobs.Tubes = append(h.sampleJobs.Tubes, model.SampleTube{
		Name: tube,
		Keys: []string{key},
	})
}

func (h *Handlers) saveSample() error {
	data, err := json.Marshal(h.sampleJobs)
	if err != nil {
		return err
	}
	h.cfg.Sample.Storage = string(data)
	return config.Save(h.configPath, h.cfg)
}

func (h *Handlers) deleteSamples(key string) {
	if key == "" {
		return
	}
	h.sampleJobsMu.Lock()
	filtered := make([]model.SampleJob, 0, len(h.sampleJobs.Jobs))
	for _, j := range h.sampleJobs.Jobs {
		if j.Key != key {
			filtered = append(filtered, j)
		}
	}
	h.sampleJobs.Jobs = filtered
	for k, v := range h.sampleJobs.Tubes {
		fk := make([]string, 0, len(v.Keys))
		for _, t := range v.Keys {
			if t != key {
				fk = append(fk, t)
			}
		}
		h.sampleJobs.Tubes[k].Keys = fk
	}
	_ = h.saveSample()
	h.sampleJobsMu.Unlock()
}

func (h *Handlers) newSample(conf model.SelfConf, server string, f url.Values, w http.ResponseWriter, r *http.Request) {
	h.upsertSample(conf, server, "", f, w, r)
}

func (h *Handlers) editSample(conf model.SelfConf, server string, f url.Values, key string, w http.ResponseWriter, r *http.Request) {
	// Delete old sample inline (not via deleteSamples to avoid double lock).
	h.sampleJobsMu.Lock()
	filtered := make([]model.SampleJob, 0, len(h.sampleJobs.Jobs))
	for _, j := range h.sampleJobs.Jobs {
		if j.Key != key {
			filtered = append(filtered, j)
		}
	}
	h.sampleJobs.Jobs = filtered
	for k, v := range h.sampleJobs.Tubes {
		fk := make([]string, 0, len(v.Keys))
		for _, t := range v.Keys {
			if t != key {
				fk = append(fk, t)
			}
		}
		h.sampleJobs.Tubes[k].Keys = fk
	}
	_ = h.saveSample()
	h.sampleJobsMu.Unlock()

	h.upsertSample(conf, server, key, f, w, r)
}

func (h *Handlers) upsertSample(conf model.SelfConf, server, existingKey string, f url.Values, w http.ResponseWriter, r *http.Request) {
	key := existingKey
	if key == "" {
		key = randToken()
	}
	var name, body, ttr string
	var tubes []string
	for k, v := range f {
		if len(v) == 0 {
			continue
		}
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
			t := strings.TrimSuffix(strings.TrimPrefix(k, "tubes["), "]")
			tubes = append(tubes, t)
		}
	}
	if len(tubes) == 0 || name == "" || body == "" || ttr == "" {
		setFlash(w, "error", "Required fields are not set")
		http.Redirect(w, r, "/sample?action=newSample", http.StatusSeeOther)
		return
	}
	sampleTTR, err := strconv.Atoi(ttr)
	if err != nil {
		setFlash(w, "error", "TTR must be a number")
		http.Redirect(w, r, "/sample?action=newSample", http.StatusSeeOther)
		return
	}
	h.sampleJobsMu.Lock()
	if h.checkSampleJobName(name) {
		h.sampleJobsMu.Unlock()
		setFlash(w, "error", "Sample with this name already exists")
		http.Redirect(w, r, "/sample?action=newSample", http.StatusSeeOther)
		return
	}
	for _, t := range tubes {
		h.addSampleTube(t, key)
	}
	h.sampleJobs.Jobs = append(h.sampleJobs.Jobs, model.SampleJob{
		Key:   key,
		Name:  name,
		Tubes: tubes,
		Data:  body,
		TTR:   sampleTTR,
	})
	err = h.saveSample()
	h.sampleJobsMu.Unlock()
	if err != nil {
		setFlash(w, "error", "Failed to save sample")
		http.Redirect(w, r, "/sample?action=newSample", http.StatusSeeOther)
		return
	}
	setFlash(w, "success", "Sample saved")
	http.Redirect(w, r, "/sample?action=manageSamples", http.StatusSeeOther)
}

func loadSample(server, tube, key string, h *Handlers) {
	job := h.findSampleJobLocked(key)
	if job == nil || job.Data == "" {
		return
	}
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = newTube(conn, tube).Put(
		[]byte(job.Data), model.DefaultPriority,
		time.Duration(model.DefaultDelay)*time.Second,
		time.Duration(job.TTR)*time.Second,
	)
}

// --- Statistics ---

func (h *Handlers) statisticPreferenceSave(conf model.SelfConf, f url.Values, w http.ResponseWriter, r *http.Request) {
	var collection, frequency string
	var tubes []string
	for k, v := range f {
		if len(v) == 0 {
			continue
		}
		switch k {
		case "frequency":
			frequency = v[0]
		case "collection":
			collection = v[0]
		case "action":
			continue
		default:
			t := strings.TrimSuffix(strings.TrimPrefix(k, "tubes["), "]")
			tubes = append(tubes, t)
		}
	}
	if len(tubes) == 0 || collection == "" || frequency == "" {
		setFlash(w, "error", "Required fields are not set")
		http.Redirect(w, r, "/statistics?action=preference", http.StatusSeeOther)
		return
	}
	if err := h.saveStatisticsConfig(collection, frequency, tubes); err != nil {
		setFlash(w, "error", "Save statistics preference error")
		http.Redirect(w, r, "/statistics?action=preference", http.StatusSeeOther)
		return
	}
	setFlash(w, "success", "Statistics preference saved")
	http.Redirect(w, r, "/statistics?action=preference", http.StatusSeeOther)
}

func (h *Handlers) saveStatisticsConfig(collection, frequency string, tubes []string) error {
	c, err := strconv.Atoi(collection)
	if err != nil {
		return err
	}
	f, err := strconv.Atoi(frequency)
	if err != nil {
		return err
	}
	c = max(c, 0)
	f = max(f, 1)

	h.statsConfigMu.Lock()
	h.statsConfig.Collection = c
	h.statsConfig.Frequency = f
	h.statsConfigMu.Unlock()

	newServer := make(map[string]map[string]map[string]*list.List)
	for _, v := range tubes {
		addr := strings.Split(v, ":")
		if len(addr) != 3 {
			continue
		}
		serverKey := addr[0] + ":" + addr[1]
		tubeName := addr[2]
		if newServer[serverKey] == nil {
			newServer[serverKey] = make(map[string]map[string]*list.List)
		}
		newServer[serverKey][tubeName] = make(map[string]*list.List)
	}
	h.statsData.Lock()
	h.statsData.Server = newServer
	h.statsData.Unlock()

	select {
	case h.notify <- true:
	default:
	}
	return nil
}

func (h *Handlers) buildStatsPrefData(conf model.SelfConf) *pageData {
	h.statsConfigMu.RLock()
	frequency := h.statsConfig.Frequency
	collection := h.statsConfig.Collection
	h.statsConfigMu.RUnlock()
	if frequency < 1 {
		frequency = 300
	}

	statsTubes := make(map[string]map[string]bool)
	for _, server := range conf.Servers {
		conn, err := dialBeanstalk(server)
		if err != nil {
			continue
		}
		tubes, _ := conn.ListTubes()
		sort.Strings(tubes)
		conn.Close()

		tubeMap := make(map[string]bool)
		for _, t := range tubes {
			h.statsData.RLock()
			checked := false
			if s, ok := h.statsData.Server[server]; ok {
				_, checked = s[t]
			}
			h.statsData.RUnlock()
			tubeMap[t] = checked
		}
		statsTubes[server] = tubeMap
	}

	return &pageData{
		StatsFrequency:  frequency,
		StatsCollection: collection,
		StatsTubes:      statsTubes,
	}
}

// StatisticsCollector runs the background statistics collection loop.
func (h *Handlers) StatisticsCollector(ctx context.Context) {
	freq := h.getStatsFrequency()
	ticker := time.NewTicker(time.Duration(freq) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.notify:
			ticker.Stop()
			freq = h.getStatsFrequency()
			ticker = time.NewTicker(time.Duration(freq) * time.Second)
		case <-ticker.C:
			h.statsConfigMu.RLock()
			collection := h.statsConfig.Collection
			h.statsConfigMu.RUnlock()
			if collection == 0 {
				continue
			}

			h.statsData.RLock()
			serversCopy := make(map[string][]string)
			for k, v := range h.statsData.Server {
				for t := range v {
					serversCopy[k] = append(serversCopy[k], t)
				}
			}
			h.statsData.RUnlock()

			for k, tubes := range serversCopy {
				for _, t := range tubes {
					h.collectTubeStats(k, t, collection)
				}
			}
		}
	}
}

func (h *Handlers) getStatsFrequency() int {
	h.statsConfigMu.RLock()
	f := h.statsConfig.Frequency
	h.statsConfigMu.RUnlock()
	return max(f, 1)
}

func (h *Handlers) collectTubeStats(server, tube string, collection int) {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return
	}
	defer conn.Close()

	statsMap, err := newTube(conn, tube).Stats()
	if err != nil {
		return
	}
	now := time.Now()
	for _, field := range model.StatisticsFields {
		for k, v := range field {
			val, err := strconv.Atoi(statsMap[v])
			if err != nil {
				continue
			}
			h.statsData.Lock()
			srvMap := h.statsData.Server[server]
			if srvMap == nil {
				h.statsData.Unlock()
				continue
			}
			tubeMap := srvMap[tube]
			if tubeMap == nil {
				h.statsData.Unlock()
				continue
			}
			if tubeMap[k] == nil {
				tubeMap[k] = list.New()
			}
			if tubeMap[k].Len() >= collection {
				tubeMap[k].Remove(tubeMap[k].Back())
			}
			tubeMap[k].PushFront([]int{
				now.Year(), int(now.Month()), now.Day(),
				now.Hour(), now.Minute(), now.Second(), val,
			})
			h.statsData.Unlock()
		}
	}
}

func (h *Handlers) statisticsJSON(server, tube string) string {
	result := make(map[string][][]int)
	h.statsData.RLock()
	srvMap := h.statsData.Server[server]
	if srvMap != nil {
		tubeMap := srvMap[tube]
		for _, field := range model.StatisticsFields {
			for k := range field {
				if tubeMap == nil {
					result[k] = [][]int{}
					continue
				}
				l, ok := tubeMap[k]
				if !ok {
					result[k] = [][]int{}
					continue
				}
				var series [][]int
				for e := l.Front(); e != nil; e = e.Next() {
					if v, ok := e.Value.([]int); ok {
						series = append(series, v)
					}
				}
				result[k] = series
			}
		}
	}
	h.statsData.RUnlock()

	data, err := json.Marshal(result)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// --- Update check ---

func (h *Handlers) checkUpdate() string {
	h.updateOnce.Do(func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(config.UpdateURL)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		var tags model.UpdateTags
		if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
			return
		}
		if len(tags) > 0 {
			versionStr := fmt.Sprintf("v%.1f", config.Version)
			if tags[0].Name != versionStr {
				h.updateInfo = fmt.Sprintf("Aurora %s is available", tags[0].Name)
			}
		}
	})
	return h.updateInfo
}

// --- Utilities ---

func randToken() string {
	b := make([]byte, 16)
	_, _ = cryptoRand.Read(b)
	return fmt.Sprintf("%x", b)
}
