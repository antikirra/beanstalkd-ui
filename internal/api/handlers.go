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

	"github.com/beanstalkd/go-beanstalk"

	"github.com/antikirra/beanstalkd-ui/internal/model"
	"github.com/antikirra/beanstalkd-ui/internal/pool"
	"github.com/antikirra/beanstalkd-ui/internal/store"
)

// Handlers holds all dependencies and mutable state for HTTP handlers.
type Handlers struct {
	log   *slog.Logger
	tmpl  *templateSet
	store *store.Store
	pool  *pool.Manager

	sampleJobs   model.SampleJobs
	sampleJobsMu sync.RWMutex

	statsConfig   model.StatsConfig
	statsConfigMu sync.RWMutex

	statsData model.StatisticsData
	notify    chan struct{}
}

// NewHandlers creates a Handlers instance with parsed templates and initial state.
func NewHandlers(log *slog.Logger, st *store.Store, tmplFS fs.FS, samples model.SampleJobs) (*Handlers, error) {
	tmpl, err := parseTemplates(tmplFS)
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Handlers{
		log:        log,
		tmpl:       tmpl,
		store:      st,
		pool:       pool.New(pool.Config{}),
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
	servers, _ := h.store.ListServers()
	return model.SelfConf{
		Servers: servers,
		Filter: readListCookie(r, "filter", ",", []string{
			"current-connections", "current-jobs-buried", "current-jobs-delayed",
			"current-jobs-ready", "current-jobs-reserved", "current-jobs-urgent", "current-tubes",
		}),
		TubeFilters: readListCookie(r, "tubefilter", ",", []string{
			"current-jobs-urgent", "current-jobs-ready", "current-jobs-reserved",
			"current-jobs-delayed", "current-jobs-buried", "total-jobs",
		}),
		TubeSelector:            rawCookieValue(r, "tubeSelector"),
		TubePauseSeconds:        readIntCookie(r, "tubePauseSeconds", -1),
		AutoRefreshTimeoutMs:    readIntCookie(r, "autoRefreshTimeoutMs", 500),
		SearchResultLimit:       readIntCookie(r, "searchResultLimit", 25),
		DisableJSONDecode:       readBoolCookie(r, "isDisabledJsonDecode"),
		DisableJobDataHighlight: readBoolCookie(r, "isDisabledJobDataHighlight"),
		EnableBase64Decode:      readBoolCookie(r, "isEnabledBase64Decode"),
	}
}

// --- Cookie helpers ---

func readListCookie(r *http.Request, name, sep string, defaults []string) []string {
	if c := cookieValue(r, name); c != "" {
		return compactUnique(strings.Split(c, sep))
	}
	return defaults
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
		Conf:        conf,
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

func (h *Handlers) handleServerAdd(w http.ResponseWriter, r *http.Request) {
	server := r.FormValue("server")
	if !isValidServer(server) {
		http.Error(w, "invalid server address", http.StatusBadRequest)
		return
	}
	if err := h.store.AddServer(server); err != nil {
		h.log.Error("failed to add server", "server", server, "error", err)
		http.Error(w, "failed to save server", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) handleServerRemove(w http.ResponseWriter, r *http.Request) {
	server := r.URL.Query().Get("server")
	if err := h.store.RemoveServer(server); err != nil {
		h.log.Error("failed to remove server", "server", server, "error", err)
	}
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
		if err := h.clearTubes(ctx, server, r.Form); err != nil {
			hxError(w, "Failed to clear tubes")
			return
		}
		hxToast(w, "success", "Tubes cleared", false)
		w.WriteHeader(http.StatusNoContent)
	default:
		ts := h.tubeStats(ctx, server)
		tubes := make([]string, len(ts))
		for i, t := range ts {
			tubes[i] = t.Name
		}
		h.render(w, r, "server.html", &pageData{
			Conf:          conf,
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
		if err := h.addJob(ctx, server,
			r.PostFormValue("tubeName"), r.PostFormValue("tubeData"),
			r.PostFormValue("tubePriority"), r.PostFormValue("tubeDelay"), r.PostFormValue("tubeTtr")); err != nil {
			hxError(w, "Failed to add job")
			return
		}
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
		if err := h.kick(ctx, server, tube, r.FormValue("count")); err != nil {
			setFlash(w, "error", "Failed to kick jobs")
		} else {
			setFlash(w, "success", "Jobs kicked")
		}
		h.redirectToTube(w, r, server, tube)
	case "kickJob":
		if !requirePOST(w, r) {
			return
		}
		if err := h.kickJob(ctx, server, q.Get("jobid")); err != nil {
			setFlash(w, "error", "Failed to kick job")
		} else {
			setFlash(w, "success", "Job kicked")
		}
		h.redirectToTube(w, r, server, tube)
	case "pause":
		if !requirePOST(w, r) {
			return
		}
		if err := h.pause(ctx, server, tube, q.Get("count"), conf.TubePauseSeconds); err != nil {
			setFlash(w, "error", "Failed to update pause")
		} else {
			setFlash(w, "success", "Tube pause updated")
		}
		h.redirectToTube(w, r, server, tube)
	case "moveJobsTo":
		if !requirePOST(w, r) {
			return
		}
		destTube := q.Get("destTube")
		if destTube == "" {
			destTube = tube
		}
		if err := h.moveJobsTo(ctx, server, tube, destTube, q.Get("state"), q.Get("destState")); err != nil {
			setFlash(w, "error", "Failed to move jobs")
		} else {
			setFlash(w, "success", "Jobs moved")
		}
		h.redirectToTube(w, r, server, destTube)
	case "deleteAll":
		if !requirePOST(w, r) {
			return
		}
		if err := h.deleteAll(ctx, server, tube, q.Get("state")); err != nil {
			setFlash(w, "error", "Failed to delete jobs")
		} else {
			setFlash(w, "success", "All jobs deleted")
		}
		h.redirectToTube(w, r, server, tube)
	case "deleteJob":
		if !requirePOST(w, r) {
			return
		}
		if err := h.deleteJob(ctx, server, q.Get("jobid")); err != nil {
			setFlash(w, "error", "Failed to delete job")
		} else {
			setFlash(w, "success", "Job deleted")
		}
		h.redirectToTube(w, r, server, tube)
	case "loadSample":
		if !requirePOST(w, r) {
			return
		}
		if err := h.loadSample(ctx, server, tube, q.Get("key")); err != nil {
			setFlash(w, "error", "Failed to load sample")
		} else {
			setFlash(w, "success", "Sample loaded")
		}
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
		Conf:          conf,
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

	data.SampleTubeMap = h.samplesForTube(tube)
	return data
}

// --- Data fetching helpers ---

func (h *Handlers) serverStats(ctx context.Context, conf model.SelfConf) []serverStat {
	stats := make([]serverStat, len(conf.Servers))
	var wg sync.WaitGroup
	for i, addr := range conf.Servers {
		wg.Go(func() { stats[i] = h.fetchServerStat(ctx, addr) })
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
		wg.Go(func() {
			tubes := h.listTubesSorted(ctx, server)
			mu.Lock()
			result[server] = tubes
			mu.Unlock()
		})
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
