# Webhook Command Executor with Loki Logging

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight Go service that executes commands via webhooks and streams logs to Grafana Loki in real-time.

## Features

- 🚀 Execute commands via simple HTTP webhooks
- 🔍 View last N lines of command output via API
- 📊 Real-time log streaming to Loki (port 3100)
- 🔒 Concurrent execution protection (one instance per command)
- ⚙️ YAML configuration for easy setup
- 📈 Grafana dashboard integration

## Installation

### Prerequisites

- Go 1.20+
- Grafana Loki (optional)
- Basic terminal knowledge

### Quick Start

```bash
# Clone the repository
git clone https://github.com/yourusername/webhook-executor.git
cd webhook-executor

# Build the binary
go build -o webhook-executor

# Run with default config
./webhook-executor
```

# Configuration

Edit __config.yaml__ to customize your setup:

```yaml
server:
  port: 9000
  tail:
    lines: 100  # Number of output lines to retain

loki:
  enabled: true
  url: "http://localhost:3100"  # Loki endpoint
  batch_wait_seconds: 2
  batch_size: 100
  timeout_seconds: 10
  labels:
    job: "webhook-server"
    app: "command-executor"
    environment: "production"

hooks:
  - id: redeploy-webhook
    execute-command: "/var/scripts/redeploy.sh"
    command-working-directory: "/var/webhook"
  
  - id: backup-db
    execute-command: "pg_dump -U user mydb | gzip > backup.sql.gz"
    command-working-directory: "/backups"
```

# API Endpoints

| Endpoint | Method |	Description |
|------|-----|------|
| /hooks/{hook-id} | GET | Trigger command execution |
| /tail/{hook-id} | GET | View command status and output |
| /hooks | GET | List all available hooks |

# Grafana Integration (optional)

1. __Set up Loki datasource__ in Grafana pointing to your Loki instance
2. __Import the dashboard__ from __grafana-dashboard.json__
3. __Query logs__ with:
```sql
{job="webhook-server", hook_id="redeploy-webhook"}
```

# Security Considerations

1. Run behind a reverse proxy with HTTPS
2. Add authentication middleware
3. Restrict network access to the service
4. Set appropriate file permissions for scripts

# Deployment
## Systemd Service
```ini
# /etc/systemd/system/webhook-executor.service
[Unit]
Description=Webhook Command Executor
After=network.target

[Service]
User=webhook
WorkingDirectory=/opt/webhook-executor
ExecStart=/opt/webhook-executor/webhook-executor
Restart=always

[Install]
WantedBy=multi-user.target
```

## Docker
```bash
docker build -t webhook-executor .
docker run -d -p 9000:9000 \
  -v /path/to/config.yaml:/app/config.yaml \
  -v /path/to/scripts:/scripts \
  webhook-executor
```

## Development
```bash
# Run tests
go test ./...

# Build with debug symbols
go build -gcflags="all=-N -l" -o webhook-executor-debug

# Format code
go fmt ./...
```

## Tips

1. For production use, consider adding:
   - Request rate limiting
   - JWT authentication
   - Prometheus metrics endpoint
2. Use the `command-working-directory` to properly scope your commands
3. Test your scripts independently before hooking them up
