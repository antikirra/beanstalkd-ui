package main

import (
	"fmt"
	"html"
	"strings"
)

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
