package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"
)

const Version = 2.2

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
		path = filepath.Join(selfDir, "beanstalkd-ui.toml")
	}

	if *verPtr {
		fmt.Printf("beanstalkd-ui version: %.1f\n", Version)
		os.Exit(0)
	}
	if *helpPtr {
		fmt.Printf("beanstalkd-ui version: %.1f\nUsage: beanstalkd-ui [OPTIONS]\n  -c <filename>   Use config file (default: beanstalkd-ui.toml)\n  -h              Output this help and exit\n  -v              Output version and exit\n", Version)
		os.Exit(0)
	}
	return path
}

// Read loads the configuration from the given TOML file.
// Creates a default config file if it does not exist.
func Read(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(path, []byte(FileTemplate), 0o644); err != nil {
			return nil, err
		}
		data = []byte(FileTemplate)
	} else if err != nil {
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
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// RemoveServer removes a server address from the config.
func (c *Config) RemoveServer(server string) {
	c.Servers = slices.DeleteFunc(c.Servers, func(s string) bool {
		return s == server
	})
}
