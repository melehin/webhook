package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v3"
)

// CommandStatus represents the status of a running command
type CommandStatus struct {
	mu       sync.Mutex
	running  bool
	output   []string
	lastExec time.Time
}

// Server holds the server state
type Server struct {
	config       Config
	statuses     map[string]*CommandStatus
	statusLock   sync.RWMutex
	outputBuffer int
	lokiClient   *LokiClient
}

func main() {
	// Load configuration
	configFile := "config.yaml"
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	configData, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(configData, &config); err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	// Create server instance
	server := &Server{
		config:       config,
		statuses:     make(map[string]*CommandStatus),
		outputBuffer: config.Server.Tail.Lines,
	}

	// Initialize Loki client if enabled
	if config.Loki.Enabled {
		labels := model.LabelSet{}
		for k, v := range config.Loki.Labels {
			labels[model.LabelName(k)] = model.LabelValue(v)
		}
		// Add hook_id label which will be set per request
		labels["hook_id"] = ""

		server.lokiClient = NewLokiClient(
			config.Loki.URL,
			config.Loki.BatchWait,
			config.Loki.BatchSize,
			time.Duration(config.Loki.Timeout)*time.Second,
			labels,
		)
		go server.lokiClient.Run()
	}

	// Initialize statuses for all hooks
	for _, hook := range config.Hooks {
		server.statuses[hook.ID] = &CommandStatus{
			output: make([]string, 0, server.outputBuffer),
		}
	}

	// Set up HTTP routes
	http.HandleFunc("/hooks/", server.handleHook)
	http.HandleFunc("/tail/", server.handleTail)
	http.HandleFunc("/hooks", server.handleRoot)

	// Start server
	addr := fmt.Sprintf(":%d", config.Server.Port)
	log.Printf("Starting server on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func (c *LokiClient) Run() {
	batch := make(map[string][]lokiLogEntry) // Changed map key to string
	ticker := time.NewTicker(time.Duration(c.batchWait) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.quit:
			return
		case e := <-c.entries:
			batch[e.labelsStr] = append(batch[e.labelsStr], e)
			if len(batch) >= c.batchSize {
				c.sendBatch(batch)
				batch = make(map[string][]lokiLogEntry)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				c.sendBatch(batch)
				batch = make(map[string][]lokiLogEntry)
			}
		}
	}
}

func (c *LokiClient) sendBatch(batch map[string][]lokiLogEntry) {
	var streams []lokiStream

	log.Println(batch)

	for _, entries := range batch {
		if len(entries) == 0 {
			continue
		}

		// Use labels from first entry (they're all the same for this key)
		labels := entries[0].labels
		var values [][]string

		for _, entry := range entries {
			ns := entry.time.UnixNano()
			values = append(values, []string{
				fmt.Sprintf("%d", ns),
				entry.line,
			})
		}

		stream := lokiStream{
			Stream: make(map[string]string),
			Values: values,
		}

		for k, v := range labels {
			stream.Stream[string(k)] = string(v)
		}

		streams = append(streams, stream)
	}

	req := lokiPushRequest{
		Streams: streams,
	}

	body, err := json.Marshal(req)
	if err != nil {
		log.Printf("Error marshaling Loki request: %v", err)
		return
	}

	resp, err := c.client.Post(c.url+"/loki/api/v1/push", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Error sending to Loki: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Error response from Loki: %s: %s", resp.Status, string(body))
	}
}

func (c *LokiClient) PushLog(hookID, line string) {
	labels := c.labels.Clone()
	labels["hook_id"] = model.LabelValue(hookID)

	// Create a string representation of the labels for use as map key
	labelsStr := labels.String()

	c.entries <- lokiLogEntry{
		labelsStr: labelsStr,
		labels:    labels,
		line:      line,
		time:      time.Now(),
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/hooks" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Webhook server is running",
		"hooks":   s.config.Hooks,
	})
}

