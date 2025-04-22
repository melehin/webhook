package main

import (
	"net/http"
	"time"

	"github.com/prometheus/common/model"
)

// Update the LokiClient struct and related methods
type LokiClient struct {
	url     string
	client  *http.Client
	entries chan lokiLogEntry
	quit    chan struct{}
	labels  model.LabelSet

	batchWait int
	batchSize int
}

func NewLokiClient(url string, batchWait, batchSize int, timeout time.Duration, labels model.LabelSet) *LokiClient {
	return &LokiClient{
		url:       url,
		client:    &http.Client{Timeout: timeout},
		entries:   make(chan lokiLogEntry, batchSize*2),
		quit:      make(chan struct{}),
		labels:    labels,
		batchWait: batchWait,
		batchSize: batchSize,
	}
}

// Loki stream structs
type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiLogEntry struct {
	labelsStr string // Changed from model.LabelSet to string representation
	labels    model.LabelSet
	line      string
	time      time.Time
}
