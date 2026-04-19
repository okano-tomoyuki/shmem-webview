package config

import (
	"encoding/json"
	"os"
)

type ServerConfig struct {
	ShmemName     string `json:"shmem_name"`
	MutexName     string `json:"mutex_name"`
	ShmemSize     int    `json:"shmem_size"`
	ShmemPollMs   int    `json:"shmem_poll_ms"`
	PollTimeoutMs int    `json:"poll_timeout_ms"`
	WsPort        int    `json:"ws_port"`
}

func Load(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg ServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
