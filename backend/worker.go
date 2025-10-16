package main

import "log"

// ScreenshotResult holds the outcome of a screenshot job.
type ScreenshotResult struct {
	URL   string
	Image []byte
	Err   error
}

// ScreenshotJob contains the work item dispatched to a worker.
type ScreenshotJob struct {
	URL         string
	Response    chan<- ScreenshotResult
	Attempts    int
	MaxAttempts int
}

// worker consumes jobs, capturing screenshots and emitting results.
func worker(id int, jobs chan ScreenshotJob, config *Config) {
	log.Printf("worker %d started", id)

	for job := range jobs {
		log.Printf("worker %d processing %s (attempt %d/%d)", id, job.URL, job.Attempts+1, job.MaxAttempts)
		img, err := takeScreenshot(job.URL, config.JobTimeoutSeconds)

		if err != nil && job.Attempts+1 < job.MaxAttempts {
			log.Printf("worker %d retrying %s: %v", id, job.URL, err)
			job.Attempts++
			go func(j ScreenshotJob) {
				jobs <- j
			}(job)
			continue
		}

		result := ScreenshotResult{
			URL:   job.URL,
			Image: img,
			Err:   err,
		}

		job.Response <- result
	}
}
