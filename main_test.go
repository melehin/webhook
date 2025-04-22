package main

import (
	"testing"
)

func createTestConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		Server: ConfigServer{
			Port: 900,
			Tail: ConfigTail{Lines: 10},
		},
		Hooks: []Hook{
			{
				ID:                      "test-hook",
				ExecuteCommand:          "echo 'test'",
				CommandWorkingDirectory: "/tmp",
			},
		},
	}
}

func createTestServer(t *testing.T) *Server {
	t.Helper()
	config := createTestConfig(t)
	return &Server{
		config:       *config,
		statuses:     make(map[string]*CommandStatus),
		outputBuffer: config.Server.Tail.Lines,
	}
}