func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hookID := r.URL.Path[len("/hooks/"):]
	if hookID == "" {
		http.Error(w, "Hook ID required", http.StatusBadRequest)
		return
	}

	// Find the hook configuration
	var hookConfig *Hook
	for _, h := range s.config.Hooks {
		if h.ID == hookID {
			hookConfig = &h
			break
		}
	}

	if hookConfig == nil {
		http.Error(w, "Hook not found", http.StatusNotFound)
		return
	}

	// Get or create status for this hook
	s.statusLock.RLock()
	status, exists := s.statuses[hookID]
	s.statusLock.RUnlock()

	if !exists {
		s.statusLock.Lock()
		status = &CommandStatus{
			output: make([]string, 0, s.outputBuffer),
		}
		s.statuses[hookID] = status
		s.statusLock.Unlock()
	}

	// Check if command is already running
	status.mu.Lock()
	if status.running {
		status.mu.Unlock()
		http.Error(w, "Command is already running", http.StatusConflict)
		return
	}

	// Mark as running
	status.running = true
	status.lastExec = time.Now()
	status.mu.Unlock()

	// Start the command in a goroutine
	go s.executeCommand(hookConfig, status)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "started",
		"hook_id": hookID,
	})
}

// Modified executeCommand function with Loki support
func (s *Server) executeCommand(hook *Hook, status *CommandStatus) {
	defer func() {
		status.mu.Lock()
		status.running = false
		status.mu.Unlock()
	}()

	cmd := exec.Command("bash", "-c", hook.ExecuteCommand)
	cmd.Dir = hook.CommandWorkingDirectory

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		msg := fmt.Sprintf("Error creating stdout pipe: %v", err)
		s.addOutput(hook.ID, status, msg)
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		msg := fmt.Sprintf("Error creating stderr pipe: %v", err)
		s.addOutput(hook.ID, status, msg)
		return
	}

	if err := cmd.Start(); err != nil {
		msg := fmt.Sprintf("Error starting command: %v", err)
		s.addOutput(hook.ID, status, msg)
		return
	}

	outputReader := io.MultiReader(stdoutPipe, stderrPipe)
	buf := make([]byte, 1024)
	var lineBuf bytes.Buffer

	for {
		n, err := outputReader.Read(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				if buf[i] == '\n' {
					line := lineBuf.String()
					s.addOutput(hook.ID, status, line)
					lineBuf.Reset()
				} else {
					lineBuf.WriteByte(buf[i])
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				msg := fmt.Sprintf("Error reading output: %v", err)
				s.addOutput(hook.ID, status, msg)
			}
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		msg := fmt.Sprintf("Command finished with error: %v", err)
		s.addOutput(hook.ID, status, msg)
	} else {
		msg := "Command finished successfully"
		s.addOutput(hook.ID, status, msg)
	}
}

// Modified addOutput function with Loki support
func (s *Server) addOutput(hookID string, status *CommandStatus, line string) {
	status.mu.Lock()
	defer status.mu.Unlock()

	// Add to local buffer
	status.output = append(status.output, line)
	if len(status.output) > s.outputBuffer {
		status.output = status.output[len(status.output)-s.outputBuffer:]
	}

	// Send to Loki if enabled
	if s.lokiClient != nil {
		s.lokiClient.PushLog(hookID, line)
	}
}

func (s *Server) handleTail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hookID := r.URL.Path[len("/tail/"):]
	if hookID == "" {
		http.Error(w, "Hook ID required", http.StatusBadRequest)
		return
	}

	s.statusLock.RLock()
	status, exists := s.statuses[hookID]
	s.statusLock.RUnlock()

	if !exists {
		http.Error(w, "Hook not found", http.StatusNotFound)
		return
	}

	status.mu.Lock()
	defer status.mu.Unlock()

	response := map[string]interface{}{
		"status":    "running",
		"output":    status.output,
		"last_exec": status.lastExec.Format(time.RFC3339),
	}

	if !status.running {
		response["status"] = "stopped"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
