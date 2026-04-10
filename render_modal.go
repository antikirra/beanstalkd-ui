package main

import (
	"fmt"
	"html"
	"strings"
)

// modalAddJob renders the modal dialog for adding a new job to a tube.
func modalAddJob(tube string) string {
	return fmt.Sprintf(
		`<div class="modal fade" id="modalAddJob" tabindex="-1"><div class="modal-dialog"><div class="modal-content">`+
			`<div class="modal-header"><button type="button" class="close" data-dismiss="modal">×</button><h4 class="modal-title">Add new job</h4></div>`+
			`<div class="modal-body"><form class="form-horizontal"><fieldset>`+
			`<div class="alert alert-danger" id="tubeSaveAlert" style="display: none;"><button type="button" class="close" onclick="$('#tubeSaveAlert').fadeOut('fast');">×</button><strong>Error!</strong> Required fields are marked * </div>`+
			`<div class="form-group"><label class="control-label col-xs-3">*Tube name</label><div class="col-xs-9"><input class="form-control focused" id="tubeName" type="text" value="%s"></div></div>`+
			`<div class="form-group"><label class="control-label col-xs-3">*Data</label><div class="col-xs-9"><textarea id="tubeData" rows="3" class="form-control"></textarea></div></div>`+
			`<div class="form-group"><label class="control-label col-xs-3">Priority</label><div class="col-xs-9"><input class="form-control focused" id="tubePriority" type="number" value="%d"></div></div>`+
			`<div class="form-group"><label class="control-label col-xs-3">Delay</label><div class="col-xs-9"><input class="form-control focused" id="tubeDelay" type="number" value="%d"></div></div>`+
			`<div class="form-group"><label class="control-label col-xs-3">TTR</label><div class="col-xs-9"><input class="form-control focused" id="tubeTtr" type="number" value="%d"></div></div>`+
			`<div class="modal-footer"><a href="#" class="btn" data-dismiss="modal">Close</a><a href="#" class="btn btn-success" id="tubeSave">Save changes</a></div>`+
			`</fieldset></form></div></div></div></div>`,
		tube, DefaultPriority, DefaultDelay, DefaultTTR)
}

// modalAddSample renders the modal dialog for adding a job to sample collection.
func modalAddSample(server, tube string) string {
	tubes := listTubesSorted(server)
	if tubes == nil {
		return ""
	}

	var tubeList strings.Builder
	for _, t := range tubes {
		checked := ""
		if t == tube {
			checked = ` checked="checked"`
		}
		fmt.Fprintf(&tubeList,
			`<div class="form-group"><div class="checkbox"><label>`+
				`<input type="checkbox" name="tubes[%s]" value="1"%s>%s`+
				`</label></div></div>`,
			t, checked, html.EscapeString(t))
	}

	return fmt.Sprintf(
		`<div id="modalAddSample" class="modal fade" tabindex="-1" role="dialog" aria-labelledby="addsamples-label" aria-hidden="true">`+
			`<div class="modal-dialog"><div class="modal-content">`+
			`<div class="modal-header"><button type="button" class="close" data-dismiss="modal" aria-hidden="true">×</button>`+
			`<h4 id="addsamples-label" class="modal-title">Add to samples</h4></div>`+
			`<div class="modal-body"><input type="hidden" name="tube" value="%s"/>`+
			`<fieldset><div class="alert alert-danger" id="sampleSaveAlert" style="display: none;">`+
			`<button type="button" class="close" onclick="$('#sampleSaveAlert').fadeOut('fast');">×</button>`+
			`<span><strong>Error!</strong> Required fields are marked *</span></div>`+
			`<input type="hidden" name="addsamplejobid" id="addsamplejobid">`+
			`<div class="form-group"><label for="addsamplename" title="You can highlight text inside the job, then hit the Add button, it will be automatically populated here.">`+
			`<b>Name *</b><i>(highlighted text is auto populated)</i></label>`+
			`<input class="form-control focused" id="addsamplename" name="addsamplename" type="text" value="" autocomplete="off"></div></fieldset>`+
			`<div><label class="control-label"><b>Available on tubes *</b></label>%s</div></div>`+
			`<div class="modal-footer"><button class="btn" data-dismiss="modal" aria-hidden="true">Close</button>`+
			`<a href="#" class="btn btn-success" id="sampleSave">Save</a></div></div></div></div>`,
		tube, tubeList.String())
}

// modalClearTubes renders the modal dialog for clearing jobs from multiple tubes.
func modalClearTubes(conf SelfConf, server string) string {
	tubes := listTubesSorted(server)
	if tubes == nil {
		return ""
	}

	var tubeList strings.Builder
	for _, t := range tubes {
		fmt.Fprintf(&tubeList,
			`<div class="checkbox"><label><input type="checkbox" name="%s" value="1"><b>%s</b></label></div>`,
			t, html.EscapeString(t))
	}

	return fmt.Sprintf(
		`<div class="modal fade" id="clear-tubes" data-cookie="tubefilter" tabindex="-1" role="dialog" aria-labelledby="clear-tubes-label" aria-hidden="true">`+
			`<div class="modal-dialog"><div class="modal-content">`+
			`<div class="modal-header"><button type="button" class="close" data-dismiss="modal"><span aria-hidden="true">&times;</span><span class="sr-only">Close</span></button>`+
			`<h4 class="modal-title" id="clear-tubes-label">Clear multiple tubes</h4></div>`+
			`<div class="modal-body"><form><fieldset><div class="form-group">`+
			`<label>Tube name <small class="text-muted">(supports <a href="http://james.padolsey.com/javascript/regex-selector-for-jquery/" target="_blank">jQuery regexp</a> syntax)</small></label>`+
			`<div class="input-group"><input class="form-control focused" id="tubeSelector" type="text" placeholder="prefix*" value="%s">`+
			`<div class="input-group-btn"><a href="javascript:void(0);" class="btn btn-info" id="clearTubesSelect">Select</a></div></div></div></fieldset>`+
			`<div><strong>Tube list</strong>%s</div></form></div>`+
			`<div class="modal-footer"><button type="button" class="btn btn-default" data-dismiss="modal">Close</button>`+
			`<a href="#" class="btn btn-success" id="clearTubes">Clear selected tubes</a><br/><br/>`+
			`<p class="text-muted text-right small">* Tube clear works by peeking to all jobs and deleting them in a loop.</p></div></div></div></div>`,
		conf.TubeSelector, tubeList.String())
}
