package main

import (
	"encoding/json"
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
