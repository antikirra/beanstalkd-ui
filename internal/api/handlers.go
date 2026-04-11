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

	"github.com/beanstalkd/go-beanstalk"

	"github.com/antikirra/beanstalkd-ui/internal/config"
	"github.com/antikirra/beanstalkd-ui/internal/model"
	"github.com/antikirra/beanstalkd-ui/internal/pool"
)

// Handlers holds all dependencies and mutable state for HTTP handlers.
type Handlers struct {
	log        *slog.Logger
	tmpl       *templateSet
	cfg        *config.Config
	cfgMu      sync.RWMutex
	configPath string
	pool       *pool.Manager

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
	p := pool.New(pool.Config{})
	return &Handlers{
		log:        log,
		tmpl:       tmpl,
		cfg:        cfg,
		configPath: configPath,
		pool:       p,
		sampleJobs: samples,
		statsData: model.StatisticsData{
			Server: make(map[string]map[string]map[string]*list.List),
		},
		notify: make(chan struct{}, 1),
	}, nil
}

// Close shuts down the connection pool, closing all idle connections.
func (h *Handlers) Close() {
	h.pool.Close()
}

func (h *Handlers) readConf(r *http.Request) model.SelfConf {
	h.cfgMu.RLock()
	conf := readCookies(r, h.cfg)
	h.cfgMu.RUnlock()
	return conf
}

func (h *Handlers) saveCfg() error {
	return config.Save(h.configPath, h.cfg)
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
	conf := h.readConf(r)
	h.render(w, r, "servers.html", &pageData{
		PageTitle:   "Servers",
		ServerStats: h.serverStats(r.Context(), conf),
		Filter:      conf.Filter,
	})
}

func (h *Handlers) handleSettings(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "settings.html", &pageData{PageTitle: "Settings"})
}

func (h *Handlers) handleServersReload(w http.ResponseWriter, r *http.Request) {
	conf := h.readConf(r)
	h.renderFragment(w, "server_table_inner", &pageData{
		ServerStats: h.serverStats(r.Context(), conf),
		Filter:      conf.Filter,
		Conf:        conf,
	})
}

func (h *Handlers) handleServerRemove(w http.ResponseWriter, r *http.Request) {
	conf := h.readConf(r)
	server := r.URL.Query().Get("server")
	removeServerInCookie(conf, server, w)

	h.cfgMu.Lock()
	h.cfg.RemoveServer(server)
	_ = h.saveCfg()
	h.cfgMu.Unlock()

	h.pool.RemoveServer(server)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) handleServer(w http.ResponseWriter, r *http.Request) {
	conf := h.readConf(r)
	q := r.URL.Query()
	server := q.Get("server")

	ctx := r.Context()
	switch q.Get("action") {
	case "reloader":
		h.renderFragment(w, "tube_table_inner", &pageData{
			TubeStats:     h.tubeStats(ctx, server),
			TubeFilters:   conf.TubeFilters,
			CurrentServer: server,
			Conf:          conf,
		})
	case "clearTubes":
		_ = r.ParseForm()
		h.clearTubes(ctx, server, r.Form)
		hxToast(w, "success", "Tubes cleared", false)
		w.WriteHeader(http.StatusNoContent)
	default:
		ts := h.tubeStats(ctx, server)
		tubes := make([]string, len(ts))
		for i, t := range ts {
			tubes[i] = t.Name
		}
		h.render(w, r, "server.html", &pageData{
			PageTitle:     server,
			CurrentServer: server,
			TubeStats:     ts,
			TubeFilters:   conf.TubeFilters,
			Tubes:         tubes,
		})
	}
}

