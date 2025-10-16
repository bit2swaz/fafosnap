package main

import (
	"fmt"
	"log"
	"net/http"
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

	jobBuffer := workerCount * 2
	if jobBuffer <= 0 {
		jobBuffer = 1
	}

	jobs := make(chan ScreenshotJob, jobBuffer)

	for i := 0; i < workerCount; i++ {
		go worker(i+1, jobs, cfg)
	}

	srv := newServer(cfg, jobs)

	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Printf("server listening on %s with %d workers", addr, workerCount)

	if err := http.ListenAndServe(addr, srv.routes()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
