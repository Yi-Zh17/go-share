package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const folder = "./folder"
const prefix = "/folder/"
const port = ":8080"

var folderPath string
var cachePath string

func startBackgroundWorker() {
	for {
		log.Println("Background worker: Starting directory scan...")

		filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if d.IsDir() {
				if d.Name() == ".cache" {
					return filepath.SkipDir
				}
				return nil
			}

			mediaType := getCategory(path)
			if mediaType != "image" && mediaType != "video" {
				return nil
			}

			job, err := buildThumbnailJob(path)
			if err != nil {
				return nil
			}
			thumbManager.schedule(job)

			return nil
		})

		log.Println("Background worker: Scan complete. Sleeping for 10 minutes.")
		time.Sleep(10 * time.Minute)
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Get share folder absolute path
	var err error
	folderPath, err = filepath.Abs(folder)
	if err != nil {
		log.Fatal(err)
	}

	// Get cache path
	cachePath = filepath.Join(folderPath, ".cache")

	// Create cache folder if not existing
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		log.Fatal("Cannot create a cache folder:", err)
	}

	// Set up the server
	server := http.NewServeMux()
	fileServer := http.StripPrefix(prefix, http.FileServer(http.Dir(folderPath)))
	server.Handle(prefix, fileServer)
	server.HandleFunc("/", handleGallery)
	server.HandleFunc("/api/upload", handleUpload)
	server.HandleFunc("/api/delete", handleDelete)
	server.HandleFunc("/api/thumb", handleThumbnail)

	// Launch the background worker in its own Goroutine
	log.Printf("Thumbnail workers: %d active, %d core reserved for HTTP/UI", workerCount(), reservedCPUCores)
	go startBackgroundWorker()

	fmt.Println("The server is listening on localhost", port)
	log.Fatal((&http.Server{
		Addr:         port,
		Handler:      server,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}).ListenAndServe())

}
