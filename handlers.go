package main

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
)

// handlerMain serves the main dashboard page.
func handlerMain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Server", "Go WebServer")
	w.Header().Set("Content-Type", "text/html")
	readCookies(r)
	server := r.URL.Query().Get("server")
	fmt.Fprint(w, tplMain(getServerStatus(), server))
}

// handlerServerList returns the server list HTML fragment (AJAX).
func handlerServerList(w http.ResponseWriter, r *http.Request) {
	setHeader(w, r)
	readCookies(r)
	fmt.Fprint(w, getServerStatus())
}

// serversRemove removes a server from the configuration and cookies. POST only.
func serversRemove(w http.ResponseWriter, r *http.Request) {
	setHeader(w, r)
	readCookies(r)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	server := r.URL.Query().Get("removeServer")
	removeServerInCookie(server, w, r)
	removeServerInConfig(server)
	http.Redirect(w, r, "./public", http.StatusTemporaryRedirect)
}

// handlerServer serves the server detail page and its AJAX actions.
func handlerServer(w http.ResponseWriter, r *http.Request) {
	setHeader(w, r)
	readCookies(r)
	server := r.URL.Query().Get("server")

	switch r.URL.Query().Get("action") {
	case "reloader":
		fmt.Fprint(w, getServerTubes(server))
	case "clearTubes":
		_ = r.ParseForm()
		clearTubes(server, r.Form)
		fmt.Fprint(w, `{"result":true}`)
	default:
		fmt.Fprint(w, tplServer(getServerTubes(server), html.EscapeString(server)))
	}
}

// handlerTube serves the tube detail page and its actions.
func handlerTube(w http.ResponseWriter, r *http.Request) {
	setHeader(w, r)
	readCookies(r)

	q := r.URL.Query()
	server := q.Get("server")
	tube := q.Get("tube")
	action := q.Get("action")

	switch action {
	case "addjob":
		addJob(server,
			r.PostFormValue("tubeName"), r.PostFormValue("tubeData"),
			r.PostFormValue("tubePriority"), r.PostFormValue("tubeDelay"), r.PostFormValue("tubeTtr"))
		fmt.Fprint(w, `{"result":true}`)
	case "search":
		content := searchTube(server, tube, q.Get("limit"), q.Get("searchStr"))
		fmt.Fprint(w, tplTube(content, html.EscapeString(server), html.EscapeString(tube)))
	case "addSample":
		_ = r.ParseForm()
		addSample(server, r.Form, w)
	default:
		handleRedirect(w, r, server, tube, action, q)
	}
}

// redirectToTube sends a 307 redirect back to the tube page.
func redirectToTube(w http.ResponseWriter, r *http.Request, server, tube string) {
	target := fmt.Sprintf("./tube?server=%s&tube=%s", server, url.QueryEscape(tube))
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

// handleRedirect dispatches state-changing tube actions. POST required for mutations.
func handleRedirect(w http.ResponseWriter, r *http.Request, server, tube, action string, q url.Values) {
	// State-changing actions require POST to prevent CSRF.
	switch action {
	case "kick", "kickJob", "pause", "deleteAll", "deleteJob", "moveJobsTo", "loadSample":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	}

	switch action {
	case "kick":
		kick(server, tube, q.Get("count"))
		redirectToTube(w, r, server, tube)
	case "kickJob":
		kickJob(server, tube, q.Get("jobid"))
		redirectToTube(w, r, server, tube)
	case "pause":
		pause(server, tube, q.Get("count"))
		redirectToTube(w, r, server, tube)
	case "moveJobsTo":
		destTube := q.Get("destTube")
		if destTube == "" {
			destTube = tube
		}
		moveJobsTo(server, tube, destTube, q.Get("state"), q.Get("destState"))
		redirectToTube(w, r, server, destTube)
	case "deleteAll":
		deleteAll(server, tube)
		redirectToTube(w, r, server, tube)
	case "deleteJob":
		deleteJob(server, tube, q.Get("jobid"))
		redirectToTube(w, r, server, tube)
	case "loadSample":
		loadSample(server, tube, q.Get("key"))
		redirectToTube(w, r, server, tube)
	default:
		fmt.Fprint(w, tplTube(currentTubeJobs(server, tube), html.EscapeString(server), html.EscapeString(tube)))
	}
}

// handlerSample serves the sample jobs management page.
func handlerSample(w http.ResponseWriter, r *http.Request) {
	setHeader(w, r)
	readCookies(r)

	q := r.URL.Query()
	server := q.Get("server")

	switch q.Get("action") {
	case "manageSamples":
		fmt.Fprint(w, tplSampleJobsManage(getSampleJobList(), server))
	case "newSample":
		fmt.Fprint(w, tplSampleJobsManage(tplSampleJobEdit("", ""), server))
	case "editSample":
		fmt.Fprint(w, tplSampleJobsManage(tplSampleJobEdit(html.EscapeString(q.Get("key")), ""), server))
	case "actionNewSample":
		_ = r.ParseForm()
		newSample(server, r.Form, w, r)
	case "actionEditSample":
		_ = r.ParseForm()
		editSample(server, r.Form, q.Get("key"), w, r)
	case "deleteSample":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		deleteSamples(q.Get("key"))
		http.Redirect(w, r, "./sample?action=manageSamples", http.StatusTemporaryRedirect)
	}
}

// handlerStatistics serves the statistics page and its AJAX actions.
func handlerStatistics(w http.ResponseWriter, r *http.Request) {
	setHeader(w, r)
	readCookies(r)

	q := r.URL.Query()
	server := q.Get("server")
	tube := q.Get("tube")

	switch q.Get("action") {
	case "preference":
		fmt.Fprint(w, tplStatisticSetting(tplStatisticEdit("")))
	case "save":
		_ = r.ParseForm()
		statisticPreferenceSave(r.Form, w, r)
	case "reloader":
		fmt.Fprint(w, statisticsJSON(server, tube))
	default:
		fmt.Fprint(w, tplStatistic(server, tube))
	}
}
