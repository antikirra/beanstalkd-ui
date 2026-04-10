package main

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// readCookies loads per-request user preferences from cookies and returns
// a SelfConf value. This is called once per HTTP request; the result is
// passed through the call chain — never stored as a global.
func readCookies(r *http.Request) SelfConf {
	var conf SelfConf

	// Servers: start from config, add from cookie.
	conf.Servers = append(conf.Servers, pubConf.Servers...)
	if c := cookieValue(r, "beansServers"); c != "" {
		for _, v := range strings.Split(c, ";") {
			if isValidServer(v) {
				conf.Servers = append(conf.Servers, v)
			}
		}
	}
	conf.Servers = compactUnique(conf.Servers)

	// Column filters.
	if c := cookieValue(r, "filter"); c != "" {
		conf.Filter = compactUnique(strings.Split(c, ","))
	} else {
		conf.Filter = []string{"current-connections", "current-jobs-buried", "current-jobs-delayed", "current-jobs-ready", "current-jobs-reserved", "current-jobs-urgent", "current-tubes"}
	}

	if c := cookieValue(r, "tubefilter"); c != "" {
		conf.TubeFilters = compactUnique(strings.Split(c, ","))
	} else {
		conf.TubeFilters = []string{"current-jobs-urgent", "current-jobs-ready", "current-jobs-reserved", "current-jobs-delayed", "current-jobs-buried", "total-jobs"}
	}

	// Scalar preferences.
	conf.TubeSelector = rawCookieValue(r, "tubeSelector")
	conf.TubePauseSeconds = readIntCookie(r, "tubePauseSeconds", -1)
	conf.AutoRefreshTimeoutMs = readIntCookie(r, "autoRefreshTimeoutMs", 500)
	conf.SearchResultLimit = readIntCookie(r, "searchResultLimit", 25)

	// Boolean flags.
	conf.DisableJSONDecode = readBoolCookie(r, "isDisabledJsonDecode")
	conf.DisableJobDataHighlight = readBoolCookie(r, "isDisabledJobDataHighlight")
	conf.EnableBase64Decode = readBoolCookie(r, "isEnabledBase64Decode")

	return conf
}

// cookieValue returns the URL-decoded cookie value, or "" if absent.
func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	v, _ := url.QueryUnescape(c.Value)
	return v
}

// rawCookieValue returns the raw cookie value without decoding, or "" if absent.
func rawCookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// readIntCookie returns an integer cookie value, or defaultValue if absent or invalid.
func readIntCookie(r *http.Request, name string, defaultValue int) int {
	c, err := r.Cookie(name)
	if err != nil {
		return defaultValue
	}
	v, err := strconv.Atoi(c.Value)
	if err != nil {
		return defaultValue
	}
	return v
}

// readBoolCookie returns true if the cookie value is "1".
func readBoolCookie(r *http.Request, name string) bool {
	c, err := r.Cookie(name)
	if err != nil {
		return false
	}
	return c.Value == "1"
}

// removeServerInCookie removes a server from the cookie and updates the response.
func removeServerInCookie(conf SelfConf, server string, w http.ResponseWriter) {
	var buf strings.Builder
	for _, v := range conf.Servers {
		if v != server {
			buf.WriteString(v)
			buf.WriteByte(';')
		}
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
