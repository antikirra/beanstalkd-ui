package main

import (
	"html"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/aurora/beanstalk"
)

// tplStatistic renders a statistics overview graphs with Flot by given server
// and tube.
func tplStatistic(conf SelfConf, server, tube string) string {
	var buf strings.Builder
	buf.WriteString(TplHeaderBegin)
	buf.WriteString(`Statistics overview - `)
	buf.WriteString(tube)
	buf.WriteString(` -`)
	buf.WriteString(TplHeaderEnd)
	buf.WriteString(TplNoScript)
	buf.WriteString(`<div class="navbar navbar-fixed-top navbar-default" role="navigation"><div class="container"><div class="navbar-header"><button type="button" class="navbar-toggle" data-toggle="collapse" data-target=".navbar-collapse"><span class="sr-only">Toggle navigation</span><span class="icon-bar"></span><span class="icon-bar"></span><span class="icon-bar"></span></button><a class="navbar-brand" href="./">Beanstalkd console</a></div><div class="collapse navbar-collapse"><ul class="nav navbar-nav">`)
	buf.WriteString(dropDownServer(conf, ""))
	buf.WriteString(`</ul><ul class="nav navbar-nav navbar-right"><li class="dropdown"><a href="#" class="dropdown-toggle" data-toggle="dropdown">Toolbox <span class="caret"></span></a><ul class="dropdown-menu"><li><a href="#filter" role="button" data-toggle="modal">Filter columns</a></li><li><a href="./sample?action=manageSamples" role="button">Manage samples</a></li><li><a href="./statistics?action=preference" role="button">Statistics preference</a></li><li class="divider"></li><li><a href="#settings" role="button" data-toggle="modal">Edit settings</a></li></ul></li>`)
	buf.WriteString(TplLinks)
	buf.WriteString(`</ul>`)
	buf.WriteString(`</div></div></div><div class="container">`)
	buf.WriteString(`<div class="clearfix form-group"><div class="pull-left"><h4 class="text-info">Statistics overview <small>`)
	buf.WriteString(html.EscapeString(server))
	buf.WriteString(` &gt; `)
	buf.WriteString(html.EscapeString(tube))
	buf.WriteString(`</small></h4></div></div><div class="clearfix form-group" id="placeholder" style="height:32em;"></div><div class="clearfix form-group"><div class="form-group">Time between updates: <input id="updateInterval" type="number" min="1" value="1" class="form-control input-sm search-query" style="display: inline-block; width: 6em;"> seconds</div></div>`)
	buf.WriteString(modalAddJob(tube))
	buf.WriteString(modalAddSample(server, tube))
	buf.WriteString(`<div id="idAllTubesCopy" style="display:none"></div>`)
	buf.WriteString(tplTubeFilter(conf))
	buf.WriteString(dropEditSettings(conf))
	buf.WriteString(`</div><script>function getParameterByName(name,url){if(!url){url=window.location.href}name=name.replace(/[\[\]]/g,"\\$&");var regex=new RegExp("[?&]"+name+"(=([^&#]*)|&|#|$)"),results=regex.exec(url);if(!results){return null}if(!results[2]){return""}return decodeURIComponent(results[2].replace(/\+/g," "))}var url="./tube?server="+getParameterByName("server");var contentType="";</script><script src='./assets/vendor/jquery/jquery.js'></script><script src="./js/jquery.color.js"></script><script src="./js/jquery.cookie.js"></script><script src="./js/jquery.regexp.js"></script><script src="./assets/vendor/bootstrap/js/bootstrap.min.js"></script>`)
	buf.WriteString(`<script src="./js/libs/flot/jquery.flot.js"></script><script src="./js/libs/flot/jquery.flot.resize.js"></script><script src="./js/libs/flot/jquery.flot.tooltip.min.js"></script><script type="text/javascript">var options={series: {shadowSize:4,lines:{show:true},points:{show:false,radius:1}},colors:["#00C851","#ffbb33","#33b5e5","#ff4444"],grid:{hoverable:true},xaxis:{mode:"time",timeformat:"%y-%m-%d %H:%M:%S"},yaxis:{min:0,tickDecimals:0},tooltip:true,tooltipOpts:{content:"%x.1 %s jobs: %y.4"}};function getRandomData(){$.get("./statistics?action=reloader&server=`)
	buf.WriteString(server)
	buf.WriteString(`&tube=`)
	buf.WriteString(url.QueryEscape(tube))
	buf.WriteString(`",function(data){var obj={};var seriesData=[];obj=jQuery.parseJSON(data);for(var prop in obj){seriesData.push({label:prop,data:$.map(obj[prop],function(i,j){return [[new Date(Date.UTC(i[0],i[1]-1,i[2],i[3],i[4],i[5])).getTime(),i[6]]];})});}var plot=$.plot($("#placeholder"),seriesData,options);plot.setData(seriesData);plot.draw();});}var updateInterval=1;$("#updateInterval").val(updateInterval).change(function(){var v=$(this).val();if(v&&!isNaN(+v)){updateInterval=+v;if(updateInterval<1){updateInterval=1}$(this).val(""+updateInterval)}});function update(){getRandomData();setTimeout(update,updateInterval*1000)};update();</script></body></html>`)
	return buf.String()
}

