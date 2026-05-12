package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const folder = "./folder"
const prefix = "/folder/"
const port = ":8080"

var folderPath string
var cachePath string

func startBackgroundWorker() {
	// Infinite loop so the worker runs as long as the server is alive
	for {
		log.Println("Background worker: Starting directory scan...")

		// Walk through every file in your shared folder
		filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			// 1. Skip directories. CRITICAL: Skip the .cache folder itself!
			if d.IsDir() {
				if d.Name() == ".cache" {
					return filepath.SkipDir
				}
				return nil
			}

			// 2. Check if it's media
			mediaType := getCategory(path)
			if mediaType != "image" && mediaType != "video" {
				return nil
			}

			// 3. Construct the expected cache filename
			relPath, _ := filepath.Rel(folderPath, path)
			flatName := strings.ReplaceAll(relPath, string(filepath.Separator), "_")
			flatName = strings.TrimSuffix(flatName, filepath.Ext(flatName)) + ".jpg"
			cacheFilePath := filepath.Join(cachePath, flatName)

			// 4. Check if a valid thumbnail already exists
			info, err := os.Stat(cacheFilePath)
			if err == nil && info.Size() > 0 {
				return nil // It exists and is valid, skip to the next file
			}

			// 5. If we reach here, it's missing or broken. Generate it.
			log.Printf("Background worker: Generating thumbnail for %s", relPath)

			if mediaType == "image" {
				exec.Command("ffmpeg",
					"-v", "error",
					"-i", path,
					"-vframes", "1",
					"-vf", "scale=256:-1",
					"-threads", "1",
					"-y",
					cacheFilePath,
				).Run()
			} else if mediaType == "video" {
				exec.Command("ffmpeg",
					"-v", "error",
					"-ss", "00:00:01.000",
					"-i", path,
					"-vframes", "1",
					"-vf", "scale=256:-1",
					"-threads", "1",
					"-y",
					cacheFilePath,
				).Run()
			}

			// 6. HARDWARE SAFETY: Sleep for 500ms to let the Pi's CPU and SD card breathe
			time.Sleep(500 * time.Millisecond)

			return nil
		})

		log.Println("Background worker: Scan complete. Sleeping for 10 minutes.")
		// Sleep for 10 minutes before scanning the drive again
		time.Sleep(10 * time.Minute)
	}
}

func main() {
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
	go startBackgroundWorker()

	fmt.Println("The server is listening on port", port)
	log.Fatal(http.ListenAndServe(":8080", server))

}
