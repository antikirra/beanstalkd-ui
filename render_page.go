package main

import (
	"fmt"
	"strings"
)

// pageParams holds the variable parts of a page layout.
type pageParams struct {
	title   string
	navbar  string // HTML between <ul class="nav navbar-nav"> and toolbox dropdown
	toolbox string // extra toolbox menu items (filter, clear tubes, etc.)
	search  string // optional search bar in navbar
	refresh string // refresh button ID: "autoRefresh" or "autoRefreshSummary"
	content string // main page content
	modals  string // modal dialogs and hidden copy divs
	jsURL   string // JS variable: `var url = "...";`
}

// renderPage assembles a complete HTML page from the common skeleton and variable parts.
func renderPage(conf SelfConf, p pageParams) string {
	var buf strings.Builder

	// Head.
	buf.WriteString(TplHeaderBegin)
	buf.WriteString(p.title)
	buf.WriteString(` -`)
	buf.WriteString(TplHeaderEnd)
	buf.WriteString(TplNoScript)

	// Navbar.
	buf.WriteString(`<div class="navbar navbar-fixed-top navbar-default" role="navigation"><div class="container">`)
	buf.WriteString(`<div class="navbar-header"><button type="button" class="navbar-toggle" data-toggle="collapse" data-target=".navbar-collapse">`)
	buf.WriteString(`<span class="sr-only">Toggle navigation</span><span class="icon-bar"></span><span class="icon-bar"></span><span class="icon-bar"></span>`)
	buf.WriteString(`</button><a class="navbar-brand" href="./">Beanstalkd console</a></div>`)
	buf.WriteString(`<div class="collapse navbar-collapse"><ul class="nav navbar-nav">`)
	buf.WriteString(p.navbar)
	buf.WriteString(`</ul><ul class="nav navbar-nav navbar-right">`)
	buf.WriteString(`<li class="dropdown"><a href="#" class="dropdown-toggle" data-toggle="dropdown">Toolbox <span class="caret"></span></a><ul class="dropdown-menu">`)
	buf.WriteString(p.toolbox)
	buf.WriteString(`<li class="divider"></li><li><a href="#settings" role="button" data-toggle="modal">Edit settings</a></li></ul></li>`)
	buf.WriteString(TplLinks)
	if p.search != "" {
		buf.WriteString(`</ul>`)
		buf.WriteString(p.search)
	}
	buf.WriteString(fmt.Sprintf(`<li><button type="button" id="%s" class="btn btn-default btn-small"><span class="glyphicon glyphicon-refresh"></span></button></li></ul>`, p.refresh))
	buf.WriteString(`</div></div></div>`)

	// Content.
	buf.WriteString(`<div class="container">`)
	buf.WriteString(p.content)
	buf.WriteString(p.modals)
	buf.WriteString(dropEditSettings(conf))
	buf.WriteString(checkUpdate())
	buf.WriteString(`</div>`)

	// Scripts.
	buf.WriteString(fmt.Sprintf(`<script>%s var contentType = "";</script>`, p.jsURL))
	buf.WriteString(`<script src="./assets/vendor/jquery/jquery.js"></script>`)
	buf.WriteString(`<script src="./js/jquery.color.js"></script>`)
	buf.WriteString(`<script src="./js/jquery.cookie.js"></script>`)
	buf.WriteString(`<script src="./js/jquery.regexp.js"></script>`)
	buf.WriteString(`<script src="./assets/vendor/bootstrap/js/bootstrap.min.js"></script>`)
	if !conf.DisableJobDataHighlight {
		buf.WriteString(`<script src="./highlight/highlight.pack.js"></script><script>hljs.initHighlightingOnLoad();</script>`)
	}
	buf.WriteString(`<script src="./js/customer.js"></script></body></html>`)

	return buf.String()
}