func (h *Handlers) handleTube(w http.ResponseWriter, r *http.Request) {
	conf := h.readConf(r)
	q := r.URL.Query()
	server := q.Get("server")
	tube := q.Get("tube")

	ctx := r.Context()
	switch q.Get("action") {
	case "addjob":
		h.addJob(ctx, server,
			r.PostFormValue("tubeName"), r.PostFormValue("tubeData"),
			r.PostFormValue("tubePriority"), r.PostFormValue("tubeDelay"), r.PostFormValue("tubeTtr"))
		hxToast(w, "success", "Job added", true)
		w.WriteHeader(http.StatusNoContent)
	case "search":
		searchLimit, _ := strconv.Atoi(q.Get("limit"))
		searchResults := h.searchTube(ctx, server, tube, searchLimit, q.Get("searchStr"))
		h.render(w, r, "tube.html", h.buildTubeData(ctx, conf, server, tube, searchResults, q.Get("searchStr"), q.Get("limit")))
	case "addSample":
		_ = r.ParseForm()
		h.addSampleFromJob(ctx, server, r.Form, w)
	case "kick":
		if !requirePOST(w, r) {
			return
		}
		h.kick(ctx, server, tube, r.FormValue("count"))
		setFlash(w, "success", "Jobs kicked")
		h.redirectToTube(w, r, server, tube)
	case "kickJob":
		if !requirePOST(w, r) {
			return
		}
		h.kickJob(ctx, server, q.Get("jobid"))
		setFlash(w, "success", "Job kicked")
		h.redirectToTube(w, r, server, tube)
	case "pause":
		if !requirePOST(w, r) {
			return
		}
		h.pause(ctx, server, tube, q.Get("count"), conf.TubePauseSeconds)
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
		h.moveJobsTo(ctx, server, tube, destTube, q.Get("state"), q.Get("destState"))
		setFlash(w, "success", "Jobs moved")
		h.redirectToTube(w, r, server, destTube)
	case "deleteAll":
		if !requirePOST(w, r) {
			return
		}
		h.deleteAll(ctx, server, tube, q.Get("state"))
		setFlash(w, "success", "All jobs deleted")
		h.redirectToTube(w, r, server, tube)
	case "deleteJob":
		if !requirePOST(w, r) {
			return
		}
		h.deleteJob(ctx, server, q.Get("jobid"))
		setFlash(w, "success", "Job deleted")
		h.redirectToTube(w, r, server, tube)
	case "loadSample":
		if !requirePOST(w, r) {
			return
		}
		h.loadSample(ctx, server, tube, q.Get("key"))
		setFlash(w, "success", "Sample loaded")
		h.redirectToTube(w, r, server, tube)
	case "reloader":
		td := h.buildTubeData(ctx, conf, server, tube, nil, "", "")
		td.Conf = conf
		h.renderFragment(w, "tube_content_inner", td)
	default:
		h.render(w, r, "tube.html", h.buildTubeData(ctx, conf, server, tube, nil, "", ""))
	}
}

func (h *Handlers) redirectToTube(w http.ResponseWriter, r *http.Request, server, tube string) {
	target := fmt.Sprintf("/tube?server=%s&tube=%s", url.QueryEscape(server), url.QueryEscape(tube))
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *Handlers) buildTubeData(ctx context.Context, conf model.SelfConf, server, tube string, searchResults []model.SearchResult, searchStr, searchLimit string) *pageData {
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

	_ = h.pool.WithConn(ctx, server, pool.ReadPool, func(conn *beanstalk.Conn) error {
		t := newTube(conn, tube)

		info, err := t.Stats()
		if err != nil && pool.IsConnError(err) {
			return err
		}
		data.TubeInfo = info

		data.Tubes, err = conn.ListTubes()
		if err != nil && pool.IsConnError(err) {
			return err
		}
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
			if err != nil {
				if pool.IsConnError(err) {
					return err
				}
				continue
			}
			if body == nil {
				continue
			}
			stats, err := conn.StatsJob(id)
			if err != nil {
				if pool.IsConnError(err) {
					return err
				}
				continue
			}
			*p.target = &jobData{ID: id, Data: string(body), Stats: stats}
		}
		return nil
	})

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
	conf := h.readConf(r)
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
			ServerTubes:   h.serverTubesMap(r.Context(), conf),
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
			ServerTubes:   h.serverTubesMap(r.Context(), conf),
		})
	case "actionNewSample":
		_ = r.ParseForm()
		h.upsertSample("", r.Form, w, r)
	case "actionEditSample":
		_ = r.ParseForm()
		h.upsertSample(q.Get("key"), r.Form, w, r)
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
	conf := h.readConf(r)
	q := r.URL.Query()
	server := q.Get("server")
	tube := q.Get("tube")

	switch q.Get("action") {
	case "preference":
		data := h.buildStatsPrefData(r.Context(), conf)
		data.PageTitle = "Statistics Preference"
		h.render(w, r, "statistics_pref.html", data)
	case "save":
		_ = r.ParseForm()
		h.statisticPreferenceSave(r.Form, w, r)
	case "reloader":
		h.renderFragment(w, "stats_table", h.statisticsRows(server, tube))
	default:
		h.render(w, r, "statistics.html", &pageData{
			PageTitle:   "Statistics",
			StatsServer: server,
			StatsTube:   tube,
		})
	}
}

// --- Data fetching helpers ---

func (h *Handlers) serverStats(ctx context.Context, conf model.SelfConf) []serverStat {
	stats := make([]serverStat, len(conf.Servers))
	var wg sync.WaitGroup
	for i, addr := range conf.Servers {
		wg.Add(1)
		go func(i int, addr string) {
			defer wg.Done()
			stats[i] = h.fetchServerStat(ctx, addr)
		}(i, addr)
	}
	wg.Wait()
	return stats
}

