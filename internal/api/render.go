package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"

	"github.com/xuri/aurora/internal/model"
)

// pageData is the universal data structure passed to all templates.
type pageData struct {
	Flash *flash

	// Navigation.
	Servers       []string
	CurrentServer string
	CurrentTube   string
	Tubes         []string

	// Server list page.
	ServerStats []serverStat
	Filter      []string

	// Server detail page.
	TubeStats   []tubeStat
	TubeFilters []string

	// Tube detail page.
	TubeInfo     map[string]string
	ReadyJob     *jobData
	DelayedJob   *jobData
	BuriedJob    *jobData
	HasStats     bool
	PauseSeconds string

	// Search.
	SearchStr     string
	SearchResults []model.SearchResult
	SearchLimit   string

	// Samples.
	SampleJobs    []model.SampleJob
	SampleJob     *model.SampleJob
	SampleTubeMap map[string][]sampleForTube
	ServerTubes   map[string][]string

	// Statistics.
	StatsFrequency  int
	StatsCollection int
	StatsServer     string
	StatsTube       string
	StatsTubes      map[string]map[string]bool

	// Settings.
	Conf model.SelfConf

	// Misc.
	Version     float64
	UpdateAlert string

	// Stats filter reference data.
	BinlogStatsGroups  []map[string]string
	CmdStatsGroups     []map[string]string
	CurrentStatsGroups []map[string]string
	OtherStatsGroups   []map[string]string
	TubeStatFields     []map[string]string
}

type serverStat struct {
	Addr    string
	Online  bool
	Stats   map[string]string
}

type tubeStat struct {
	Name  string
	Stats map[string]string
}

type jobData struct {
	ID    uint64
	Data  string
	Stats map[string]string
}

type sampleForTube struct {
	Key  string
	Name string
}

func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"tubeURL": func(server, tube string) string {
			return fmt.Sprintf("/tube?server=%s&tube=%s", server, url.QueryEscape(tube))
		},
		"serverURL": func(server string) string {
			return fmt.Sprintf("/server?server=%s", server)
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"prettyJSON": func(s string) string {
			var out bytes.Buffer
			if err := json.Indent(&out, []byte(s), "", "  "); err != nil {
				return s
			}
			return out.String()
		},
		"base64Decode": func(s string) string {
			data, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return s
			}
			return string(data)
		},
		"formatJobData": func(conf model.SelfConf, body string) string {
			data := body
			if !conf.DisableJSONDecode {
				var out bytes.Buffer
				if err := json.Indent(&out, []byte(body), "", "  "); err == nil {
					data = out.String()
				}
			}
			if conf.EnableBase64Decode {
				if decoded, err := base64.StdEncoding.DecodeString(data); err == nil {
					data = string(decoded)
				}
			}
			return data
		},
		"checkedIf": func(cond bool) template.HTMLAttr {
			if cond {
				return template.HTMLAttr(`checked`)
			}
			return ""
		},
		"contains": func(slice []string, item string) bool {
			for _, v := range slice {
				if v == item {
					return true
				}
			}
			return false
		},
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
		"add": func(a, b int) int { return a + b },
		"dict": func(pairs ...any) map[string]any {
			m := make(map[string]any, len(pairs)/2)
			for i := 0; i < len(pairs)-1; i += 2 {
				m[pairs[i].(string)] = pairs[i+1]
			}
			return m
		},
		"defaultPriority": func() uint32 { return model.DefaultPriority },
		"defaultDelay":    func() int { return model.DefaultDelay },
		"defaultTTR":      func() int { return model.DefaultTTR },
		"jobStatsOrder":   func() []string { return model.JobStatsOrder },
	}
}

//go:generate echo "templates are embedded via admin_tmpl"

func parseTemplates(tmplFS fs.FS) (*template.Template, error) {
	return template.New("").Funcs(templateFuncMap()).ParseFS(tmplFS, "*.html")
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func (h *Handlers) render(w http.ResponseWriter, r *http.Request, name string, data *pageData) {
	data.Version = h.cfg.Version
	data.Conf = readCookies(r, h.cfg)
	data.UpdateAlert = h.checkUpdate()

	if f := getFlash(w, r); f != nil {
		data.Flash = f
	}

	var tmplName string
	if isHTMX(r) {
		tmplName = name
	} else {
		tmplName = "layout.html"
	}

	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, tmplName, data); err != nil {
		h.log.Error("template render failed", "template", tmplName, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func (h *Handlers) renderFragment(w http.ResponseWriter, r *http.Request, name string, data any) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		h.log.Error("fragment render failed", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}
