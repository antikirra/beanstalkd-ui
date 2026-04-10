package main

import (
	"container/list"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/aurora/beanstalk"
)

// statisticPreferenceSave provide method to save statistics preference
// settings.
func statisticPreferenceSave(f url.Values, w http.ResponseWriter, r *http.Request) {
	var err error
	var collection, frequency string
	var tubes []string
	alert := `<div class="alert alert-danger" id="sfsa"><button type="button" class="close" onclick="$('#sfsa').fadeOut('fast');">×</button><span> Required fields are not set correct</span></div>`
	err = readConf()
	if err != nil {
		fmt.Fprint(w, tplStatisticSetting(tplStatisticEdit(`<div class="alert alert-danger"><button type="button" class="close" data-dismiss="alert">×</button><span> Read config error</span></div>`)))
		return
	}
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
		fmt.Fprint(w, tplStatisticSetting(tplStatisticEdit(alert)))
		return
	}
	err = saveStatisticsConfig(collection, frequency, tubes)
	if err != nil {
		fmt.Fprint(w, tplStatisticSetting(tplStatisticEdit(`<div class="alert alert-danger" id="sfsa"><button type="button" class="close" onclick="$('#sfsa').fadeOut('fast');">×</button><span> Save statistics preference error</span></div>`)))
		return
	}
	fmt.Fprint(w, tplStatisticSetting(tplStatisticEdit(`<div class="alert alert-success" id="sfsa"><button type="button" class="close" onclick="$('#sfsa').fadeOut('fast');">×</button><span> Statistics preference saved</span></div>`)))
}

// saveStatisticsConfig validate collection and frequency parameter and send notify
// to statistic Goroutine that the configuration of statistics preference
// settings has changed.
func saveStatisticsConfig(collection string, frequency string, tubes []string) error {
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
	selfConf.StatisticsCollection = c
	selfConf.StatisticsFrequency = f
	statisticsDataServer = make(map[string]map[string]map[string]*list.List)
	for _, v := range tubes {
		addr := strings.Split(v, `:`)
		if len(addr) != 3 {
			continue
		}
		tube := make(map[string]map[string]*list.List)
		tube[addr[2]] = make(map[string]*list.List)
		s, ok := statisticsDataServer[addr[0]+`:`+addr[1]]
		if !ok {
			statisticsDataServer[addr[0]+`:`+addr[1]] = tube
		} else {
			s[addr[2]] = tube[addr[2]]
		}
	}
	statisticsData.Lock()
	statisticsData.Server = statisticsDataServer
	statisticsData.Unlock()
	notify <- true
	return nil
}

// statistic provide method to control statisticAgent collect the statistics
// data in a Goroutine.
func statisticsCollector() {
	freq := selfConf.StatisticsFrequency
	if freq < 1 {
		freq = 1
	}
	ticker := time.NewTicker(time.Duration(freq) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-notify:
			ticker.Stop()
			freq = selfConf.StatisticsFrequency
			if freq < 1 {
				freq = 1
			}
			ticker = time.NewTicker(time.Duration(freq) * time.Second)
		case <-ticker.C:
			statisticsData.RLock()
			serversCopy := make(map[string][]string)
			for k, v := range statisticsData.Server {
				for t := range v {
					serversCopy[k] = append(serversCopy[k], t)
				}
			}
			statisticsData.RUnlock()
			collection := selfConf.StatisticsCollection
			if collection == 0 {
				continue
			}
			for k, tubes := range serversCopy {
				for _, t := range tubes {
					_ = statisticAgent(k, t)
				}
			}
		}
	}
}

// statisticAgent collect the statistics data by given server and tube.
func statisticAgent(server string, tube string) error {
	var err error
	var bstkConn *beanstalk.Conn
	if bstkConn, err = beanstalk.Dial("tcp", server); err != nil {
		return err
	}
	defer bstkConn.Close()
	tubeStats := &beanstalk.Tube{
		Conn: bstkConn,
		Name: tube,
	}
	statsMap, err := tubeStats.Stats()
	if err != nil {
		return err
	}
	for _, field := range statisticsFields {
		for k, v := range field {
			t := time.Now()
			stats, err := strconv.Atoi(statsMap[v])
			if err != nil {
				continue
			}
			statisticsData.Lock()
			_, ok := statisticsData.Server[server][tube][k]
			if !ok {
				statisticsData.Server[server][tube][k] = list.New()
			}
			if statisticsData.Server[server][tube][k].Len() >= selfConf.StatisticsCollection {
				front := statisticsData.Server[server][tube][k].Back()
				statisticsData.Server[server][tube][k].Remove(front)
			}
			statisticsData.Server[server][tube][k].PushFront([]int{t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second(), stats})
			statisticsData.Unlock()
		}
	}
	return nil
}

// statisticsJSON returns real-time statistics data as JSON for the given server and tube.
func statisticsJSON(server, tube string) string {
	result := make(map[string][][]int)

	statisticsData.RLock()
	for _, field := range statisticsFields {
		for k := range field {
			l, ok := statisticsData.Server[server][tube][k]
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
	statisticsData.RUnlock()

	data, err := json.Marshal(result)
	if err != nil {
		return "{}"
	}
	return string(data)
}
