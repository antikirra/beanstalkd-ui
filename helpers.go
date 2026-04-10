package main

import (
	"fmt"
	"net/url"
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

// alertHTML returns a Bootstrap alert div with a close button.
func alertHTML(id, class, msg string) string {
	return fmt.Sprintf(
		`<div class="alert alert-%s" id="%s">`+
			`<button type="button" class="close" onclick="$('#%s').fadeOut('fast');">×</button>`+
			`<span>%s</span></div>`,
		class, id, id, msg)
}

// checkedAttr returns `checked="checked"` if condition is true, empty string otherwise.
func checkedAttr(condition bool) string {
	if condition {
		return ` checked="checked"`
	}
	return ""
}
