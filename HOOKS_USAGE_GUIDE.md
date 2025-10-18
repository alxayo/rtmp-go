# RTMP Server Event Hooks Usage Guide

## Overview

The RTMP server includes a comprehensive event hook system that allows you to respond to real-time server events through shell scripts, HTTP webhooks, or structured output. This enables integration with monitoring systems, automation workflows, and custom business logic.

## Table of Contents

- [Available Events](#available-events)
- [Hook Types](#hook-types)
- [Command Line Configuration](#command-line-configuration)
- [Event Data Structure](#event-data-structure)
- [Step-by-Step Usage Examples](#step-by-step-usage-examples)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

## Available Events

The following events are currently implemented and triggered by the server:

| Event Type | Description | When Triggered |
|------------|-------------|----------------|
| `connection_accept` | New client connects | After successful handshake |
| `connection_close` | Client disconnects | When connection is closed |
| `publish_start` | Stream publishing begins | After successful publish command |
| `play_start` | Stream playback begins | After successful play command |

## Hook Types

### 1. Shell Script Hooks

Execute custom shell scripts when events occur.

**Configuration Format:** `event_type=script_path`

**Script Arguments:**
1. `$1`: Event type (e.g., "connection_accept")
2. `$2`: Stream key (if applicable, empty string otherwise)
3. `$3`: JSON-encoded event data

### 2. HTTP Webhook Hooks

Send HTTP POST requests to webhook URLs when events occur.

**Configuration Format:** `event_type=webhook_url`

**Request Details:**
- Method: `POST`
- Content-Type: `application/json`
- Body: Complete event data as JSON

### 3. Structured Stdio Output

Print structured event data to stdout for processing by other tools.

**Formats:**
- `json`: One JSON object per line
- `env`: Environment variable format (KEY=value)

## Command Line Configuration

### Basic Hook Configuration

```bash
# Shell script hooks
./rtmp-server --hook-script "connection_accept=/opt/scripts/on-connect.sh"
./rtmp-server --hook-script "publish_start=/opt/scripts/on-publish.sh"

# Webhook hooks
./rtmp-server --hook-webhook "connection_accept=http://localhost:8080/hooks/connect"
./rtmp-server --hook-webhook "publish_start=http://localhost:8080/hooks/publish"

# Structured stdio output
./rtmp-server --hook-stdio-format json
./rtmp-server --hook-stdio-format env
```

### Multiple Hooks

You can register multiple hooks for the same event or different events:

```bash
./rtmp-server \
  --hook-script "connection_accept=/opt/scripts/log-connection.sh" \
  --hook-script "publish_start=/opt/scripts/notify-publish.sh" \
  --hook-webhook "connection_accept=http://api.example.com/rtmp/connect" \
  --hook-webhook "publish_start=http://api.example.com/rtmp/publish" \
  --hook-stdio-format json
```

### Hook Execution Configuration

```bash
# Set hook execution timeout (default: 30s)
./rtmp-server --hook-timeout "60s" --hook-script "publish_start=/opt/scripts/slow-script.sh"

# Set maximum concurrent hook executions (default: 10)
./rtmp-server --hook-concurrency 20 --hook-webhook "connection_accept=http://api.example.com/hooks"
```

### Combined with Server Configuration

```bash
./rtmp-server \
  --listen ":1935" \
  --log-level info \
  --hook-script "publish_start=/opt/scripts/on-publish.sh" \
  --hook-webhook "connection_accept=http://monitor.example.com/rtmp-events" \
  --hook-timeout "45s" \
  --hook-concurrency 15
```

## Event Data Structure

### Common Fields

All events include these base fields:

```json
{
  "type": "event_type",
  "timestamp": 1634567890,
  "conn_id": "c000001",
  "stream_key": "live/stream1",
  "data": { /* event-specific data */ }
}
```

### Connection Accept Event

```json
{
  "type": "connection_accept",
  "timestamp": 1634567890,
  "conn_id": "c000001",
  "stream_key": "",
  "data": {
    "client_ip": "192.168.1.100",
    "client_port": 54321,
    "server_ip": "0.0.0.0",
    "server_port": 1935
  }
}
```

### Connection Close Event

```json
{
  "type": "connection_close",
  "timestamp": 1634567890,
  "conn_id": "c000001",
  "stream_key": "",
  "data": {
    "reason": "server_shutdown"
  }
}
```

### Publish Start Event

```json
{
  "type": "publish_start",
  "timestamp": 1634567890,
  "conn_id": "c000001",
  "stream_key": "live/stream1",
  "data": {
    "app": "live",
    "publishing_name": "stream1",
    "publishing_type": "live"
  }
}
```

### Play Start Event

```json
{
  "type": "play_start",
  "timestamp": 1634567890,
  "conn_id": "c000002",
  "stream_key": "live/stream1",
  "data": {
    "app": "live"
  }
}
```

## Step-by-Step Usage Examples

### Example 1: Basic Connection Logging

**Step 1:** Create a simple logging script

```bash
# /opt/scripts/log-connections.sh
#!/bin/bash

EVENT_TYPE=$1
STREAM_KEY=$2
EVENT_DATA=$3

echo "$(date): $EVENT_TYPE - Connection ID: $(echo "$EVENT_DATA" | jq -r '.conn_id // "unknown"')" >> /var/log/rtmp-connections.log

if [[ "$EVENT_TYPE" == "connection_accept" ]]; then
    CLIENT_IP=$(echo "$EVENT_DATA" | jq -r '.data.client_ip')
    echo "  Client connected from: $CLIENT_IP" >> /var/log/rtmp-connections.log
fi
```

**Step 2:** Make the script executable

```bash
chmod +x /opt/scripts/log-connections.sh
```

**Step 3:** Start the server with the hook

```bash
./rtmp-server \
  --hook-script "connection_accept=/opt/scripts/log-connections.sh" \
  --hook-script "connection_close=/opt/scripts/log-connections.sh"
```

**Step 4:** Test with a client connection

```bash
ffmpeg -f lavfi -i testsrc=size=640x480:rate=30 -f lavfi -i sine=frequency=1000 \
  -c:v libx264 -c:a aac -f flv rtmp://localhost:1935/live/test
```

### Example 2: Webhook Integration with Monitoring Service

**Step 1:** Set up a simple webhook receiver (for testing)

```python
# webhook-receiver.py
from flask import Flask, request
import json

app = Flask(__name__)

@app.route('/rtmp-events', methods=['POST'])
def handle_rtmp_event():
    event = request.get_json()
    print(f"Received event: {event['type']} for stream: {event.get('stream_key', 'N/A')}")
    
    if event['type'] == 'publish_start':
        # Alert monitoring system
        print(f"🔴 LIVE: Stream {event['stream_key']} started publishing")
    elif event['type'] == 'connection_accept':
        client_ip = event['data']['client_ip']
        print(f"📡 New connection from {client_ip}")
    
    return {'status': 'received'}

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=8080)
```

**Step 2:** Start the webhook receiver

```bash
python3 webhook-receiver.py
```

**Step 3:** Start the RTMP server with webhook configuration

```bash
./rtmp-server \
  --hook-webhook "connection_accept=http://localhost:8080/rtmp-events" \
  --hook-webhook "publish_start=http://localhost:8080/rtmp-events" \
  --hook-webhook "connection_close=http://localhost:8080/rtmp-events"
```

**Step 4:** Test and observe webhook calls

```bash
# Publisher
ffmpeg -f lavfi -i testsrc -f lavfi -i sine -c:v libx264 -c:a aac -f flv rtmp://localhost:1935/live/webhook-test

# Viewer
ffplay rtmp://localhost:1935/live/webhook-test
```

### Example 3: Stream Management Automation

**Step 1:** Create a stream management script

```bash
# /opt/scripts/stream-manager.sh
#!/bin/bash

EVENT_TYPE=$1
STREAM_KEY=$2
EVENT_DATA=$3

STREAM_REGISTRY="/var/lib/rtmp/streams.txt"

case "$EVENT_TYPE" in
    "publish_start")
        echo "$(date '+%Y-%m-%d %H:%M:%S') PUBLISH_START $STREAM_KEY" >> "$STREAM_REGISTRY"
        
        # Send notification to Slack
        curl -X POST -H 'Content-type: application/json' \
          --data "{\"text\":\"🔴 Stream $STREAM_KEY started publishing\"}" \
          "$SLACK_WEBHOOK_URL"
        
        # Start recording if it's a premium stream
        if [[ "$STREAM_KEY" == *"/premium/"* ]]; then
            echo "Starting recording for premium stream: $STREAM_KEY"
            # Add recording logic here
        fi
        ;;
        
    "connection_accept")
        CLIENT_IP=$(echo "$EVENT_DATA" | jq -r '.data.client_ip')
        echo "$(date '+%Y-%m-%d %H:%M:%S') CONNECTION $CLIENT_IP" >> "$STREAM_REGISTRY"
        
        # Check if IP is in allowlist for restricted streams
        if [[ "$CLIENT_IP" != "192.168.1."* ]] && [[ "$CLIENT_IP" != "10.0.0."* ]]; then
            echo "External connection from $CLIENT_IP - alerting security team"
            # Add security alerting logic here
        fi
        ;;
        
    "connection_close")
        echo "$(date '+%Y-%m-%d %H:%M:%S') DISCONNECT" >> "$STREAM_REGISTRY"
        ;;
esac
```

**Step 2:** Set up the environment

```bash
mkdir -p /var/lib/rtmp
chmod +x /opt/scripts/stream-manager.sh
export SLACK_WEBHOOK_URL="https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK"
```

**Step 3:** Start the server

```bash
./rtmp-server \
  --hook-script "publish_start=/opt/scripts/stream-manager.sh" \
  --hook-script "connection_accept=/opt/scripts/stream-manager.sh" \
  --hook-script "connection_close=/opt/scripts/stream-manager.sh"
```

### Example 4: JSON Output Processing with jq

**Step 1:** Start the server with JSON stdio output

```bash
./rtmp-server --hook-stdio-format json > rtmp-events.log 2>&1 &
```

**Step 2:** Process events in real-time

```bash
# Monitor live connections
tail -f rtmp-events.log | jq -r 'select(.type == "connection_accept") | "New connection: \(.data.client_ip):\(.data.client_port)"'

# Monitor stream publications
tail -f rtmp-events.log | jq -r 'select(.type == "publish_start") | "Stream started: \(.stream_key) from connection \(.conn_id)"'

# Count events by type
tail -f rtmp-events.log | jq -r '.type' | sort | uniq -c

# Extract client IPs
tail -f rtmp-events.log | jq -r 'select(.type == "connection_accept") | .data.client_ip' | sort | uniq
```

**Step 3:** Create a dashboard script

```bash
#!/bin/bash
# rtmp-dashboard.sh

echo "RTMP Server Event Dashboard"
echo "=========================="

echo "Recent Connections (last 10):"
tail -n 100 rtmp-events.log | jq -r 'select(.type == "connection_accept") | "\(.timestamp | strftime("%H:%M:%S")) \(.data.client_ip)"' | tail -10

echo ""
echo "Active Streams:"
tail -n 100 rtmp-events.log | jq -r 'select(.type == "publish_start") | .stream_key' | sort | uniq

echo ""
echo "Event Summary (last hour):"
tail -n 1000 rtmp-events.log | jq -r '.type' | sort | uniq -c
```

## Best Practices

### 1. Error Handling in Scripts

Always include error handling in your hook scripts:

```bash
#!/bin/bash
set -euo pipefail  # Exit on error, undefined vars, pipe failures

EVENT_TYPE=$1
STREAM_KEY=$2
EVENT_DATA=$3

# Validate inputs
if [[ -z "$EVENT_TYPE" ]]; then
    echo "ERROR: No event type provided" >&2
    exit 1
fi

# Use timeout for external calls
timeout 30s curl -X POST "http://api.example.com/webhook" -d "$EVENT_DATA" || {
    echo "ERROR: Webhook call failed or timed out" >&2
    exit 1
}
```

### 2. Logging and Debugging

Enable verbose logging during development:

```bash
# Add debug logging to your scripts
exec > >(logger -t rtmp-hook) 2>&1
echo "Processing event: $EVENT_TYPE for stream: $STREAM_KEY"
```

### 3. Performance Considerations

- Keep hook scripts lightweight and fast
- Use background processing for heavy operations:

```bash
# Process heavy tasks in background
{
    # Heavy processing here
    process_analytics "$EVENT_DATA"
} &
```

### 4. Security

- Validate webhook URLs before using them
- Use HTTPS for webhook endpoints
- Sanitize data before logging or processing

```bash
# Sanitize stream key before logging
SAFE_STREAM_KEY=$(echo "$STREAM_KEY" | sed 's/[^a-zA-Z0-9/_-]//g')
```

### 5. Configuration Management

Use configuration files for complex setups:

```bash
# config/hooks.conf
WEBHOOK_BASE_URL="https://api.example.com"
LOG_DIR="/var/log/rtmp"
ALERT_EMAIL="admin@example.com"

# Source in your scripts
source /opt/rtmp/config/hooks.conf
```

## Troubleshooting

### Common Issues

1. **Hook script not executing**
   - Check file permissions: `chmod +x script.sh`
   - Verify the script path is absolute
   - Check the script shebang line (`#!/bin/bash`)

2. **Webhook not receiving events**
   - Verify the URL is accessible: `curl -X POST http://your-webhook-url`
   - Check firewall rules
   - Monitor server logs for HTTP errors

3. **Timeout errors**
   - Increase timeout: `--hook-timeout "60s"`
   - Optimize script performance
   - Use asynchronous processing for slow operations

4. **Too many concurrent executions**
   - Increase concurrency limit: `--hook-concurrency 20`
   - Optimize hook script performance
   - Consider batching events

### Debug Mode

Enable debug logging to troubleshoot hook issues:

```bash
./rtmp-server --log-level debug --hook-script "connection_accept=/opt/scripts/debug.sh"
```

### Testing Hooks

Test your hooks independently:

```bash
# Test script directly
/opt/scripts/your-hook.sh "connection_accept" "live/test" '{"data":{"client_ip":"127.0.0.1"}}'

# Test webhook endpoint
curl -X POST -H "Content-Type: application/json" \
  -d '{"type":"test","timestamp":1634567890,"data":{}}' \
  http://localhost:8080/your-webhook
```

## Monitoring Hook Performance

Monitor hook execution metrics in the server logs:

```bash
# Look for hook-related log entries
tail -f server.log | grep -i hook

# Monitor hook execution times
tail -f server.log | grep "Hook execution" | jq '.duration_ms'
```

## Next Steps

This covers the current Phase 1 implementation of the event hook system. The foundation is in place for future enhancements including:

- Additional event types (codec detection, stream metadata, error events)
- Advanced filtering and authentication features
- Retry logic and delivery guarantees
- Integration with enterprise monitoring and analytics platforms

For more advanced use cases and enterprise features, refer to the roadmap in the main project documentation.