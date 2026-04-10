package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// checkUpdate render update notice alert.
func checkUpdate() string {
	updateOnce.Do(func() {
		client := &http.Client{Timeout: 5 * time.Second}
		r, err := client.Get(UpdateURL)
		if err != nil {
			return
		}
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		u := UpdateTags{}
		if err = json.Unmarshal(body, &u); err != nil {
			return
		}
		if len(u) < 1 {
			return
		}
		v, err := strconv.ParseFloat(u[0].Name, 64)
		if err != nil {
			return
		}
		if Version < v {
			updateInfo = fmt.Sprintf(`<br/><div class="alert alert-info" style="position: relative;top:50px;"><span>You are currently running version %.1f of aurora. A new version is available: <b>%.1f</b> Get it from <b><a href="https://github.com/xuri/aurora" target="_blank">GitHub</a></b></span></div>`, Version, v)
		}
	})
	return updateInfo
}
