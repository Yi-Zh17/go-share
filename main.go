package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/disintegration/imaging"
)

const folder = "./folder"
const prefix = "/folder/"
const port = ":8080"

var folderPath string
var cachePath string

func startBackgroundWorker() {
	for {
		log.Println("Background worker: Starting directory scan...")

		var wg sync.WaitGroup

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

			relPath, _ := filepath.Rel(folderPath, path)
			cacheFilePath := filepath.Join(cachePath, cacheKeyFor(relPath))

			info, err := os.Stat(cacheFilePath)
			if err == nil && info.Size() > 0 {
				return nil
			}

			log.Printf("Background worker: Generating thumbnail for %s", relPath)

			wg.Add(1)
			go func(media, src, dst string) {
				defer wg.Done()

				// Acquire semaphore slot for background generation
				thumbLimiter <- struct{}{}
				defer func() { <-thumbLimiter }()

				switch media {
				case "image":
					srcImg, err := imaging.Open(src)
					if err != nil {
						log.Printf("Background worker: failed to open %s: %v", src, err)
						return
					}
					thumb := imaging.Thumbnail(srcImg, 256, 256, imaging.Lanczos)
					if err := imaging.Save(thumb, dst); err != nil {
						log.Printf("Background worker: failed to save thumbnail for %s: %v", src, err)
					}
				case "video":
					cmd := exec.Command("ffmpeg",
						"-v", "error",
						"-ss", "00:00:01.000",
						"-i", src,
						"-vframes", "1",
						"-vf", "scale=256:-1",
						"-threads", "1",
						"-y", dst,
					)
					if err := cmd.Run(); err != nil {
						log.Printf("Background worker: ffmpeg failed for %s: %v", src, err)
					}
				}
			}(mediaType, path, cacheFilePath)

			return nil
		})

		wg.Wait()
		log.Println("Background worker: Scan complete. Sleeping for 10 minutes.")
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
	server.HandleFunc("/api/scan-duplicates", handleScanDuplicates)
	server.HandleFunc("/api/mkdir", handleMkdir)
	server.HandleFunc("/api/move", handleMove)
	server.HandleFunc("/api/rename", handleRename)
	server.HandleFunc("/api/folders", handleListFolders)

	// Launch the background worker in its own Goroutine
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
