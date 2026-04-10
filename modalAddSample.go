package main

import (
	"fmt"
	"html"
	"strings"
)

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
