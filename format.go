package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"html"
)

// prettyJSON returns the JSON input pretty-printed with indentation.
func prettyJSON(b []byte) []byte {
	var out bytes.Buffer
	err := json.Indent(&out, b, "", "\t")
	if err != nil {
		return b
	}
	return out.Bytes()
}

// base64Decode attempts to decode a base64 string, returning the original on failure.
func base64Decode(s string) string {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return s
	}
	return string(data)
}

// preformat formats a job body for HTML display based on user preferences.
func preformat(conf SelfConf, jobBody []byte) string {
	job := string(jobBody)
	if !conf.DisableJSONDecode {
		job = string(prettyJSON(jobBody))
	}
	if conf.EnableBase64Decode {
		job = base64Decode(job)
	}
	return html.EscapeString(job)
}
