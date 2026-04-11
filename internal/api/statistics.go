package api

import (
	"container/list"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/beanstalkd/go-beanstalk"

	"github.com/antikirra/beanstalkd-ui/internal/model"
	"github.com/antikirra/beanstalkd-ui/internal/pool"
)

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
			Conf:        conf,
			PageTitle:   "Statistics",
			StatsServer: server,
			StatsTube:   tube,
		})
	}
}

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
		Conf:            conf,
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