const (
	toolboxFilterServer     = `<li><a href="#filterServer" role="button" data-toggle="modal">Filter columns</a></li>`
	toolboxFilterTube       = `<li><a href="#filter" role="button" data-toggle="modal">Filter columns</a></li>`
	toolboxClearTubes       = `<li><a href="#clear-tubes" role="button" data-toggle="modal">Clear multiple tubes</a></li>`
	toolboxManageSamples    = `<li><a href="./sample?action=manageSamples" role="button">Manage samples</a></li>`
	toolboxStatsPref        = `<li><a href="./statistics?action=preference" role="button">Statistics preference</a></li>`
	getParameterByNameJS    = `function getParameterByName(name,url){if(!url){url=window.location.href}name=name.replace(/[\[\]]/g,"\\$&");var regex=new RegExp("[?&]"+name+"(=([^&#]*)|&|#|$)"),results=regex.exec(url);if(!results){return null}if(!results[2]){return""}return decodeURIComponent(results[2].replace(/\+/g," "))}`
	addServerModal          = `<div id="servers-add" class="modal fade" tabindex="-1" role="dialog"><div class="modal-dialog"><div class="modal-content"><div class="modal-header"><button type="button" class="close" data-dismiss="modal"><span aria-hidden="true">&times;</span><span class="sr-only">Close</span></button><h4 class="modal-title" id="servers-add-labal">Add Server</h4></div><div class="modal-body"><form class="form-horizontal"><div class="form-group"><label class="control-label col-sm-2" for="host">Host</label><div class="col-sm-10"><input type="text" id="host" value="localhost" class="form-control"></div></div><div class="form-group"><label class="control-label col-sm-2" for="port">Port</label><div class="col-sm-10"><input type="number" id="port" value="11300" class="form-control"></div></div></form></div><div class="modal-footer"><button class="btn btn-info">Add server</button><button class="btn" data-dismiss="modal" aria-hidden="true">Cancel</button></div></div></div></div>`
)

// tplMain renders the main server list dashboard.
func tplMain(conf SelfConf, serverList, currentServer string) string {
	return renderPage(conf, pageParams{
		title:   "All servers",
		toolbox: toolboxFilterServer + toolboxManageSamples + toolboxStatsPref,
		refresh: "autoRefreshSummary",
		content: `<div id="idServers">` + serverList + `</div>`,
		modals:  `<div id="idServersCopy" style="display:none"></div>` + addServerModal + tplServerFilter(conf),
		jsURL:   `var url = "./index?server=";`,
	})
}

// tplServer renders the server detail page with tube list.
func tplServer(conf SelfConf, content, server string) string {
	return renderPage(conf, pageParams{
		title:   server,
		navbar:  dropDownServer(conf, server) + dropDownTube(conf, server, ""),
		toolbox: toolboxFilterTube + toolboxClearTubes + toolboxManageSamples + toolboxStatsPref,
		refresh: "autoRefresh",
		content: content + modalClearTubes(conf, server) + `<div id='idAllTubesCopy' style="display:none"></div>`,
		modals:  tplTubeFilter(conf),
		jsURL:   getParameterByNameJS + ` var url="./server?server="+getParameterByName("server");`,
	})
}

// tplTube renders the tube detail page with job data.
func tplTube(conf SelfConf, content, server, tube string) string {
	return renderPage(conf, pageParams{
		title:   tube + " - " + server,
		navbar:  dropDownServer(conf, server) + dropDownTube(conf, server, tube),
		toolbox: toolboxFilterTube + toolboxManageSamples + toolboxStatsPref,
		search:  tplSearchTube(conf, server, tube, ""),
		refresh: "autoRefresh",
		content: content + modalAddJob(tube) + modalAddSample(server, tube) + `<div id="idAllTubesCopy" style="display:none"></div>`,
		modals:  tplTubeFilter(conf),
		jsURL:   getParameterByNameJS + ` var url="./tube?server="+getParameterByName("server");`,
	})
}
