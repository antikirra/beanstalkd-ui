package api

import (
	"container/list"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/antikirra/beanstalkd-ui/internal/config"
	"github.com/antikirra/beanstalkd-ui/internal/model"
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
	notify    chan struct{}
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
			Server: make(map[string]map[string]map[string]*list.List),
		},
		notify: make(chan struct{}, 1),
	}, nil
}

// --- Cookie helpers ---

func readListCookie(r *http.Request, name, sep string, defaults []string) []string {
	if c := cookieValue(r, name); c != "" {
		return compactUnique(strings.Split(c, sep))
	}
	return defaults
}

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

	conf.Filter = readListCookie(r, "filter", ",", []string{
		"current-connections", "current-jobs-buried", "current-jobs-delayed",
		"current-jobs-ready", "current-jobs-reserved", "current-jobs-urgent", "current-tubes",
	})
	conf.TubeFilters = readListCookie(r, "tubefilter", ",", []string{
		"current-jobs-urgent", "current-jobs-ready", "current-jobs-reserved",
		"current-jobs-delayed", "current-jobs-buried", "total-jobs",
	})

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
	s := rawCookieValue(r, name)
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return defaultValue
}

func readBoolCookie(r *http.Request, name string) bool {
	return rawCookieValue(r, name) == "1"
}

func removeServerInCookie(conf model.SelfConf, server string, w http.ResponseWriter) {
	remaining := slices.DeleteFunc(slices.Clone(conf.Servers), func(s string) bool {
		return s == server
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "beansServers",
		Value:    url.QueryEscape(strings.Join(remaining, ";")),
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
		ServerStats:        h.serverStats(conf),
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
		ServerStats: h.serverStats(conf),
		Filter:      conf.Filter,
		Conf:        conf,
	})
}

