package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/beanstalkd/go-beanstalk"

	"github.com/antikirra/beanstalkd-ui/internal/model"
	"github.com/antikirra/beanstalkd-ui/internal/pool"
)

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
			Conf:          conf,
			PageTitle:     "Samples",
			CurrentServer: server,
			SampleJobs:    jobs,
		})
	case "newSample":
		h.render(w, r, "sample_edit.html", &pageData{
			Conf:          conf,
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
			Conf:          conf,
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
		if err := h.deleteSamples(q.Get("key")); err != nil {
			flashRedirect(w, r, "error", "Failed to delete sample", "/sample?action=manageSamples")
			return
		}
		flashRedirect(w, r, "success", "Sample deleted", "/sample?action=manageSamples")
	default:
		http.Redirect(w, r, "/sample?action=manageSamples", http.StatusSeeOther)
	}
}

func (h *Handlers) samplesForTube(tube string) map[string][]sampleForTube {
	h.sampleJobsMu.RLock()
	defer h.sampleJobsMu.RUnlock()
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
		return map[string][]sampleForTube{tube: samples}
	}
	return nil
}

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

// saveSample persists sample jobs to the store. Caller must hold sampleJobsMu.
func (h *Handlers) saveSample() error {
	return h.store.SaveSamples(h.sampleJobs)
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

func (h *Handlers) deleteSamples(key string) error {
	if key == "" {
		return nil
	}
	h.sampleJobsMu.Lock()
	defer h.sampleJobsMu.Unlock()
	h.removeSampleByKey(key)
	return h.saveSample()
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

func (h *Handlers) loadSample(ctx context.Context, server, tube, key string) error {
	job := h.findSampleByKey(key)
	if job == nil || job.Data == "" {
		return fmt.Errorf("sample not found")
	}
	return h.pool.WithConn(ctx, server, pool.WritePool, func(conn *beanstalk.Conn) error {
		_, err := newTube(conn, tube).Put(
			[]byte(job.Data), model.DefaultPriority,
			time.Duration(model.DefaultDelay)*time.Second,
			time.Duration(job.TTR)*time.Second,
		)
		return err
	})
}
