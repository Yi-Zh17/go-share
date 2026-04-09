package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

const maxMemory = 32 << 20

type DeleteRequest struct {
	Paths []string `json:"paths"`
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form
	err := r.ParseMultipartForm(maxMemory)
	if err != nil {
		http.Error(w, "Form too large", http.StatusBadRequest)
		return
	}

	// Get sub-path
	subPath := r.FormValue("path")

	// Retrieve files
	for _, fileHeader := range r.MultipartForm.File["files"] {
		// Open file
		source, err := fileHeader.Open()
		if err != nil {
			http.Error(w, "Cannot open file", http.StatusInternalServerError)
			return
		}
		// Close source when finish
		defer source.Close()

		// Create paths
		cleanFileName := filepath.Base(fileHeader.Filename)
		fullDiskPath := filepath.Join(folderPath, subPath, cleanFileName)

		// Create destination files
		destination, err := os.Create(fullDiskPath)
		if err != nil {
			http.Error(w, "Could not create file on disk", http.StatusInternalServerError)
			return
		}
		defer destination.Close()

		// Copy files
		if _, err := io.Copy(destination, source); err != nil {
			http.Error(w, "Failed to save file", http.StatusInternalServerError)
			return
		}
	}
	// Reply to browser
	w.WriteHeader(http.StatusOK)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	// Create a struct
	var req DeleteRequest

	// Decode request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	for _, urlPath := range req.Paths {
		// Trim prefix
		relPath := strings.TrimPrefix(urlPath, prefix)

		// Create absolute path
		fullPath := filepath.Join(folderPath, relPath)

		// Security measure
		if !strings.HasPrefix(fullPath, folderPath) {
			http.Error(w, "Access Denied", http.StatusForbidden)
			return
		}

		// Remove
		if err := os.RemoveAll(fullPath); err != nil {
			log.Printf("Failed to delete %s: %v", fullPath, err)
		}
	}
}

func handleThumbnail(w http.ResponseWriter, r *http.Request) {
	// Get file path
	subPath := r.URL.Query().Get("path")
	if subPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	// Absolute path to the original file
	cleanSubPath := filepath.Clean(strings.TrimPrefix(subPath, prefix))
	originalPath := filepath.Join(folderPath, cleanSubPath)

	// Security
	if !strings.HasPrefix(originalPath, folderPath) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Define the cache filename
	flatName := strings.ReplaceAll(cleanSubPath, string(filepath.Separator), "_")
	// Force jpg format
	flatName = strings.TrimSuffix(flatName, filepath.Ext(flatName)) + ".jpg"

	cacheFilePath := filepath.Join(cachePath, flatName)

	if _, err := os.Stat(cacheFilePath); err == nil {
		// The thumbnail already exists
		http.ServeFile(w, r, cacheFilePath)
		return
	}

	// The thumbnail does not exist
	mediaType := getCategory(originalPath)

	switch mediaType {
	case "image":
		// Open original image
		src, err := imaging.Open(originalPath)
		if err != nil {
			log.Printf("Failed to open image for thumbnail: %v", err)
			http.Error(w, "Thumbnail generation failed", http.StatusInternalServerError)
			return
		}

		// Resize to 256 * 256
		thumb := imaging.Thumbnail(src, 256, 256, imaging.Lanczos)

		err = imaging.Save(thumb, cacheFilePath)
		if err != nil {
			log.Printf("Failed to save image thumbnail: %v", err)
			http.Error(w, "Cache write failed", http.StatusInternalServerError)
			return
		}
	case "video":
		// Run FFmpeg to extract a single frame
		cmd := exec.Command("ffmpeg",
			"-ss", "00:00:01:000", // Extract at 1 second
			"-i", originalPath,
			"-vframes", "1", // Output 1 frame
			"-vf", "scale=256:-1", // Resize width to 256px, keep aspect ratio
			"-threads", "1",
			"-y", // Overwrite if exists
			cacheFilePath,
		)

		err := cmd.Run()
		if err != nil {
			log.Printf("FFmpeg failed for %s: %v", originalPath, err)
			http.Error(w, "Video thumbnail failed", http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "No thumbnail for this type", http.StatusBadRequest)
		return
	}

	// Serve thumbnail
	http.ServeFile(w, r, cacheFilePath)
}
