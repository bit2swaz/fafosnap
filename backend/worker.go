package main

import "log"

// ScreenshotJob contains the work item dispatched to a worker.
type ScreenshotJob struct {
	URL     string
	Attempt int
}

// ScreenshotResult holds the outcome of a screenshot job.
type ScreenshotResult struct {
	URL     string
	Attempt int
	Image   []byte
	Err     error
}

// worker consumes jobs, capturing screenshots and emitting results.
func worker(id int, jobs <-chan ScreenshotJob, results chan<- ScreenshotResult, timeoutSeconds int) {
	log.Printf("worker %d ready", id)

	for job := range jobs {
		log.Printf("worker %d processing %s (attempt %d)", id, job.URL, job.Attempt+1)

		img, err := takeScreenshot(job.URL, timeoutSeconds)
		if err != nil {
			log.Printf("worker %d error on %s attempt %d: %v", id, job.URL, job.Attempt+1, err)
			img = nil
		}

		results <- ScreenshotResult{
			URL:     job.URL,
			Attempt: job.Attempt,
			Image:   img,
			Err:     err,
		}
	}

	log.Printf("worker %d shutting down", id)
}