func (h *Handlers) fetchServerStat(ctx context.Context, addr string) serverStat {
	ss := serverStat{Addr: addr}
	_ = h.pool.WithConn(ctx, addr, pool.ReadPool, func(conn *beanstalk.Conn) error {
		ss.Online = true
		var err error
		ss.Stats, err = conn.Stats()
		return err
	})
	return ss
}

func (h *Handlers) tubeStats(ctx context.Context, server string) []tubeStat {
	var stats []tubeStat
	_ = h.pool.WithConn(ctx, server, pool.ReadPool, func(conn *beanstalk.Conn) error {
		tubes, err := conn.ListTubes()
		if err != nil {
			return err
		}
		slices.Sort(tubes)

		stats = make([]tubeStat, 0, len(tubes))
		for _, name := range tubes {
			ts := tubeStat{Name: name}
			t := newTube(conn, name)
			s, err := t.Stats()
			if err != nil && pool.IsConnError(err) {
				return err
			}
			ts.Stats = s
			stats = append(stats, ts)
		}
		return nil
	})
	return stats
}

func (h *Handlers) serverTubesMap(ctx context.Context, conf model.SelfConf) map[string][]string {
	result := make(map[string][]string, len(conf.Servers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, server := range conf.Servers {
		wg.Add(1)
		go func(server string) {
			defer wg.Done()
			tubes := h.listTubesSorted(ctx, server)
			mu.Lock()
			result[server] = tubes
			mu.Unlock()
		}(server)
	}
	wg.Wait()
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

func (h *Handlers) addSampleFromJob(ctx context.Context, server string, data url.Values, w http.ResponseWriter) {
	sampleName := data.Get("addsamplename")
	if sampleName == "" {
		hxError(w, "Sample name required")
		return
	}

	rawID := data.Get("addsamplejobid")
	jobID, err := strconv.Atoi(rawID)
	if err != nil {
		hxError(w, "Invalid job ID")
		return
	}

	var body []byte
	sampleTTR := model.DefaultTTR
	connErr := h.pool.WithConn(ctx, server, pool.ReadPool, func(conn *beanstalk.Conn) error {
		var err error
		body, err = conn.Peek(uint64(jobID))
		if err != nil {
			return fmt.Errorf("peek: %w", err)
		}
		jobStats, err := conn.StatsJob(uint64(jobID))
		if err != nil {
			return fmt.Errorf("stats: %w", err)
		}
		if ttr, err := strconv.Atoi(jobStats["ttr"]); err == nil {
			sampleTTR = ttr
		}
		return nil
	})
	if connErr != nil {
		hxError(w, "Read job data failed")
		return
	}

	job := model.SampleJob{
		Key:   randToken(),
		Name:  sampleName,
		Tubes: parseTubesFromForm(data),
		Data:  string(body),
		TTR:   sampleTTR,
	}
	if err := h.replaceSample("", job); err != nil {
		hxError(w, err.Error())
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

// saveSample persists sample jobs to config. Caller must hold sampleJobsMu.
func (h *Handlers) saveSample() error {
	data, err := json.Marshal(h.sampleJobs)
	if err != nil {
		return err
	}
	h.cfgMu.Lock()
	h.cfg.Sample.Storage = string(data)
	err = h.saveCfg()
	h.cfgMu.Unlock()
	return err
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

// replaceSample atomically removes an old sample (if removeKey is set) and inserts a new one.
// Caller must NOT hold sampleJobsMu.
func (h *Handlers) replaceSample(removeKey string, job model.SampleJob) error {
	h.sampleJobsMu.Lock()
	defer h.sampleJobsMu.Unlock()
	if removeKey != "" {
		h.removeSampleByKey(removeKey)
	}
	if h.sampleNameExists(job.Name) {
		return fmt.Errorf("Sample with this name already exists")
	}
	for _, t := range job.Tubes {
		h.addSampleTube(t, job.Key)
	}
	h.sampleJobs.Jobs = append(h.sampleJobs.Jobs, job)
	return h.saveSample()
}

func (h *Handlers) upsertSample(replaceKey string, f url.Values, w http.ResponseWriter, r *http.Request) {
	key := replaceKey
	if key == "" {
		key = randToken()
	}
	name := f.Get("name")
	body := f.Get("jobdata")
	ttr := f.Get("ttr")
	tubes := parseTubesFromForm(f)
	if len(tubes) == 0 || name == "" || body == "" || ttr == "" {
		flashRedirect(w, r, "error", "Required fields are not set", "/sample?action=newSample")
		return
	}
	sampleTTR, err := strconv.Atoi(ttr)
	if err != nil {
		flashRedirect(w, r, "error", "TTR must be a number", "/sample?action=newSample")
		return
	}
	if err := h.replaceSample(replaceKey, model.SampleJob{
		Key: key, Name: name, Tubes: tubes, Data: body, TTR: sampleTTR,
	}); err != nil {
		flashRedirect(w, r, "error", err.Error(), "/sample?action=newSample")
		return
	}
	flashRedirect(w, r, "success", "Sample saved", "/sample?action=manageSamples")
}

func (h *Handlers) loadSample(ctx context.Context, server, tube, key string) {
	job := h.findSampleByKey(key)
	if job == nil || job.Data == "" {
		return
	}
	_ = h.pool.WithConn(ctx, server, pool.WritePool, func(conn *beanstalk.Conn) error {
		_, err := newTube(conn, tube).Put(
			[]byte(job.Data), model.DefaultPriority,
			time.Duration(model.DefaultDelay)*time.Second,
			time.Duration(job.TTR)*time.Second,
		)
		return err
	})
}

// --- Statistics ---

func (h *Handlers) statisticPreferenceSave(f url.Values, w http.ResponseWriter, r *http.Request) {
	collection := f.Get("collection")
	frequency := f.Get("frequency")
	tubes := parseTubesFromForm(f)
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

func (h *Handlers) buildStatsPrefData(ctx context.Context, conf model.SelfConf) *pageData {
	h.statsConfigMu.RLock()
	frequency := h.statsConfig.Frequency
	collection := h.statsConfig.Collection
	h.statsConfigMu.RUnlock()
	if frequency < 1 {
		frequency = 300
	}

	serverTubes := h.serverTubesMap(ctx, conf)

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
			h.collectAllStats(ctx)
		}
	}
}

func (h *Handlers) collectAllStats(ctx context.Context) {
	h.statsConfigMu.RLock()
	collection := h.statsConfig.Collection
	h.statsConfigMu.RUnlock()
	if collection == 0 {
		return
	}

	h.statsData.RLock()
	serversCopy := make(map[string][]string)
	for k, v := range h.statsData.Server {
		for t := range v {
			serversCopy[k] = append(serversCopy[k], t)
		}
	}
	h.statsData.RUnlock()

	for server, tubes := range serversCopy {
		h.collectServerStats(ctx, server, tubes, collection)
	}
}

func (h *Handlers) statsFrequency() int {
	h.statsConfigMu.RLock()
	f := h.statsConfig.Frequency
	h.statsConfigMu.RUnlock()
	return max(f, 1)
}

func (h *Handlers) collectServerStats(ctx context.Context, server string, tubes []string, collection int) {
	type tubeData struct {
		tube   string
		values map[string]int
	}

	var collected []tubeData
	_ = h.pool.WithConn(ctx, server, pool.ReadPool, func(conn *beanstalk.Conn) error {
		for _, tube := range tubes {
			statsMap, err := newTube(conn, tube).Stats()
			if err != nil {
				if pool.IsConnError(err) {
					return err
				}
				continue
			}
			values := make(map[string]int)
			for _, f := range model.StatisticsFields {
				val, err := strconv.Atoi(statsMap[f.Stat])
				if err != nil {
					continue
				}
				values[f.Key] = val
			}
			if len(values) > 0 {
				collected = append(collected, tubeData{tube: tube, values: values})
			}
		}
		return nil
	})

	if len(collected) == 0 {
		return
	}

	now := time.Now()
	ts := []int{
		now.Year(), int(now.Month()), now.Day(),
		now.Hour(), now.Minute(), now.Second(),
	}

	h.statsData.Lock()
	defer h.statsData.Unlock()
	srvMap := h.statsData.Server[server]
	if srvMap == nil {
		return
	}
	for _, td := range collected {
		tubeMap := srvMap[td.tube]
		if tubeMap == nil {
			continue
		}
		for k, val := range td.values {
			if tubeMap[k] == nil {
				tubeMap[k] = list.New()
			}
			if tubeMap[k].Len() >= collection {
				tubeMap[k].Remove(tubeMap[k].Back())
			}
			tubeMap[k].PushFront(append(append([]int{}, ts...), val))
		}
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

// parseTubesFromForm extracts tube names from form keys like "tubes[name]".
func parseTubesFromForm(f url.Values) []string {
	var tubes []string
	for k := range f {
		if strings.HasPrefix(k, "tubes[") {
			tubes = append(tubes, strings.TrimSuffix(strings.TrimPrefix(k, "tubes["), "]"))
		}
	}
	return tubes
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