// tplStatisticEdit renders the statistics preference form.
func tplStatisticEdit(conf SelfConf, alert string) string {
	var err error
	var buf, ST, tubeList strings.Builder
	statsConfigMu.RLock()
	frequency := statsConfig.Frequency
	collection := statsConfig.Collection
	statsConfigMu.RUnlock()
	if frequency < 1 {
		frequency = 300
	}
	for _, server := range conf.Servers {
		var conn *beanstalk.Conn
		tubeList.Reset()
		if conn, err = dialBeanstalk(server); err != nil {
			continue
		}
		tubes, _ := conn.ListTubes()
		sort.Strings(tubes)
		conn.Close()
		for _, v := range tubes {
			var checked string
			statisticsData.RLock()
			s, ok := statisticsData.Server[server]
			if ok {
				_, ok := s[v]
				if ok {
					checked = `checked="checked"`
				}
			}
			statisticsData.RUnlock()
			tubeList.WriteString(`<div class="control-group"><div class="controls"><label class="checkbox-inline"><input type="checkbox" name="tubes[`)
			tubeList.WriteString(server)
			tubeList.WriteString(`:`)
			tubeList.WriteString(v)
			tubeList.WriteString(`]" value="1" `)
			tubeList.WriteString(checked)
			tubeList.WriteString(`>`)
			tubeList.WriteString(v)
			tubeList.WriteString(`</label></div></div>`)
		}
		ST.WriteString(`<div class="pull-left" style="padding-right: 35px;">`)
		ST.WriteString(server)
		ST.WriteString(`<blockquote>`)
		ST.WriteString(tubeList.String())
		ST.WriteString(`</blockquote></div>`)
	}

	buf.WriteString(`<form name="statisticsPreference" action="./statistics?action=save" method="POST"><div class="clearfix form-group"><div class="pull-left"><h4 class="text-info">Statistics preference</h4></div></div><div class="form-group"><fieldset>`)
	buf.WriteString(alert)
	buf.WriteString(`<div class="control-group"><label class="control-label"><b>Collection record number of each server or tube (Default: <i>0</i>, reserved for not statistics, recommended value: <i>300</i>) *</b></label><div class="controls form-group"><input class="form-control input-sm focused" name="collection" type="number" min="0" style="width: 15em;" required="" value="`)
	buf.WriteString(strconv.Itoa(collection))
	buf.WriteString(`" autocomplete="off"></div></div><div class="control-group"><label class="control-label"><b>Acquisition frequency seconds of each server or tube (Default: <i>300</i>, minimum: <i>1</i>) *</b></label><div class="controls form-group"><input class="form-control input-sm" name="frequency" type="number" min="1" style="width: 15em;" required="" value="`)
	buf.WriteString(strconv.Itoa(frequency))
	buf.WriteString(`" autocomplete="off"></div></div></fieldset><div class="clearfix"><label class="control-label"><b>Available on tubes *</b></label><br/>`)
	buf.WriteString(ST.String())
	buf.WriteString(`</div></div><div><input type="submit" class="btn btn-success" value="Save"/></div></form>`)
	return buf.String()
}

// tplStatisticSetting renders the statistics preferences page.
func tplStatisticSetting(conf SelfConf, content string) string {
	return renderPage(conf, pageParams{
		title:   "Statistics preference",
		navbar:  dropDownServer(conf, ""),
		toolbox: toolboxManageSamples + toolboxStatsPref,
		refresh: "autoRefresh",
		content: content,
		jsURL:   `var url = "./sample";`,
	})
}