func (h *Handlers) handleServerRemove(w http.ResponseWriter, r *http.Request) {
	conf := readCookies(r, h.cfg)
	server := r.URL.Query().Get("server")
	removeServerInCookie(conf, server, w)
	h.cfg.RemoveServer(server)
	_ = config.Save(h.configPath, h.cfg)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) handleServer(w http.ResponseWriter, r *http.Request) {
	conf := readCookies(r, h.cfg)
	q := r.URL.Query()
	server := q.Get("server")

	switch q.Get("action") {
	case "reloader":
		h.renderFragment(w, r, "tube_table_inner", &pageData{
			TubeStats:     h.tubeStats(server),
			TubeFilters:   conf.TubeFilters,
			CurrentServer: server,
			Conf:          conf,
		})
	case "clearTubes":
		_ = r.ParseForm()
		clearTubes(r.Context(), server, r.Form)
		hxToast(w, "success", "Tubes cleared", false)
		w.WriteHeader(http.StatusNoContent)
	default:
		h.render(w, r, "server.html", &pageData{
			PageTitle:     server,
			CurrentServer: server,
			TubeStats:     h.tubeStats(server),
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

	switch q.Get("action") {
	case "addjob":
		addJob(server,
			r.PostFormValue("tubeName"), r.PostFormValue("tubeData"),
			r.PostFormValue("tubePriority"), r.PostFormValue("tubeDelay"), r.PostFormValue("tubeTtr"))
		hxToast(w, "success", "Job added", true)
		w.WriteHeader(http.StatusNoContent)
	case "search":
		searchLimit, _ := strconv.Atoi(q.Get("limit"))
		searchResults := searchTube(server, tube, searchLimit, q.Get("searchStr"))
		h.render(w, r, "tube.html", h.buildTubeData(conf, server, tube, searchResults, q.Get("searchStr"), q.Get("limit")))
	case "addSample":
		_ = r.ParseForm()
		h.addSampleFromJob(server, r.Form, w)
	case "kick":
		if !requirePOST(w, r) {
			return
		}
		kick(server, tube, r.FormValue("count"))
		setFlash(w, "success", "Jobs kicked")
		h.redirectToTube(w, r, server, tube)
	case "kickJob":
		if !requirePOST(w, r) {
			return
		}
		kickJob(server, q.Get("jobid"))
		setFlash(w, "success", "Job kicked")
		h.redirectToTube(w, r, server, tube)
	case "pause":
		if !requirePOST(w, r) {
			return
		}
		pause(server, tube, q.Get("count"), conf.TubePauseSeconds)
		setFlash(w, "success", "Tube pause updated")
		h.redirectToTube(w, r, server, tube)
	case "moveJobsTo":
		if !requirePOST(w, r) {
			return
		}
		destTube := q.Get("destTube")
		if destTube == "" {
			destTube = tube
		}
		moveJobsTo(r.Context(), server, tube, destTube, q.Get("state"), q.Get("destState"))
		setFlash(w, "success", "Jobs moved")
		h.redirectToTube(w, r, server, destTube)
	case "deleteAll":
		if !requirePOST(w, r) {
			return
		}
		deleteAll(r.Context(), server, tube, q.Get("state"))
		setFlash(w, "success", "All jobs deleted")
		h.redirectToTube(w, r, server, tube)
	case "deleteJob":
		if !requirePOST(w, r) {
			return
		}
		deleteJob(server, q.Get("jobid"))
		setFlash(w, "success", "Job deleted")
		h.redirectToTube(w, r, server, tube)
	case "loadSample":
		if !requirePOST(w, r) {
			return
		}
		h.loadSample(server, tube, q.Get("key"))
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
	slices.Sort(data.Tubes)

	if data.TubeInfo != nil {
		data.PauseSeconds = data.TubeInfo["pause-time-left"]
	}

	// Peek jobs for showcase.
	for _, p := range []struct {
		peek   func() (uint64, []byte, error)
		target **jobData
	}{
		{t.PeekReady, &data.ReadyJob},
		{t.PeekDelayed, &data.DelayedJob},
		{t.PeekBuried, &data.BuriedJob},
	} {
		id, body, err := p.peek()
		if err != nil || body == nil {
			continue
		}
		stats, err := conn.StatsJob(id)
		if err != nil {
			continue
		}
		*p.target = &jobData{ID: id, Data: string(body), Stats: stats}
	}

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
			ServerTubes:   h.serverTubesMap(conf),
		})
	case "editSample":
		key := q.Get("key")
		job := h.findSampleByKey(key)
		title := "Edit Sample"
		if job != nil {
			title = "Edit: " + job.Name
		}
		h.render(w, r, "sample_edit.html", &pageData{
			PageTitle:     title,
			CurrentServer: server,
			SampleJob:     job,
			Servers:       conf.Servers,
			ServerTubes:   h.serverTubesMap(conf),
		})
	case "actionNewSample":
		_ = r.ParseForm()
		h.upsertSample("", r.Form, w, r)
	case "actionEditSample":
		_ = r.ParseForm()
		h.editSample(r.Form, q.Get("key"), w, r)
	case "deleteSample":
		if !requirePOST(w, r) {
			return
		}
		h.deleteSamples(q.Get("key"))
		flashRedirect(w, r, "success", "Sample deleted", "/sample?action=manageSamples")
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
		h.statisticPreferenceSave(r.Form, w, r)
	case "reloader":
		h.renderFragment(w, r, "stats_table", h.statisticsRows(server, tube))
	default:
		h.render(w, r, "statistics.html", &pageData{
			PageTitle:   "Statistics",
			StatsServer: server,
			StatsTube:   tube,
		})
	}
}

// --- Data fetching helpers ---

func (h *Handlers) serverStats(conf model.SelfConf) []serverStat {
	stats := make([]serverStat, 0, len(conf.Servers))
	for _, addr := range conf.Servers {
		stats = append(stats, fetchServerStat(addr))
	}
	return stats
}

func fetchServerStat(addr string) serverStat {
	ss := serverStat{Addr: addr}
	conn, err := dialBeanstalk(addr)
	if err != nil {
		return ss
	}
	defer conn.Close()
	ss.Online = true
	ss.Stats, _ = conn.Stats()
	return ss
}

func (h *Handlers) tubeStats(server string) []tubeStat {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return nil
	}
	defer conn.Close()

	tubes, _ := conn.ListTubes()
	slices.Sort(tubes)

	stats := make([]tubeStat, 0, len(tubes))
	for _, name := range tubes {
		ts := tubeStat{Name: name}
		t := newTube(conn, name)
		ts.Stats, _ = t.Stats()
		stats = append(stats, ts)
	}
	return stats
}

