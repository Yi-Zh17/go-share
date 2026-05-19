package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

		// Wrap the file operations in an anonymous function.
		// This forces the 'defer' statements to execute at the end of each loop iteration.
		err := func() error {
			// Open file
			source, err := fileHeader.Open()
			if err != nil {
				return err // Return the error to the outer loop
			}
			// Close source when this anonymous function finishes
			defer source.Close()

			// Create paths
			cleanFileName := filepath.Base(fileHeader.Filename)
			fullDiskPath := filepath.Join(folderPath, subPath, cleanFileName)

			// Create destination files
			destination, err := os.Create(fullDiskPath)
			if err != nil {
				return err
			}
			defer destination.Close()

			// Copy files
			if _, err := io.Copy(destination, source); err != nil {
				return err
			}

			if job, err := buildThumbnailJob(fullDiskPath); err == nil {
				thumbManager.schedule(job)
			}

			return nil // Success for this file
		}()

		// If the anonymous function returned an error, halt the upload process
		if err != nil {
			http.Error(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
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

		if job, err := buildThumbnailJob(fullPath); err == nil {
			if err := os.Remove(job.cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("Failed to delete cache %s: %v", job.cachePath, err)
			}
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

	job, err := buildThumbnailJob(originalPath)
	if err != nil {
		http.Error(w, "No thumbnail for this type", http.StatusBadRequest)
		return
	}

	if err := thumbManager.ensure(job, true); err != nil {
		log.Printf("Thumbnail generation failed for %s: %v", originalPath, err)
		http.Error(w, "Thumbnail generation failed", http.StatusInternalServerError)
		return
	}

	// Serve thumbnail
	http.ServeFile(w, r, job.cachePath)
}
