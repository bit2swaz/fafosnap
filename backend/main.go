package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func main() {
	cfg, err := LoadConfiguration("config.json")
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	workerCount := cfg.WorkerCount
	if workerCount <= 0 {
		log.Printf("workerCount (%d) invalid, defaulting to 1", workerCount)
		workerCount = 1
	}

	jobTimeout := cfg.JobTimeoutSeconds
	if jobTimeout <= 0 {
		jobTimeout = 60
	}

	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 2
	}

	urls := sanitizeURLs(cfg.DefaultURLs)
	if len(urls) == 0 {
		urls = []string{"https://github.com"}
		log.Printf("no defaultUrls set in config; using fallback list: %v", urls)
	}

	log.Printf("processing %d url(s) with %d worker(s)", len(urls), workerCount)

	jobs := make(chan ScreenshotJob, len(urls)*maxAttempts)
	results := make(chan ScreenshotResult, len(urls)*maxAttempts)

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(id, jobs, results, jobTimeout)
		}(i + 1)
	}

	for _, url := range urls {
		jobs <- ScreenshotJob{URL: url, Attempt: 0}
	}

	pending := len(urls)
	successes := 0
	failures := 0
	failedURLs := make([]string, 0)

	outputDir := "screenshots"
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Fatalf("create output directory: %v", err)
	}

	fileIndex := 1
	for pending > 0 {
		res := <-results
		nextAttempt := res.Attempt + 1

		if res.Err != nil && nextAttempt < maxAttempts {
			log.Printf("retrying %s (attempt %d of %d)", res.URL, nextAttempt+1, maxAttempts)
			jobs <- ScreenshotJob{URL: res.URL, Attempt: nextAttempt}
			continue
		}

		if res.Err != nil {
			failures++
			failedURLs = append(failedURLs, fmt.Sprintf("%s (after %d attempt(s))", res.URL, nextAttempt))
			log.Printf("failed to capture %s after %d attempt(s): %v", res.URL, nextAttempt, res.Err)
		} else {
			successes++
			filename := fmt.Sprintf("output-%02d.png", fileIndex)
			fileIndex++
			path := filepath.Join(outputDir, filename)
			if err := os.WriteFile(path, res.Image, 0o644); err != nil {
				log.Printf("failed to write %s: %v", path, err)
				failures++
				failedURLs = append(failedURLs, fmt.Sprintf("%s (write error)", res.URL))
			} else {
				log.Printf("saved screenshot for %s -> %s", res.URL, path)
			}
		}

		pending--
	}

	close(jobs)
	wg.Wait()

	log.Printf("completed %d url(s): %d success, %d failure", len(urls), successes, failures)
	if failures > 0 {
		log.Printf("failed captures: %s", strings.Join(failedURLs, "; "))
	}
}

func sanitizeURLs(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}

	urls := make([]string, 0, len(raw))
	for _, candidate := range raw {
		u := strings.TrimSpace(candidate)
		if u == "" {
			continue
		}
		urls = append(urls, u)
	}
	return urls
}
