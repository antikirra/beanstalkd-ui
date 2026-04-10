package config

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	Version   = 2.2
	UpdateURL = "https://api.github.com/repos/xuri/aurora/tags"
)

const FileTemplate = `servers = []
listen = "127.0.0.1:3000"
version = 2.2

[openpage]
enabled = true

[auth]
enabled = false
password = "password"
username = "admin"

[sample]
storage = "{}"
`

// Config holds the application configuration parsed from the TOML file.
type Config struct {
	Servers  []string `toml:"servers"`
	Listen   string   `toml:"listen"`
	Version  float64  `toml:"version"`
	OpenPage struct {
		Enabled bool `toml:"enabled"`
	} `toml:"openpage"`
	Auth struct {
		Password string `toml:"password"`
		Username string `toml:"username"`
		Enabled  bool   `toml:"enabled"`
	} `toml:"auth"`
	Sample struct {
		Storage string `toml:"storage"`
	} `toml:"sample"`
}

// ParseFlags parses CLI flags and returns the config file path.
func ParseFlags() string {
	configPtr := flag.String("c", "", "Use config file.")
	verPtr := flag.Bool("v", false, "Output version and exit.")
	helpPtr := flag.Bool("h", false, "Output this help and exit.")
	flag.Parse()

	var path string
	if *configPtr != "" {
		path = *configPtr
	} else {
		selfDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			os.Exit(1)
		}
		path = filepath.Join(selfDir, "aurora.toml")
	}

	if *verPtr {
		fmt.Printf("aurora version: %.1f\n", Version)
		os.Exit(0)
	}
	if *helpPtr {
		fmt.Printf("aurora version: %.1f\nUsage: aurora [OPTIONS]\n  -c <filename>   Use config file (default: aurora.toml)\n  -h              Output this help and exit\n  -v              Output version and exit\n", Version)
		os.Exit(0)
	}
	return path
}

// Read loads the configuration from the given TOML file.
// Creates a default config file if it does not exist.
func Read(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(FileTemplate), 0644); err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save writes the configuration back to the TOML file.
func Save(path string, cfg *Config) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

// RemoveServer removes a server address from the config.
func (c *Config) RemoveServer(server string) {
	filtered := make([]string, 0, len(c.Servers))
	for _, v := range c.Servers {
		if v != server {
			filtered = append(filtered, v)
		}
	}
	c.Servers = filtered
}
