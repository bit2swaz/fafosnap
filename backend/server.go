package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type server struct {
	config *Config
	jobs   chan ScreenshotJob
}

type screenshotRequest struct {
	URLs []string `json:"urls"`
}

type screenshotResponse struct {
	Results []screenshotResponseItem `json:"results"`
}

type screenshotResponseItem struct {
	URL   string `json:"url"`
	Image string `json:"image,omitempty"`
	Error string `json:"error,omitempty"`
}

func newServer(cfg *Config, jobs chan ScreenshotJob) *server {
	return &server{
		config: cfg,
		jobs:   jobs,
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/screenshots", s.handleScreenshots)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func (s *server) handleScreenshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	var req screenshotRequest
	defer r.Body.Close()

	const maxBodyBytes = 1 << 20 // 1 MiB
	body := io.LimitReader(r.Body, maxBodyBytes)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	if len(req.URLs) == 0 {
		http.Error(w, "urls array must not be empty", http.StatusBadRequest)
		return
	}

	maxPerRequest := s.config.MaxUrlsPerRequest
	if maxPerRequest <= 0 {
		maxPerRequest = 8
	}

	if len(req.URLs) > maxPerRequest {
		http.Error(w, "too many urls in request", http.StatusBadRequest)
		return
	}

	respCh := make(chan ScreenshotResult, len(req.URLs))
	cleanedURLs := make([]string, 0, len(req.URLs))

	for _, rawURL := range req.URLs {
		url := strings.TrimSpace(rawURL)
		if url == "" {
			http.Error(w, "urls must not contain empty values", http.StatusBadRequest)
			return
		}

		cleanedURLs = append(cleanedURLs, url)
	}

	maxAttempts := 2

	for _, url := range cleanedURLs {
		job := ScreenshotJob{
			URL:         url,
			Response:    respCh,
			Attempts:    0,
			MaxAttempts: maxAttempts,
		}

		select {
		case s.jobs <- job:
		case <-ctx.Done():
			http.Error(w, "request cancelled", http.StatusRequestTimeout)
			return
		}
	}

	results := make([]screenshotResponseItem, 0, len(cleanedURLs))

	timeoutPerURL := s.config.JobTimeoutSeconds
	if timeoutPerURL <= 0 {
		timeoutPerURL = 60
	}
	totalTimeout := time.Duration(timeoutPerURL*len(cleanedURLs)) * time.Second
	if totalTimeout <= 0 {
		totalTimeout = time.Duration(len(cleanedURLs)) * time.Minute
	}
	deadline := time.After(totalTimeout)

	for i := 0; i < len(cleanedURLs); i++ {
		select {
		case <-ctx.Done():
			http.Error(w, "request cancelled", http.StatusRequestTimeout)
			return
		case <-deadline:
			http.Error(w, "timed out waiting for screenshots", http.StatusGatewayTimeout)
			return
		case res := <-respCh:
			item := screenshotResponseItem{URL: res.URL}
			if res.Err != nil {
				item.Error = res.Err.Error()
			} else if len(res.Image) > 0 {
				item.Image = base64.StdEncoding.EncodeToString(res.Image)
			} else {
				item.Error = "no image data returned"
			}
			results = append(results, item)
		}
	}

	writeJSON(w, screenshotResponse{Results: results})
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(payload); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
