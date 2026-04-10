package main

import (
	"bytes"
	cryptoRand "crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// readConf read external config file when program startup.
func readConf() error {
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		if err := os.WriteFile(configFile, []byte(ConfigFileTemplate), 0644); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}
	if _, err := toml.Decode(string(data), &pubConf); err != nil {
		return err
	}
	sampleJobsMu.Lock()
	err = json.Unmarshal([]byte(pubConf.Sample.Storage), &sampleJobs)
	sampleJobsMu.Unlock()
	if err != nil {
		return err
	}
	return nil
}

// compactUnique removes empty strings and deduplicates a string slice.
func compactUnique(s []string) []string {
	result := slices.DeleteFunc(s, func(v string) bool { return v == "" })
	slices.Sort(result)
	return slices.Compact(result)
}

// removeServerInConfig removes a server address from the global config.
func removeServerInConfig(server string) {
	filtered := make([]string, 0, len(pubConf.Servers))
	for _, v := range pubConf.Servers {
		if v != server {
			filtered = append(filtered, v)
		}
	}
	pubConf.Servers = filtered
}

// runCmd run command opens a new browser window pointing to url.
func runCmd(prog string, args ...string) error {
	cmd := exec.Command(prog, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}


// prettyJSON provide method get JSON string with indent.
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

// parseFlags parse flags of program.
func parseFlags() {
	configPtr := flag.String("c", "", "Use config file.")
	verPtr := flag.Bool("v", false, "Output version and exit.")
	helpPtr := flag.Bool("h", false, "Output this help and exit.")
	flag.Parse()
	if *configPtr == "" {
		selfDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			os.Exit(0)
		}
		configFile = selfDir + string(os.PathSeparator) + `aurora.toml`
	} else {
		configFile = *configPtr
	}
	if *verPtr {
		fmt.Printf("aurora version: %.1f\r\n", Version)
		os.Exit(0)
	}
	if *helpPtr {
		fmt.Printf("aurora version: %.1f\r\nCopyright (c) 2016 - 2020 Ri Xu https://xuri.me All rights reserved.\r\n\r\nUsage: aurora [OPTIONS] [cmd [arg ...]]\n  -c <filename>   Use config file. (default: aurora.toml)\r\n  -h \t\t  Output this help and exit.\r\n  -v \t\t  Output version and exit.\r\n", Version)
		os.Exit(0)
	}
}

// basicAuth provide a simple method to HTTP authenticate.
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

// randToken generate a cryptographically secure random token.
func randToken() string {
	b := make([]byte, 16)
	_, _ = cryptoRand.Read(b)
	return fmt.Sprintf("%x", b)
}

// setHeader provide common method set HTTP header response.
func setHeader(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Server", "WebServer")
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "no-cache, private, max-age=0")
	w.Header().Set("Expires", time.Unix(0, 0).Format(http.TimeFormat))
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Accel-Expires", "0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
}

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
