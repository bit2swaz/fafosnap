package main

import (
	"fmt"
	"log"
	"os"
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

	jobs := make(chan string)
	results := make(chan ScreenshotResult)

	for i := 0; i < workerCount; i++ {
		go worker(i+1, jobs, results, cfg)
	}

	urls := []string{
		"https://google.com",
		"https://vercel.com",
		"https://github.com",
	}

	go func() {
		for _, url := range urls {
			jobs <- url
		}
		close(jobs)
	}()

	for i := 0; i < len(urls); i++ {
		res := <-results
		if res.Err != nil {
			log.Printf("error capturing %s: %v", res.URL, res.Err)
			continue
		}

		filename := fmt.Sprintf("output-%d.png", i+1)
		if err := os.WriteFile(filename, res.Image, 0o644); err != nil {
			log.Printf("failed to write %s for %s: %v", filename, res.URL, err)
			continue
		}

		log.Printf("saved screenshot for %s to %s", res.URL, filename)
	}

	log.Println("all jobs completed")
}
