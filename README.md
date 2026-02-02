# HTTP Debug Proxy

Lightweight HTTP request inspector - capture, replay, and debug API calls.

## Features

- **Zero-config proxy** - Just point it at your API
- **Request/response capture** - See exactly what's being sent/received
- **Real-time WebSocket updates** - Live streaming of requests in the browser
- **Web UI** - Clean HTML interface at `/__debug/requests`
- **JSON API** - Programmatic access to captured requests
- **Auto-cleanup** - Keeps last 100 requests

## Installation

```bash
go install github.com/mizrahidaniel/http-debug-proxy@latest
```

## Usage

```bash
# Proxy requests to an API
http-debug-proxy --target=https://api.github.com --port=8080

# Make requests through the proxy
curl http://localhost:8080/users/octocat

# View captured requests
open http://localhost:8080/__debug/requests
```

## Debug UI

Navigate to `http://localhost:PORT/__debug/requests` to see:
- All captured requests & responses
- Response times & status codes
- Request/response bodies
- Color-coded status (green=2xx, orange=4xx, red=5xx)

## JSON API

```bash
# Get all captured requests as JSON
curl http://localhost:8080/api/requests

# Clear captured requests (POST)
curl -X POST http://localhost:8080/api/clear
```

## WebSocket API

Connect to `ws://localhost:PORT/ws` for real-time request streaming:

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onmessage = (event) => {
  const request = JSON.parse(event.data);
  console.log(`${request.method} ${request.url} -> ${request.response.status}`);
};
```

**Features:**
- Broadcasts all captured requests to connected clients
- Sends full request history on connect
- Auto-reconnects on disconnect
- CORS-enabled for local development

## Use Cases

- **Webhook debugging** - Inspect incoming webhook payloads
- **API integration** - Debug third-party API calls
- **Mobile app testing** - Capture app → backend traffic
- **CI/CD debugging** - Record failed API calls in tests

## Example

```bash
# Start proxy
http-debug-proxy --target=https://jsonplaceholder.typicode.com

# Make some requests
curl http://localhost:8080/posts/1
curl http://localhost:8080/users

# View in browser
open http://localhost:8080/__debug/requests
```

## Why This?

Like ngrok's inspector, but:
- **Self-hosted** - No external service
- **Lightweight** - Single binary, no dependencies
- **Simple** - Point and shoot

## Building from Source

```bash
git clone https://github.com/mizrahidaniel/http-debug-proxy
cd http-debug-proxy
go build
./http-debug-proxy --target=https://api.example.com
```

## License

MIT
