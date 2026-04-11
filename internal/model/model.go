package model

import (
	"container/list"
	"sync"
)

// Default beanstalkd job parameters.
const (
	DefaultDelay            = 0
	DefaultPriority  uint32 = 1024
	DefaultTTR              = 60
	DefaultTubePauseSeconds = 3600
)

// SelfConf holds per-request user preferences loaded from cookies.
// Created per-request by readCookies and passed through the call chain.
type SelfConf struct {
	Filter      []string
	Servers     []string
	TubeFilters []string

	TubeSelector string

	TubePauseSeconds     int
	AutoRefreshTimeoutMs int
	SearchResultLimit    int

	DisableJSONDecode       bool
	DisableJobDataHighlight bool
	EnableBase64Decode      bool
}

// SampleJobs holds the collection of sample jobs and their tube associations.
type SampleJobs struct {
	Jobs  []SampleJob  `json:"jobs"`
	Tubes []SampleTube `json:"tubes"`
}

// SampleJob represents a saved job template.
type SampleJob struct {
	Tubes []string `json:"tubes"`
	Key   string   `json:"key"`
	Name  string   `json:"name"`
	Data  string   `json:"data"`
	TTR   int      `json:"ttr"`
}

// SampleTube maps a tube name to sample job keys available on that tube.
type SampleTube struct {
	Keys []string `json:"keys"`
	Name string   `json:"name"`
}

// StatsConfig holds statistics collection parameters.
type StatsConfig struct {
	Collection int
	Frequency  int
}

// StatisticsData holds time-series statistics data protected by a mutex.
type StatisticsData struct {
	sync.RWMutex
	Server map[string]map[string]map[string]*list.List
}

// SearchResult holds a single job found by tube search.
type SearchResult struct {
	Data  string
	State string
	ID    uint64
}

// JobStatsOrder defines the display order for job statistics.
var JobStatsOrder = []string{
	"id", "tube", "state", "pri", "age", "delay", "ttr",
	"time-left", "file", "reserves", "timeouts", "releases", "buries", "kicks",
}

// StatField maps a display key to a beanstalkd stat name.
type StatField struct {
	Key  string // display key, e.g. "ready"
	Stat string // beanstalkd stat name, e.g. "current-jobs-ready"
}

// StatisticsFields defines which tube stats to collect for time-series.
var StatisticsFields = []StatField{
	{"ready", "current-jobs-ready"},
	{"delayed", "current-jobs-delayed"},
	{"reserved", "current-jobs-reserved"},
	{"buried", "current-jobs-buried"},
}

// StatsDesc pairs a stat name with its human-readable description.
type StatsDesc struct {
	Name string
	Desc string
}

// Server stats filter groups.
var BinlogStatsGroups = []StatsDesc{
	{"binlog-current-index", "the index of the current binlog file being written to. If binlog is not active this value will be 0"},
	{"binlog-max-size", "the maximum size in bytes a binlog file is allowed to get before a new binlog file is opened"},
	{"binlog-oldest-index", "the index of the oldest binlog file needed to store the current jobs"},
	{"binlog-records-migrated", "the cumulative number of records written as part of compaction"},
	{"binlog-records-written", "the cumulative number of records written to the binlog"},
}

var CmdStatsGroups = []StatsDesc{
	{"cmd-bury", "the cumulative number of bury commands"},
	{"cmd-delete", "the cumulative number of delete commands"},
	{"cmd-ignore", "the cumulative number of ignore commands"},
	{"cmd-kick", "the cumulative number of kick commands"},
	{"cmd-list-tube-used", "the cumulative number of list-tube-used commands"},
	{"cmd-list-tubes", "the cumulative number of list-tubes commands"},
	{"cmd-list-tubes-watched", "the cumulative number of list-tubes-watched commands"},
	{"cmd-pause-tube", "the cumulative number of pause-tube commands"},
	{"cmd-peek", "the cumulative number of peek commands"},
	{"cmd-peek-buried", "the cumulative number of peek-buried commands"},
	{"cmd-peek-delayed", "the cumulative number of peek-delayed commands"},
	{"cmd-peek-ready", "the cumulative number of peek-ready commands"},
	{"cmd-put", "the cumulative number of put commands"},
	{"cmd-release", "the cumulative number of release commands"},
	{"cmd-reserve", "the cumulative number of reserve commands"},
	{"cmd-stats", "the cumulative number of stats commands"},
	{"cmd-stats-job", "the cumulative number of stats-job commands"},
	{"cmd-stats-tube", "the cumulative number of stats-tube commands"},
	{"cmd-use", "the cumulative number of use commands"},
	{"cmd-watch", "the cumulative number of watch commands"},
}

var CurrentStatsGroups = []StatsDesc{
	{"current-connections", "the number of currently open connections"},
	{"current-jobs-buried", "the number of buried jobs"},
	{"current-jobs-delayed", "the number of delayed jobs"},
	{"current-jobs-ready", "the number of jobs in the ready queue"},
	{"current-jobs-reserved", "the number of jobs reserved by all clients"},
	{"current-jobs-urgent", "the number of ready jobs with priority < 1024"},
	{"current-producers", "the number of open connections that have each issued at least one put command"},
	{"current-tubes", "the number of currently-existing tubes"},
	{"current-waiting", "the number of open connections that have issued a reserve command but not yet received a response"},
	{"current-workers", "the number of open connections that have each issued at least one reserve command"},
}

var OtherStatsGroups = []StatsDesc{
	{"hostname", "the hostname of the machine as determined by uname"},
	{"id", "a random id string for this server process, generated when each beanstalkd process starts"},
	{"job-timeouts", "the cumulative count of times a job has timed out"},
	{"max-job-size", "the maximum number of bytes in a job"},
	{"pid", "the process id of the server"},
	{"rusage-stime", "the cumulative system CPU time of this process in seconds and microseconds"},
	{"rusage-utime", "the cumulative user CPU time of this process in seconds and microseconds"},
	{"total-connections", "the cumulative count of connections"},
	{"total-jobs", "the cumulative count of jobs created"},
	{"uptime", "the number of seconds since this server process started running"},
	{"version", "the version string of the server"},
}

var TubeStatFields = []StatsDesc{
	{"current-jobs-urgent", "number of ready jobs with priority < 1024 in this tube"},
	{"current-jobs-ready", "number of jobs in the ready queue in this tube"},
	{"current-jobs-reserved", "number of jobs reserved by all clients in this tube"},
	{"current-jobs-delayed", "number of delayed jobs in this tube"},
	{"current-jobs-buried", "number of buried jobs in this tube"},
	{"current-using", "number of open connections that are currently using this tube"},
	{"current-waiting", "number of open connections that have issued a reserve command while watching this tube but not yet received a response"},
	{"current-watching", "number of open connections that are currently watching this tube"},
	{"cmd-delete", "cumulative number of delete commands for this tube"},
	{"cmd-pause-tube", "cumulative number of pause-tube commands for this tube"},
	{"pause", "number of seconds the tube has been paused for"},
	{"pause-time-left", "number of seconds until the tube is un-paused"},
	{"total-jobs", "cumulative count of jobs created in this tube in the current beanstalkd process"},
}
