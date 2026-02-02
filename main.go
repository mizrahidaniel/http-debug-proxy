package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type CapturedRequest struct {
	ID        int                 `json:"id"`
	Method    string              `json:"method"`
	URL       string              `json:"url"`
	Headers   map[string][]string `json:"headers"`
	Body      string              `json:"body"`
	Timestamp time.Time           `json:"timestamp"`
	Response  *CapturedResponse   `json:"response,omitempty"`
}

type CapturedResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Body    string              `json:"body"`
	Latency string              `json:"latency"`
}

var (
	requests     []CapturedRequest
	requestMutex sync.RWMutex
	requestID    int
	
	// WebSocket management
	clients      = make(map[*websocket.Conn]bool)
	clientsMutex sync.RWMutex
	broadcast    = make(chan CapturedRequest, 100)
	upgrader     = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for development
		},
	}
	
	// Server config
	serverPort int
)

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	target := flag.String("target", "", "Target URL to proxy requests to")
	flag.Parse()

	if *target == "" {
		log.Fatal("--target is required (e.g., --target=https://api.example.com)")
	}

	targetURL, err := url.Parse(*target)
	if err != nil {
		log.Fatalf("Invalid target URL: %v", err)
	}

	// Set global server port
	serverPort = *port

	// Start WebSocket broadcaster
	go handleBroadcasts()

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	
	// Custom transport to capture responses
	originalTransport := http.DefaultTransport
	proxy.Transport = &captureTransport{
		Transport: originalTransport,
		Target:    targetURL,
	}

	// WebSocket endpoint
	http.HandleFunc("/ws", handleWebSocket)
	
	// API endpoints
	http.HandleFunc("/api/requests", handleAPIRequests)
	http.HandleFunc("/api/clear", handleAPIClear)
	
	// Debug UI endpoints (legacy)
	http.HandleFunc("/__debug/requests", handleDebugRequests)
	http.HandleFunc("/__debug/clear", handleDebugClear)
	
	// Proxy handler (catch-all)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Don't proxy WebSocket or API routes
		if r.URL.Path == "/ws" || r.URL.Path == "/api/requests" || 
		   r.URL.Path == "/api/clear" || r.URL.Path == "/__debug/requests" || 
		   r.URL.Path == "/__debug/clear" {
			http.NotFound(w, r)
			return
		}
		
		start := time.Now()
		
		// Capture request
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		
		captured := CapturedRequest{
			ID:        getNextID(),
			Method:    r.Method,
			URL:       r.URL.String(),
			Headers:   r.Header.Clone(),
			Body:      string(bodyBytes),
			Timestamp: start,
		}
		
		// Create response recorder
		rec := &responseRecorder{
			ResponseWriter: w,
			statusCode:     200,
			body:           new(bytes.Buffer),
		}
		
		// Proxy the request
		proxy.ServeHTTP(rec, r)
		
		// Capture response
		captured.Response = &CapturedResponse{
			Status:  rec.statusCode,
			Headers: rec.Header().Clone(),
			Body:    rec.body.String(),
			Latency: time.Since(start).String(),
		}
		
		storeRequest(captured)
		
		// Broadcast to WebSocket clients
		select {
		case broadcast <- captured:
		default:
			// Channel full, skip this broadcast
		}
		
		log.Printf("[%d] %s %s -> %d (%s)", captured.ID, captured.Method, captured.URL, rec.statusCode, captured.Response.Latency)
	})

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("🔍 HTTP Debug Proxy running on http://localhost%s\n", addr)
	fmt.Printf("   Proxying to: %s\n", *target)
	fmt.Printf("   Debug UI: http://localhost%s/__debug/requests\n", addr)
	fmt.Printf("   WebSocket: ws://localhost%s/ws\n", addr)
	fmt.Printf("   API: http://localhost%s/api/requests\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// WebSocket handler
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()
	
	// Register client
	clientsMutex.Lock()
	clients[conn] = true
	clientCount := len(clients)
	clientsMutex.Unlock()
	
	log.Printf("WebSocket client connected (total: %d)", clientCount)
	
	// Send existing requests history
	requestMutex.RLock()
	for _, req := range requests {
		if err := conn.WriteJSON(req); err != nil {
			log.Printf("Error sending history: %v", err)
			break
		}
	}
	requestMutex.RUnlock()
	
	// Keep connection alive and handle disconnects
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			clientsMutex.Lock()
			delete(clients, conn)
			clientCount := len(clients)
			clientsMutex.Unlock()
			log.Printf("WebSocket client disconnected (remaining: %d)", clientCount)
			break
		}
	}
}

// Broadcast handler
func handleBroadcasts() {
	for req := range broadcast {
		clientsMutex.RLock()
		for client := range clients {
			err := client.WriteJSON(req)
			if err != nil {
				log.Printf("WebSocket write error: %v", err)
				client.Close()
				clientsMutex.RUnlock()
				clientsMutex.Lock()
				delete(clients, client)
				clientsMutex.Unlock()
				clientsMutex.RLock()
			}
		}
		clientsMutex.RUnlock()
	}
}

// API: Get all requests as JSON
func handleAPIRequests(w http.ResponseWriter, r *http.Request) {
	requestMutex.RLock()
	defer requestMutex.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"requests": requests,
		"count":    len(requests),
	})
}

