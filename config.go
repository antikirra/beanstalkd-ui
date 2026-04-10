package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
