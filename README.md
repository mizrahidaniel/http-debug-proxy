# HTTP Debug Proxy

Lightweight HTTP proxy for request/response inspection, modification, and debugging.

## The Problem

When developing APIs or integrating with third-party services, you need to:
- See exactly what requests your app is sending
- Inspect response headers and bodies
- Modify requests/responses on the fly for testing
- Debug webhook payloads
- Test error scenarios without touching production

**Existing tools** (mitmproxy, Charles, Burp) are heavyweight, complex, or require GUI.

## What This Is

Single-binary HTTP proxy that logs everything passing through it:

```bash
# Start proxy on :8888
http-debug-proxy

# Route traffic through it
export HTTP_PROXY=http://localhost:8888

# Or point your app to it
curl --proxy http://localhost:8888 https://api.example.com/users
```

## Features

### Core (MVP)
- ✅ HTTP/HTTPS proxy with automatic HTTPS interception
- ✅ Request/response logging to console (pretty-printed)
- ✅ JSON syntax highlighting
- ✅ Request filtering (by host, path, method)
- ✅ Export logs to JSON/HAR format

### Advanced
- 🔄 Request/response modification rules
- 🔄 Replay saved requests
- 🔄 Mock responses (respond without hitting upstream)
- 🔄 Performance metrics (timing, size, status codes)
- 🔄 Web UI for browsing logs

## Use Cases

**API Development:**
```bash
# Debug your own API calls
http-debug-proxy --filter="api.myapp.com"
```

**Webhook Debugging:**
```bash
# Inspect webhook payloads from Stripe, GitHub, etc.
http-debug-proxy --save-to=webhooks.json
```

**Integration Testing:**
```bash
# Mock third-party API responses
http-debug-proxy --mock="api.stripe.com:./mocks/stripe.json"
```

## Status

🚧 **In Development** - MVP proxy server with logging coming first

## Why Not X?

**vs mitmproxy:** Simpler, focused on quick debugging, no Python/GUI needed  
**vs Charles Proxy:** Free, open source, CLI-first, lightweight  
**vs Burp Suite:** Not security-focused, just debugging/logging  
**vs ngrok inspect:** Works locally, no external service, more control

## Tech Stack

- **Language:** Go (fast, single binary, great HTTP/2 support)
- **HTTP Proxy:** net/http reverse proxy
- **HTTPS:** Dynamic cert generation (similar to mitmproxy)
- **Output:** JSON, HAR, or pretty console logs

## Development

```bash
# Clone
git clone https://github.com/mizrahidaniel/http-debug-proxy
cd http-debug-proxy

# Build
go build -o http-debug-proxy ./cmd/proxy

# Run
./http-debug-proxy
```

## License

MIT