// API: Clear all requests
func handleAPIClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	requestMutex.Lock()
	requests = []CapturedRequest{}
	requestID = 0
	requestMutex.Unlock()
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "cleared",
	})
}

type captureTransport struct {
	Transport http.RoundTripper
	Target    *url.URL
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite request to target
	req.URL.Scheme = t.Target.Scheme
	req.URL.Host = t.Target.Host
	req.Host = t.Target.Host
	
	return t.Transport.RoundTrip(req)
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func getNextID() int {
	requestMutex.Lock()
	defer requestMutex.Unlock()
	requestID++
	return requestID
}

func storeRequest(req CapturedRequest) {
	requestMutex.Lock()
	defer requestMutex.Unlock()
	requests = append(requests, req)
	
	// Keep only last 100 requests
	if len(requests) > 100 {
		requests = requests[len(requests)-100:]
	}
}

func handleDebugRequests(w http.ResponseWriter, r *http.Request) {
	requestMutex.RLock()
	defer requestMutex.RUnlock()
	
	if r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"requests": requests,
			"count":    len(requests),
		})
		return
	}
	
	// Simple HTML UI with WebSocket support
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>HTTP Debug Proxy</title>
<style>
body { font-family: monospace; margin: 20px; background: #1e1e1e; color: #d4d4d4; }
h1 { color: #4ec9b0; }
.status-indicator { display: inline-block; width: 10px; height: 10px; border-radius: 50%%; margin-right: 5px; }
.ws-connected { background: #4ec9b0; }
.ws-disconnected { background: #f48771; }
.request { border: 1px solid #444; margin: 10px 0; padding: 10px; border-radius: 4px; animation: fadeIn 0.3s; }
@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
.method { font-weight: bold; color: #dcdcaa; }
.url { color: #9cdcfe; }
.status { font-weight: bold; }
.status-2xx { color: #4ec9b0; }
.status-4xx { color: #ce9178; }
.status-5xx { color: #f48771; }
.body { background: #2d2d2d; padding: 10px; margin: 5px 0; border-radius: 4px; white-space: pre-wrap; overflow-x: auto; }
</style>
</head>
<body>
<h1>🔍 HTTP Debug Proxy</h1>
<p>
  <span class="status-indicator ws-disconnected" id="ws-status"></span>
  <span id="ws-text">Connecting to WebSocket...</span> |
  <span id="request-count">0</span> requests captured |
  <a href="/__debug/clear" style="color: #569cd6;">Clear</a>
</p>
<div id="requests"></div>

<script>
const ws = new WebSocket('ws://localhost:%d/ws');
const requestsDiv = document.getElementById('requests');
const wsStatus = document.getElementById('ws-status');
const wsText = document.getElementById('ws-text');
const countSpan = document.getElementById('request-count');
let requestCount = 0;

ws.onopen = () => {
  wsStatus.className = 'status-indicator ws-connected';
  wsText.textContent = 'Live updates enabled';
  console.log('WebSocket connected');
};

ws.onmessage = (event) => {
  const req = JSON.parse(event.data);
  requestCount++;
  countSpan.textContent = requestCount;
  
  const statusClass = req.response && req.response.status >= 200 && req.response.status < 300 ? 'status-2xx' :
                      req.response && req.response.status >= 400 && req.response.status < 500 ? 'status-4xx' :
                      req.response && req.response.status >= 500 ? 'status-5xx' : 'status';
  
  const reqDiv = document.createElement('div');
  reqDiv.className = 'request';
  reqDiv.innerHTML = '<div><span class="method">' + req.method + '</span> <span class="url">' + req.url + '</span></div>' +
    '<div>Time: ' + new Date(req.timestamp).toLocaleTimeString() + '</div>';
  
  if (req.response) {
    reqDiv.innerHTML += '<div>Response: <span class="' + statusClass + '">' + req.response.status + '</span> (in ' + req.response.latency + ')</div>';
    
    if (req.body) {
      reqDiv.innerHTML += '<details><summary>Request Body</summary><div class="body">' + req.body + '</div></details>';
    }
    if (req.response.body) {
      reqDiv.innerHTML += '<details><summary>Response Body</summary><div class="body">' + req.response.body + '</div></details>';
    }
  }
  
  // Prepend new requests to top
  requestsDiv.insertBefore(reqDiv, requestsDiv.firstChild);
  
  // Keep only last 50 in DOM
  while (requestsDiv.children.length > 50) {
    requestsDiv.removeChild(requestsDiv.lastChild);
  }
};

ws.onerror = (error) => {
  wsStatus.className = 'status-indicator ws-disconnected';
  wsText.textContent = 'Connection error';
  console.error('WebSocket error:', error);
};

ws.onclose = () => {
  wsStatus.className = 'status-indicator ws-disconnected';
  wsText.textContent = 'Connection closed';
  console.log('WebSocket closed');
};
</script>
</body>
</html>`, serverPort)
}

func handleDebugClear(w http.ResponseWriter, r *http.Request) {
	requestMutex.Lock()
	requests = []CapturedRequest{}
	requestID = 0
	requestMutex.Unlock()
	
	http.Redirect(w, r, "/__debug/requests", http.StatusSeeOther)
}
