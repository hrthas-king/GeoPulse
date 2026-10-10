// Package config 仅持久化显示间隔；公网 IP 和流量记录不写入设置。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hrthas-king/GeoPulse/internal/app"
)

type Config struct {
	IntervalSeconds int `json:"interval_seconds"`
}

func Path() string { return filepath.Join(os.Getenv("LOCALAPPDATA"), "GeoPulse", "config.json") }

func Load() Config {
	c := Config{IntervalSeconds: 1}
	data, err := os.ReadFile(Path())
	if err == nil {
		var saved Config
		if json.Unmarshal(data, &saved) == nil && app.ValidInterval(saved.IntervalSeconds) {
			c = saved
		}
	}
	return c
}

func Save(c Config) error {
	if !app.ValidInterval(c.IntervalSeconds) {
		return fmt.Errorf("显示间隔只能为 1、2、5 秒")
	}
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "config-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
