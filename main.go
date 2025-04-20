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

	"gopkg.in/yaml.v3"
)

// Config represents the YAML configuration structure
type Config struct {
	Server struct {
		Port int `yaml:"port"`
		Tail struct {
			Lines int `yaml:"lines"`
		} `yaml:"tail"`
	} `yaml:"server"`
	Hooks []Hook `yaml:"hooks"`
}

// Hook represents a single hook configuration
type Hook struct {
	ID                      string `yaml:"id"`
	ExecuteCommand          string `yaml:"execute-command"`
	CommandWorkingDirectory string `yaml:"command-working-directory"`
}

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

	// Initialize statuses for all hooks
	for _, hook := range config.Hooks {
		server.statuses[hook.ID] = &CommandStatus{
			output: make([]string, 0, server.outputBuffer),
		}
	}

	// Set up HTTP routes
	http.HandleFunc("/hooks/", server.handleHook)
	http.HandleFunc("/tail/", server.handleTail)
	http.HandleFunc("/", server.handleRoot)

	// Start server
	addr := fmt.Sprintf(":%d", config.Server.Port)
	log.Printf("Starting server on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
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

func (s *Server) executeCommand(hook *Hook, status *CommandStatus) {
	defer func() {
		status.mu.Lock()
		status.running = false
		status.mu.Unlock()
	}()

	cmd := exec.Command("bash", "-c", hook.ExecuteCommand)
	cmd.Dir = hook.CommandWorkingDirectory

	// Create pipes for stdout and stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		s.addOutput(status, fmt.Sprintf("Error creating stdout pipe: %v", err))
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		s.addOutput(status, fmt.Sprintf("Error creating stderr pipe: %v", err))
		return
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		s.addOutput(status, fmt.Sprintf("Error starting command: %v", err))
		return
	}

	// Create multi-reader for stdout and stderr
	outputReader := io.MultiReader(stdoutPipe, stderrPipe)

	// Read output line by line
	buf := make([]byte, 1024)
	var lineBuf bytes.Buffer

	for {
		n, err := outputReader.Read(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				if buf[i] == '\n' {
					// Complete line found
					s.addOutput(status, lineBuf.String())
					lineBuf.Reset()
				} else {
					lineBuf.WriteByte(buf[i])
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				s.addOutput(status, fmt.Sprintf("Error reading output: %v", err))
			}
			break
		}
	}

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		s.addOutput(status, fmt.Sprintf("Command finished with error: %v", err))
	} else {
		s.addOutput(status, "Command finished successfully")
	}
}

func (s *Server) addOutput(status *CommandStatus, line string) {
	status.mu.Lock()
	defer status.mu.Unlock()

	// Add new line to output
	status.output = append(status.output, line)

	// Trim output if it exceeds the buffer size
	if len(status.output) > s.outputBuffer {
		status.output = status.output[len(status.output)-s.outputBuffer:]
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
