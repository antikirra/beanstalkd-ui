package main

import (
	"fmt"
	"net/url"

	"github.com/xuri/aurora/beanstalk"
)

// tubeActionURL returns a URL for a tube action with the given parameters.
func tubeActionURL(server, tube, action string) string {
	return fmt.Sprintf("?server=%s&tube=%s&action=%s", server, url.QueryEscape(tube), action)
}

// tubeURL returns a URL to the tube detail page.
func tubeURL(server, tube string) string {
	return fmt.Sprintf("?server=%s&tube=%s", server, url.QueryEscape(tube))
}

// serverURL returns a URL to the server detail page.
func serverURL(server string) string {
	return fmt.Sprintf("./server?server=%s", server)
}

// listTubesSorted returns sorted tube names for a server, or nil on error.
func listTubesSorted(server string) []string {
	conn, err := dialBeanstalk(server)
	if err != nil {
		return nil
	}
	defer conn.Close()
	tubes, _ := conn.ListTubes()
	return tubes
}

// tubeStats returns stats for a single tube, or nil on error.
func tubeStats(conn *beanstalk.Conn, tube string) map[string]string {
	stats, err := newTube(conn, tube).Stats()
	if err != nil {
		return nil
	}
	return stats
}
