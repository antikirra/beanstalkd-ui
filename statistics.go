package main

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// statisticPreferenceSave saves the statistics preference settings.
func statisticPreferenceSave(conf SelfConf, f url.Values, w http.ResponseWriter, r *http.Request) {
	var err error
	var collection, frequency string
	var tubes []string
	alert := alertHTML("sfsa", "danger", " Required fields are not set correct")
	for k, v := range f {
		switch k {
		case "frequency":
			frequency = v[0]
		case "collection":
			collection = v[0]
		case "action":
			continue
		default:
			t := strings.TrimSuffix(strings.TrimPrefix(k, `tubes[`), `]`)
			tubes = append(tubes, t)
		}
	}
	if len(tubes) == 0 || collection == "" || frequency == "" {
		fmt.Fprint(w, tplStatisticSetting(conf, tplStatisticEdit(conf,alert)))
		return
	}
	err = saveStatisticsConfig(collection, frequency, tubes)
	if err != nil {
		fmt.Fprint(w, tplStatisticSetting(conf, tplStatisticEdit(conf, alertHTML("sfsa", "danger", " Save statistics preference error"))))
		return
	}
	fmt.Fprint(w, tplStatisticSetting(conf, tplStatisticEdit(conf, alertHTML("sfsa", "success", " Statistics preference saved"))))
}

// saveStatisticsConfig validates and applies the statistics collection settings.
func saveStatisticsConfig(collection, frequency string, tubes []string) error {
	c, err := strconv.Atoi(collection)
	if err != nil {
		return err
	}
	f, err := strconv.Atoi(frequency)
	if err != nil {
		return err
	}
	if c < 1 {
		c = 0
	}
	if f < 1 {
		f = 1
	}
	statsConfigMu.Lock()
	statsConfig.Collection = c
	statsConfig.Frequency = f
	statsConfigMu.Unlock()

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
	statisticsData.Lock()
	statisticsData.Server = newServer
	statisticsData.Unlock()
	notify <- true
	return nil
}

// statistic provide method to control statisticAgent collect the statistics
// data in a Goroutine.
func statisticsCollector(ctx context.Context) {
	freq := getStatsFrequency()
	ticker := time.NewTicker(time.Duration(freq) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
			ticker.Stop()
			freq = getStatsFrequency()
			ticker = time.NewTicker(time.Duration(freq) * time.Second)
		case <-ticker.C:
			statsConfigMu.RLock()
			collection := statsConfig.Collection
			statsConfigMu.RUnlock()
			if collection == 0 {
				continue
			}

			statisticsData.RLock()
			serversCopy := make(map[string][]string)
			for k, v := range statisticsData.Server {
				for t := range v {
					serversCopy[k] = append(serversCopy[k], t)
				}
			}
			statisticsData.RUnlock()

			for k, tubes := range serversCopy {
				for _, t := range tubes {
					_ = statisticAgent(k, t, collection)
				}
			}
		}
	}
}

// getStatsFrequency returns the statistics collection frequency, minimum 1 second.
func getStatsFrequency() int {
	statsConfigMu.RLock()
	f := statsConfig.Frequency
	statsConfigMu.RUnlock()
	if f < 1 {
		return 1
	}
	return f
}

// statisticAgent collect the statistics data by given server and tube.
func statisticAgent(server, tube string, collection int) error {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return err
	}
	defer conn.Close()

	statsMap, err := newTube(conn, tube).Stats()
	if err != nil {
		return err
	}
	now := time.Now()
	for _, field := range statisticsFields {
		for k, v := range field {
			val, err := strconv.Atoi(statsMap[v])
			if err != nil {
				continue
			}
			statisticsData.Lock()
			srvMap := statisticsData.Server[server]
			if srvMap == nil {
				statisticsData.Unlock()
				continue
			}
			tubeMap := srvMap[tube]
			if tubeMap == nil {
				statisticsData.Unlock()
				continue
			}
			if tubeMap[k] == nil {
				tubeMap[k] = list.New()
			}
			if tubeMap[k].Len() >= collection {
				tubeMap[k].Remove(tubeMap[k].Back())
			}
			tubeMap[k].PushFront([]int{now.Year(), int(now.Month()), now.Day(), now.Hour(), now.Minute(), now.Second(), val})
			statisticsData.Unlock()
		}
	}
	return nil
}

// statisticsJSON returns real-time statistics data as JSON for the given server and tube.
func statisticsJSON(server, tube string) string {
	result := make(map[string][][]int)

	statisticsData.RLock()
	srvMap := statisticsData.Server[server]
	if srvMap != nil {
		tubeMap := srvMap[tube]
		for _, field := range statisticsFields {
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
					series = append(series, e.Value.([]int))
				}
				result[k] = series
			}
		}
	}
	statisticsData.RUnlock()

	data, err := json.Marshal(result)
	if err != nil {
		return "{}"
	}
	return string(data)
}
