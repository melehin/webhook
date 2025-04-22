package main

// Config represents the YAML configuration structure
type Config struct {
	Server ConfigServer `yaml:"server"`
	Loki   ConfigLoki   `yaml:"loki"`
	Hooks  []Hook       `yaml:"hooks"`
}

type ConfigServer struct {
	Port int        `yaml:"port"`
	Tail ConfigTail `yaml:"tail"`
}

type ConfigTail struct {
	Lines int `yaml:"lines"`
}

type ConfigLoki struct {
	Enabled   bool              `yaml:"enabled"`
	URL       string            `yaml:"url"`
	BatchWait int               `yaml:"batch_wait_seconds"`
	BatchSize int               `yaml:"batch_size"`
	Timeout   int               `yaml:"timeout_seconds"`
	Labels    map[string]string `yaml:"labels"`
}

// Hook represents a single hook configuration
type Hook struct {
	ID                      string `yaml:"id"`
	ExecuteCommand          string `yaml:"execute-command"`
	CommandWorkingDirectory string `yaml:"command-working-directory"`
}
