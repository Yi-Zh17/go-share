package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

// Limit to 3 concurrent background thumbnail generations.
// On a 4-core Pi this leaves 1 core free for HTTP handlers and disk I/O.
// On-demand requests (handleThumbnail) skip the limiter when all slots are taken
// so the UI never blocks during bulk thumbnail work.
var thumbLimiter = make(chan struct{}, 3)

type DeleteRequest struct {
	Paths []string `json:"paths"`
}

func cacheKeyFor(relPath string) string {
	normalized := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(relPath, "\\", "/")))
	sum := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x.jpg", sum)
}

func resolveCollision(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

type DupFile struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type DupGroup struct {
	Hash  string    `json:"hash"`
	Size  int64     `json:"size"`
	Files []DupFile `json:"files"`
}

type ScanResult struct {
	Groups []DupGroup `json:"groups"`
}

func handleMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	cleanName := filepath.Base(req.Name)
	if cleanName == "." || cleanName == ".." || cleanName == "" {
		http.Error(w, "Invalid folder name", http.StatusBadRequest)
		return
	}
	cleanSubPath := filepath.Clean(strings.TrimPrefix(req.Path, "/"))
	targetPath := filepath.Join(folderPath, cleanSubPath, cleanName)
	if !strings.HasPrefix(targetPath, folderPath) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		http.Error(w, "Failed to create folder: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Paths []string `json:"paths"`
		Dest  string   `json:"dest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	destDir := filepath.Join(folderPath, filepath.Clean(strings.TrimPrefix(req.Dest, prefix)))
	if !strings.HasPrefix(destDir, folderPath) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	for _, urlPath := range req.Paths {
		relPath := strings.TrimPrefix(urlPath, prefix)
		srcPath := filepath.Join(folderPath, relPath)
		if !strings.HasPrefix(srcPath, folderPath) {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
		name := filepath.Base(srcPath)
		destPath := filepath.Join(destDir, name)
		destPath = resolveCollision(destPath)
		if err := os.Rename(srcPath, destPath); err != nil {
			log.Printf("Failed to move %s to %s: %v", srcPath, destPath, err)
			http.Error(w, "Move failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Move thumbnail cache too
		srcRel, _ := filepath.Rel(folderPath, srcPath)
		dstRel, _ := filepath.Rel(folderPath, destPath)
		os.Rename(
			filepath.Join(cachePath, cacheKeyFor(srcRel)),
			filepath.Join(cachePath, cacheKeyFor(dstRel)),
		)
	}
	w.WriteHeader(http.StatusOK)
}

func handleListFolders(w http.ResponseWriter, r *http.Request) {
	subPath := r.URL.Query().Get("path")
	if subPath == "" {
		subPath = "."
	}
	cleanSubPath := filepath.Clean(strings.TrimPrefix(subPath, "/"))
	targetPath := filepath.Join(folderPath, cleanSubPath)
	if !strings.HasPrefix(targetPath, folderPath) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		http.Error(w, "Folder not found", http.StatusNotFound)
		return
	}
	type folderEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	folders := make([]folderEntry, 0)
	for _, e := range entries {
		if e.IsDir() && e.Name() != ".cache" {
			folders = append(folders, folderEntry{
				Name: e.Name(),
				Path: prefix + filepath.Join(cleanSubPath, e.Name()),
			})
		}
	}
	parent := ""
	if cleanSubPath != "." {
		parent = prefix + filepath.Dir(cleanSubPath)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"folders": folders,
		"parent":  parent,
		"current": prefix + cleanSubPath,
	})
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
			fullDiskPath = resolveCollision(fullDiskPath)

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

		// Also remove cached thumbnail so a future file with the same name
		// doesn't inherit a stale preview.
		cacheFile := filepath.Join(cachePath, cacheKeyFor(relPath))
		os.Remove(cacheFile)
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
	cacheFilePath := filepath.Join(cachePath, cacheKeyFor(cleanSubPath))

	info, err := os.Stat(cacheFilePath)
	if err == nil {
		if info.Size() > 0 {
			// Valid size, but check that the original hasn't been modified
			// since the cache was created (e.g. file deleted and a new
			// file uploaded under the same name).
			origInfo, origErr := os.Stat(originalPath)
			if origErr != nil || !info.ModTime().Before(origInfo.ModTime()) {
				http.ServeFile(w, r, cacheFilePath)
				return
			}
			log.Printf("Stale thumbnail cache for %s, regenerating", cleanSubPath)
			os.Remove(cacheFilePath)
		} else {
			log.Printf("Detected broken 0-byte cache file, regenerating: %s", cacheFilePath)
			os.Remove(cacheFilePath)
		}
	}

	// The thumbnail does not exist — acquire a concurrency slot.
	// Use defer so a generation error never permanently leaks a slot.
	select {
	case thumbLimiter <- struct{}{}:
		defer func() { <-thumbLimiter }()
	default:
		log.Printf("On-demand thumbnail for %s: all workers busy, proceeding without limit", cleanSubPath)
	}

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
		info, err = os.Stat(cacheFilePath)
		if err != nil || info.Size() == 0 {
			os.Remove(cacheFilePath)
			http.Error(w, "Generated empty file", http.StatusInternalServerError)

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
		info, err = os.Stat(cacheFilePath)
		if err != nil || info.Size() == 0 {
			os.Remove(cacheFilePath)
			http.Error(w, "Generated empty file", http.StatusInternalServerError)

			return
		}
	default:
		http.Error(w, "No thumbnail for this type", http.StatusBadRequest)
		return
	}

	// Serve thumbnail
	http.ServeFile(w, r, cacheFilePath)
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func handleScanDuplicates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type scannedFile struct {
		path string
		name string
		size int64
	}
	sizeGroups := make(map[int64][]scannedFile)

	err := filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".cache" {
				return filepath.SkipDir
			}
			return nil
		}
		if getCategory(path) != "image" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sz := info.Size()
		sizeGroups[sz] = append(sizeGroups[sz], scannedFile{
			path: path,
			name: d.Name(),
			size: sz,
		})
		return nil
	})
	if err != nil {
		log.Printf("Duplicate scan walk failed: %v", err)
		http.Error(w, "Scan failed", http.StatusInternalServerError)
		return
	}

	type hashedFile struct {
		path string
		name string
		size int64
	}
	hashGroups := make(map[string][]hashedFile)

	for _, files := range sizeGroups {
		if len(files) < 2 {
			continue
		}
		for _, f := range files {
			h, err := fileHash(f.path)
			if err != nil {
				log.Printf("Failed to hash %s: %v", f.path, err)
				continue
			}
			hashGroups[h] = append(hashGroups[h], hashedFile{
				path: f.path,
				name: f.name,
				size: f.size,
			})
		}
	}

	var result ScanResult
	for hash, files := range hashGroups {
		if len(files) < 2 {
			continue
		}
		group := DupGroup{
			Hash:  hash,
			Size:  files[0].size,
			Files: make([]DupFile, 0, len(files)),
		}
		for _, f := range files {
			relPath, _ := filepath.Rel(folderPath, f.path)
			group.Files = append(group.Files, DupFile{
				URL:  prefix + relPath,
				Name: f.name,
				Size: f.size,
			})
		}
		result.Groups = append(result.Groups, group)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("Failed to encode scan result: %v", err)
	}
}
