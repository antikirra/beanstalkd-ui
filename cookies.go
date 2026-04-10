package main

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// readCookies read config property storage in cookie.
func readCookies(r *http.Request) {
	var servers, filters, tubeFilters []string
	var validServers []string
	var tubeSelectorValue string
	// Read servers in cookies
	beansServers, err := r.Cookie("beansServers")
	if err == nil {
		beansServersValue, _ := url.QueryUnescape(beansServers.Value)
		servers = strings.Split(beansServersValue, `;`)
	}
	// Read Filter in cookies
	filter, err := r.Cookie("filter")
	if err == nil {
		filterValue, _ := url.QueryUnescape(filter.Value)
		filters = strings.Split(filterValue, `,`)
		filters = compactUnique(filters)
	} else {
		filters = []string{"current-connections", "current-jobs-buried", "current-jobs-delayed", "current-jobs-ready", "current-jobs-reserved", "current-jobs-urgent", "current-tubes"}
	}
	// Start from config servers, not from stale global state.
	validServers = append(validServers, pubConf.Servers...)
	for _, v := range servers {
		if isValidServer(v) {
			validServers = append(validServers, v)
		}
	}
	// Read Tube Filter in cookies
	tubeFilter, err := r.Cookie("tubefilter")
	if err == nil {
		tubeFilterValue, _ := url.QueryUnescape(tubeFilter.Value)
		tubeFilters = strings.Split(tubeFilterValue, `,`)
		tubeFilters = compactUnique(tubeFilters)
	} else {
		tubeFilters = []string{"current-jobs-urgent", "current-jobs-ready", "current-jobs-reserved", "current-jobs-delayed", "current-jobs-buried", "total-jobs"}
	}
	tubeSelector, err := r.Cookie("tubeSelector")
	if err != nil {
		tubeSelectorValue = ""
	} else {
		tubeSelectorValue = tubeSelector.Value
	}

	validServers = compactUnique(validServers)

	selfConfMu.Lock()
	selfConf.Servers = validServers
	selfConf.Filter = filters
	selfConf.TubeFilters = tubeFilters
	selfConf.DisableJSONDecode = readBoolCookie(r, "isDisabledJsonDecode")
	selfConf.DisableUnserialization = readBoolCookie(r, "isDisabledUnserialization")
	selfConf.DisableJobDataHighlight = readBoolCookie(r, "isDisabledJobDataHighlight")
	selfConf.EnableBase64Decode = readBoolCookie(r, "isEnabledBase64Decode")
	selfConf.TubePauseSeconds = readIntCookie(r, `tubePauseSeconds`, -1)
	selfConf.AutoRefreshTimeoutMs = readIntCookie(r, `autoRefreshTimeoutMs`, 500)
	selfConf.SearchResultLimit = readIntCookie(r, `searchResultLimit`, 25)
	selfConf.TubeSelector = tubeSelectorValue
	selfConfMu.Unlock()
}

// readIntCookie returns an integer cookie value, or defaultValue if absent or invalid.
func readIntCookie(r *http.Request, name string, defaultValue int) int {
	cookie, err := r.Cookie(name)
	if err != nil {
		return defaultValue
	}
	value, err := strconv.Atoi(cookie.Value)
	if err != nil {
		return defaultValue
	}
	return value
}

// readBoolCookie returns true if the cookie value is "1".
func readBoolCookie(r *http.Request, name string) bool {
	cookie, err := r.Cookie(name)
	if err != nil {
		return false
	}
	return cookie.Value == "1"
}

// removeServerInCookie removes a server from cookies and updates the response.
func removeServerInCookie(server string, w http.ResponseWriter, r *http.Request) {
	selfConfMu.Lock()
	filtered := make([]string, 0, len(selfConf.Servers))
	for _, v := range selfConf.Servers {
		if v != server {
			filtered = append(filtered, v)
		}
	}
	selfConf.Servers = filtered
	servers := selfConf.Servers
	selfConfMu.Unlock()

	var buf strings.Builder
	for _, v := range servers {
		buf.WriteString(v)
		buf.WriteByte(';')
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "beansServers",
		Value:    url.QueryEscape(buf.String()),
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
}

// isValidServer checks whether addr is a valid host:port string.
func isValidServer(addr string) bool {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return false
	}
	host, port := parts[0], parts[1]
	if _, err := url.ParseRequestURI(addr); err != nil && net.ParseIP(host) == nil {
		return false
	}
	_, err := strconv.Atoi(port)
	return err == nil
}
