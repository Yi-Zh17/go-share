package main

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed templates/*
var templateFS embed.FS
var tmpls *template.Template

type File struct {
	Name      string
	Path      string
	URL       string
	Ext       string
	MediaType string
}

type PageData struct {
	CurrentPath string
	ParentPath  string
	Items       []File
}

func init() {
	var err error
	tmpls, err = template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Fatal(err)
	}
}

func getCategory(filename string) string {
	ext := filepath.Ext(filename)

	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return "image"

	case ".mp4", ".mov", ".mkv":
		return "video"

	case ".zip", ".gz":
		return "archive"

	case ".doc", ".docx", ".md", ".txt", ".pdf":
		return "document"

	default:
		return "unknown"
	}
}

func handleGallery(w http.ResponseWriter, r *http.Request) {
	// Get subfolder
	subPath := r.URL.Query().Get("path")
	if subPath == "" {
		subPath = "."
	}

	cleanSubPath := filepath.Clean(strings.TrimPrefix(subPath, "/"))
	targetPath := filepath.Join(folderPath, cleanSubPath)

	// Read the directory
	files, err := os.ReadDir(targetPath)
	if err != nil {
		http.Error(w, "Folder not found", http.StatusNotFound)
		return
	}

	// Create list of files
	fileObjs := make([]File, 0, len(files))

	for _, file := range files {
		var webURL string
		// Check if folder
		mediaType := getCategory(file.Name())
		if file.IsDir() {
			mediaType = "folder"
			webURL = path.Join(subPath, file.Name())
		} else {
			webURL = path.Join(prefix, subPath, file.Name())
		}

		fileObjs = append(fileObjs, File{
			Name:      file.Name(),
			Path:      filepath.Join(folderPath, file.Name()),
			URL:       webURL,
			Ext:       filepath.Ext(file.Name()),
			MediaType: mediaType,
		})
	}

	parent := ""
	if subPath != "" && subPath != "." {
		parent = filepath.Dir(subPath)
	}

	page := PageData{
		CurrentPath: subPath,
		ParentPath:  parent,
		Items:       fileObjs,
	}

	// Call template
	tmpls.ExecuteTemplate(w, "index.html", page)
}
