package main

import "log"

// ScreenshotResult holds the outcome of a screenshot job.
type ScreenshotResult struct {
	URL   string
	Image []byte
	Err   error
}

// worker consumes jobs, capturing screenshots and emitting results.
func worker(id int, jobs <-chan string, results chan<- ScreenshotResult, config *Config) {
	log.Printf("worker %d started", id)

	for url := range jobs {
		img, err := takeScreenshot(url, config.JobTimeoutSeconds)
		results <- ScreenshotResult{
			URL:   url,
			Image: img,
			Err:   err,
		}
	}
}