func (h *Handlers) serverTubesMap(conf model.SelfConf) map[string][]string {
	result := make(map[string][]string, len(conf.Servers))
	for _, server := range conf.Servers {
		result[server] = listTubesSorted(server)
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

// --- Sample CRUD ---

func (h *Handlers) findSampleByKey(key string) *model.SampleJob {
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

func (h *Handlers) addSampleFromJob(server string, data url.Values, w http.ResponseWriter) {
	sampleName := data.Get("addsamplename")
	if sampleName == "" {
		hxError(w, "Sample name required")
		return
	}

	h.sampleJobsMu.RLock()
	nameExists := h.sampleNameExists(sampleName)
	h.sampleJobsMu.RUnlock()
	if nameExists {
		hxError(w, "Sample with this name already exists")
		return
	}

	rawID := data.Get("addsamplejobid")
	jobID, err := strconv.Atoi(rawID)
	if err != nil {
		hxError(w, "Invalid job ID")
		return
	}

	conn, err := dialBeanstalk(server)
	if err != nil {
		hxError(w, "Connect to beanstalkd failed")
		return
	}
	defer conn.Close()

	body, err := conn.Peek(uint64(jobID))
	if err != nil {
		hxError(w, "Read job content failed")
		return
	}
	jobStats, err := conn.StatsJob(uint64(jobID))
	if err != nil {
		hxError(w, "Read job stats failed")
		return
	}
	sampleTTR := model.DefaultTTR
	if ttr, err := strconv.Atoi(jobStats["ttr"]); err == nil {
		sampleTTR = ttr
	}

	key := randToken()
	var tubes []string
	for k := range data {
		if strings.HasPrefix(k, "tubes[") {
			tubes = append(tubes, parseTubeName(k))
		}
	}
	h.sampleJobsMu.Lock()
	for _, t := range tubes {
		h.addSampleTube(t, key)
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
		hxError(w, "Failed to save sample")
		return
	}
	hxToast(w, "success", "Sample saved", true)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) sampleNameExists(name string) bool {
	return slices.ContainsFunc(h.sampleJobs.Jobs, func(j model.SampleJob) bool {
		return j.Name == name
	})
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

// removeSampleByKey removes a sample job from both Jobs and Tubes lists.
// Caller must hold sampleJobsMu.
func (h *Handlers) removeSampleByKey(key string) {
	h.sampleJobs.Jobs = slices.DeleteFunc(h.sampleJobs.Jobs, func(j model.SampleJob) bool {
		return j.Key == key
	})
	for k, v := range h.sampleJobs.Tubes {
		h.sampleJobs.Tubes[k].Keys = slices.DeleteFunc(v.Keys, func(s string) bool {
			return s == key
		})
	}
}

func (h *Handlers) deleteSamples(key string) {
	if key == "" {
		return
	}
	h.sampleJobsMu.Lock()
	h.removeSampleByKey(key)
	_ = h.saveSample()
	h.sampleJobsMu.Unlock()
}

func (h *Handlers) editSample(f url.Values, key string, w http.ResponseWriter, r *http.Request) {
	h.sampleJobsMu.Lock()
	h.removeSampleByKey(key)
	_ = h.saveSample()
	h.sampleJobsMu.Unlock()

	h.upsertSample(key, f, w, r)
}

func (h *Handlers) upsertSample(existingKey string, f url.Values, w http.ResponseWriter, r *http.Request) {
	key := existingKey
	if key == "" {
		key = randToken()
	}
	name := f.Get("name")
	body := f.Get("jobdata")
	ttr := f.Get("ttr")
	var tubes []string
	for k := range f {
		if strings.HasPrefix(k, "tubes[") {
			tubes = append(tubes, parseTubeName(k))
		}
	}
	if len(tubes) == 0 || name == "" || body == "" || ttr == "" {
		flashRedirect(w, r, "error", "Required fields are not set", "/sample?action=newSample")
		return
	}
	sampleTTR, err := strconv.Atoi(ttr)
	if err != nil {
		flashRedirect(w, r, "error", "TTR must be a number", "/sample?action=newSample")
		return
	}
	h.sampleJobsMu.Lock()
	if h.sampleNameExists(name) {
		h.sampleJobsMu.Unlock()
		flashRedirect(w, r, "error", "Sample with this name already exists", "/sample?action=newSample")
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
		flashRedirect(w, r, "error", "Failed to save sample", "/sample?action=newSample")
		return
	}
	flashRedirect(w, r, "success", "Sample saved", "/sample?action=manageSamples")
}

func (h *Handlers) loadSample(server, tube, key string) {
	job := h.findSampleByKey(key)
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

func (h *Handlers) statisticPreferenceSave(f url.Values, w http.ResponseWriter, r *http.Request) {
	collection := f.Get("collection")
	frequency := f.Get("frequency")
	var tubes []string
	for k := range f {
		if strings.HasPrefix(k, "tubes[") {
			tubes = append(tubes, parseTubeName(k))
		}
	}
	const statsPrefURL = "/statistics?action=preference"
	if len(tubes) == 0 || collection == "" || frequency == "" {
		flashRedirect(w, r, "error", "Required fields are not set", statsPrefURL)
		return
	}
	if err := h.saveStatisticsConfig(collection, frequency, tubes); err != nil {
		flashRedirect(w, r, "error", "Save statistics preference error", statsPrefURL)
		return
	}
	flashRedirect(w, r, "success", "Statistics preference saved", statsPrefURL)
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
		i := strings.LastIndex(v, ":")
		if i < 0 {
			continue
		}
		serverKey := v[:i]
		tubeName := v[i+1:]
		if newServer[serverKey] == nil {
			newServer[serverKey] = make(map[string]map[string]*list.List)
		}
		newServer[serverKey][tubeName] = make(map[string]*list.List)
	}
	h.statsData.Lock()
	h.statsData.Server = newServer
	h.statsData.Unlock()

	select {
	case h.notify <- struct{}{}:
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

	// Collect tube lists from all servers.
	serverTubes := make(map[string][]string, len(conf.Servers))
	for _, server := range conf.Servers {
		serverTubes[server] = listTubesSorted(server)
	}

	// Single lock to check which tubes are being monitored.
	h.statsData.RLock()
	statsTubes := make(map[string]map[string]bool, len(serverTubes))
	for server, tubes := range serverTubes {
		tubeMap := make(map[string]bool, len(tubes))
		for _, t := range tubes {
			checked := false
			if s, ok := h.statsData.Server[server]; ok {
				_, checked = s[t]
			}
			tubeMap[t] = checked
		}
		statsTubes[server] = tubeMap
	}
	h.statsData.RUnlock()

	return &pageData{
		StatsFrequency:  frequency,
		StatsCollection: collection,
		StatsTubes:      statsTubes,
	}
}

// StatisticsCollector runs the background statistics collection loop.
func (h *Handlers) StatisticsCollector(ctx context.Context) {
	freq := h.statsFrequency()
	ticker := time.NewTicker(time.Duration(freq) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.notify:
			ticker.Stop()
			freq = h.statsFrequency()
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

func (h *Handlers) statsFrequency() int {
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
	values := make(map[string]int)
	for _, f := range model.StatisticsFields {
		val, err := strconv.Atoi(statsMap[f.Stat])
		if err != nil {
			continue
		}
		values[f.Key] = val
	}
	if len(values) == 0 {
		return
	}

	h.statsData.Lock()
	defer h.statsData.Unlock()
	srvMap := h.statsData.Server[server]
	if srvMap == nil {
		return
	}
	tubeMap := srvMap[tube]
	if tubeMap == nil {
		return
	}
	ts := []int{
		now.Year(), int(now.Month()), now.Day(),
		now.Hour(), now.Minute(), now.Second(),
	}
	for k, val := range values {
		if tubeMap[k] == nil {
			tubeMap[k] = list.New()
		}
		if tubeMap[k].Len() >= collection {
			tubeMap[k].Remove(tubeMap[k].Back())
		}
		tubeMap[k].PushFront(append(ts, val))
	}
}

type statsRow struct {
	Key    string
	Count  int
	Latest string
}

func (h *Handlers) statisticsRows(server, tube string) []statsRow {
	h.statsData.RLock()
	defer h.statsData.RUnlock()

	var tubeMap map[string]*list.List
	if srvMap := h.statsData.Server[server]; srvMap != nil {
		tubeMap = srvMap[tube]
	}

	rows := make([]statsRow, 0, len(model.StatisticsFields))
	for _, f := range model.StatisticsFields {
		row := statsRow{Key: f.Key}
		if l := tubeMap[f.Key]; l != nil {
			row.Count = l.Len()
			if front := l.Front(); front != nil {
				if v, ok := front.Value.([]int); ok && len(v) > 6 {
					row.Latest = strconv.Itoa(v[6])
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// --- Utilities ---

func flashRedirect(w http.ResponseWriter, r *http.Request, typ, message, target string) {
	setFlash(w, typ, message)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// hxToast sets HX-Trigger header to show a toast notification on the client.
// If closeModal is true, a closeModal event is also triggered.
func hxToast(w http.ResponseWriter, typ, message string, closeModal bool) {
	events := map[string]any{
		"showToast": map[string]string{"type": typ, "message": message},
	}
	if closeModal {
		events["closeModal"] = ""
	}
	data, _ := json.Marshal(events)
	w.Header().Set("HX-Trigger", string(data))
}

// hxError sends a 422 response with an error toast.
func hxError(w http.ResponseWriter, message string) {
	hxToast(w, "error", message, false)
	w.WriteHeader(http.StatusUnprocessableEntity)
}

func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// parseTubeName extracts tube name from form key "tubes[name]" → "name".
func parseTubeName(key string) string {
	return strings.TrimSuffix(strings.TrimPrefix(key, "tubes["), "]")
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
