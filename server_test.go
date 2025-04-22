package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHandleHook(t *testing.T) {
	server := createTestServer(t)
	hookID := "test-hook"

	t.Run("successful execution", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/hooks/"+hookID, nil)
		w := httptest.NewRecorder()

		server.handleHook(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"started"`)
	})

	t.Run("invalid method", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/hooks/"+hookID, nil)
		w := httptest.NewRecorder()

		server.handleHook(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("hook not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/hooks/invalid-hook", nil)
		w := httptest.NewRecorder()

		server.handleHook(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleTail(t *testing.T) {
	server := createTestServer(t)
	hookID := "test-hook"

	t.Run("get tail output", func(t *testing.T) {
		// First execute the hook
		execReq := httptest.NewRequest("GET", "/hooks/"+hookID, nil)
		execRec := httptest.NewRecorder()
		server.handleHook(execRec, execReq)

		// Wait a bit for command to complete
		time.Sleep(100 * time.Millisecond)

		req := httptest.NewRequest("GET", "/tail/"+hookID, nil)
		w := httptest.NewRecorder()

		server.handleTail(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"stopped"`)
		assert.Contains(t, w.Body.String(), "test")
	})
}

func TestExecuteCommand(t *testing.T) {
	server := createTestServer(t)
	hook := &Hook{
		ID:                      "test-cmd",
		ExecuteCommand:          "echo 'test output'",
		CommandWorkingDirectory: "/tmp",
	}
	status := &CommandStatus{
		output: make([]string, 0, server.outputBuffer),
	}

	server.executeCommand(hook, status)

	status.mu.Lock()
	defer status.mu.Unlock()

	assert.False(t, status.running)
	assert.Greater(t, len(status.output), 0)
	assert.Contains(t, strings.Join(status.output, "\n"), "test output")
}
