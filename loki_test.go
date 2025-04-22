package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func TestLokiClient(t *testing.T) {
	t.Run("push logs to loki", func(t *testing.T) {
		// Create test Loki server
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/loki/api/v1/push", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusNoContent)
		}))
		defer ts.Close()

		labels := model.LabelSet{
			"job": "test-job",
		}

		client := NewLokiClient(ts.URL, 1, 10, 5*time.Second, labels)
		go client.Run()
		defer close(client.quit)

		client.PushLog("test-hook", "test log line")
		time.Sleep(100 * time.Millisecond) // Allow time for batch to process
	})

	t.Run("handle loki errors", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("server error"))
		}))
		defer ts.Close()

		labels := model.LabelSet{
			"job": "test-job",
		}

		client := NewLokiClient(ts.URL, 1, 1, 5*time.Second, labels)
		go client.Run()
		defer close(client.quit)

		client.PushLog("test-hook", "test log line")
		time.Sleep(100 * time.Millisecond)
	})
}
