package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

// basicAuth wraps a handler with HTTP Basic authentication.
func basicAuth(f ViewFunc) ViewFunc {
	if !pubConf.Auth.Enabled {
		return func(w http.ResponseWriter, r *http.Request) {
			f(w, r)
		}
	}
	const prefix = "Basic "
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, prefix) {
			payload, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
			if err == nil {
				pair := bytes.SplitN(payload, []byte(":"), 2)
				if len(pair) == 2 &&
					subtle.ConstantTimeCompare(pair[0], []byte(pubConf.Auth.Username)) == 1 &&
					subtle.ConstantTimeCompare(pair[1], []byte(pubConf.Auth.Password)) == 1 {
					f(w, r)
					return
				}
			}
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		w.WriteHeader(http.StatusUnauthorized)
	}
}
