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
	"slices"
	"strings"

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
	PageTitle string
	PagePath  string
	Version   float64

	// Stats filter reference data.
	BinlogStatsGroups  []map[string]string
	CmdStatsGroups     []map[string]string
	CurrentStatsGroups []map[string]string
	OtherStatsGroups   []map[string]string
	TubeStatFields     []map[string]string
}

type serverStat struct {
	Addr   string
	Online bool
	Stats  map[string]string
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
		"contains": slices.Contains[[]string, string],
		"add": func(a, b int) int { return a + b },
		"dict": func(pairs ...any) map[string]any {
			m := make(map[string]any, len(pairs)/2)
			for i := 0; i < len(pairs)-1; i += 2 {
				m[pairs[i].(string)] = pairs[i+1]
			}
			return m
		},
		"refreshInterval": func(ms int) string {
			if ms <= 0 {
				ms = 500
			}
			if ms < 1000 {
				return fmt.Sprintf("every %dms", ms)
			}
			return fmt.Sprintf("every %ds", ms/1000)
		},
		"statVal": func(col, val string) template.HTML {
			if val == "" || val == "0" {
				return template.HTML(val)
			}
			if strings.Contains(col, "buried") {
				return template.HTML(`<span class="val-danger">` + template.HTMLEscapeString(val) + `</span>`)
			}
			if strings.Contains(col, "urgent") {
				return template.HTML(`<span class="val-warning">` + template.HTMLEscapeString(val) + `</span>`)
			}
			return template.HTML(val)
		},
		"shortCol": func(s string) string {
			s = strings.TrimPrefix(s, "current-")
			s = strings.TrimPrefix(s, "cmd-")
			return s
		},
		"defaultPriority": func() uint32 { return model.DefaultPriority },
		"defaultDelay":    func() int { return model.DefaultDelay },
		"defaultTTR":      func() int { return model.DefaultTTR },
		"jobStatsOrder":   func() []string { return model.JobStatsOrder },
	}
}

// templateSet holds pre-parsed per-page templates (layout + page content).
type templateSet struct {
	pages     map[string]*template.Template
	fragments *template.Template
}

// parseTemplates builds a per-page template set.
// Each page template is a clone of the layout with the page's "content" block added on top.
func parseTemplates(tmplFS fs.FS) (*templateSet, error) {
	funcMap := templateFuncMap()

	// Parse the shared layout once. Include partials (_*.html) if any exist.
	layout, err := template.New("layout.html").Funcs(funcMap).ParseFS(tmplFS, "layout.html")
	if err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}
	if partials, _ := fs.Glob(tmplFS, "_*.html"); len(partials) > 0 {
		if _, err := layout.ParseFS(tmplFS, "_*.html"); err != nil {
			return nil, fmt.Errorf("parse partials: %w", err)
		}
	}

	// Each page file defines {{define "content"}}. Clone layout+partials and parse the page on top.
	pageFiles := []string{
		"servers.html",
		"server.html",
		"tube.html",
		"samples.html",
		"sample_edit.html",
		"statistics.html",
		"statistics_pref.html",
		"settings.html",
	}

	pages := make(map[string]*template.Template, len(pageFiles))
	for _, name := range pageFiles {
		clone, err := layout.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone layout for %s: %w", name, err)
		}
		if _, err := clone.ParseFS(tmplFS, name); err != nil {
			return nil, fmt.Errorf("parse page %s: %w", name, err)
		}
		pages[name] = clone
	}

	// Parse all files together for fragment lookups (server_table, tube_table, etc.).
	fragments, err := template.New("").Funcs(funcMap).ParseFS(tmplFS, "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse fragments: %w", err)
	}

	return &templateSet{pages: pages, fragments: fragments}, nil
}

func (h *Handlers) render(w http.ResponseWriter, r *http.Request, name string, data *pageData) {
	data.Version = h.cfg.Version
	data.Conf = readCookies(r, h.cfg)
	data.PagePath = r.URL.Path

	if f := getFlash(w, r); f != nil {
		data.Flash = f
	}

	pageTmpl, ok := h.tmpl.pages[name]
	if !ok {
		h.log.Error("page template not found", "template", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := pageTmpl.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		h.log.Error("template render failed", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func (h *Handlers) renderFragment(w http.ResponseWriter, r *http.Request, name string, data any) {
	var buf bytes.Buffer
	if err := h.tmpl.fragments.ExecuteTemplate(&buf, name, data); err != nil {
		h.log.Error("fragment render failed", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}
