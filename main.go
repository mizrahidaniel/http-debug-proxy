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

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	
	// Custom transport to capture responses
	originalTransport := http.DefaultTransport
	proxy.Transport = &captureTransport{
		Transport: originalTransport,
		Target:    targetURL,
	}

	// Proxy handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__debug/requests" {
			handleDebugRequests(w, r)
			return
		}
		if r.URL.Path == "/__debug/clear" {
			handleDebugClear(w, r)
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
		
		// Store request ID in context for response capture
		r.Header.Set("X-Debug-Request-ID", fmt.Sprintf("%d", captured.ID))
		
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
		
		log.Printf("[%d] %s %s -> %d (%s)", captured.ID, captured.Method, captured.URL, rec.statusCode, captured.Response.Latency)
	})

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("🔍 HTTP Debug Proxy running on http://localhost%s\n", addr)
	fmt.Printf("   Proxying to: %s\n", *target)
	fmt.Printf("   Debug UI: http://localhost%s/__debug/requests\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
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
	
	// Simple HTML UI
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>HTTP Debug Proxy</title>
<style>
body { font-family: monospace; margin: 20px; background: #1e1e1e; color: #d4d4d4; }
h1 { color: #4ec9b0; }
.request { border: 1px solid #444; margin: 10px 0; padding: 10px; border-radius: 4px; }
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
<p>%d requests captured | <a href="/__debug/clear" style="color: #569cd6;">Clear</a></p>
`, len(requests))
	
	for i := len(requests) - 1; i >= 0; i-- {
		req := requests[i]
		statusClass := "status"
		if req.Response != nil {
			if req.Response.Status >= 200 && req.Response.Status < 300 {
				statusClass += "-2xx"
			} else if req.Response.Status >= 400 && req.Response.Status < 500 {
				statusClass += "-4xx"
			} else if req.Response.Status >= 500 {
				statusClass += "-5xx"
			}
		}
		
		fmt.Fprintf(w, `<div class="request">
<div><span class="method">%s</span> <span class="url">%s</span></div>
<div>Time: %s</div>`, req.Method, req.URL, req.Timestamp.Format("15:04:05"))
		
		if req.Response != nil {
			fmt.Fprintf(w, `<div>Response: <span class="%s">%d</span> (in %s)</div>`, 
				statusClass, req.Response.Status, req.Response.Latency)
			
			if req.Body != "" {
				fmt.Fprintf(w, `<details><summary>Request Body</summary><div class="body">%s</div></details>`, req.Body)
			}
			if req.Response.Body != "" {
				fmt.Fprintf(w, `<details><summary>Response Body</summary><div class="body">%s</div></details>`, req.Response.Body)
			}
		}
		fmt.Fprintf(w, `</div>`)
	}
	
	fmt.Fprintf(w, "</body></html>")
}

func handleDebugClear(w http.ResponseWriter, r *http.Request) {
	requestMutex.Lock()
	requests = []CapturedRequest{}
	requestID = 0
	requestMutex.Unlock()
	
	http.Redirect(w, r, "/__debug/requests", http.StatusSeeOther)
}
