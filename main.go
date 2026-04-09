package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const folder = "./folder"
const prefix = "/folder/"
const port = ":8080"

var folderPath string
var cachePath string

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

	fmt.Println("The server is listening on port", port)
	log.Fatal(http.ListenAndServe(":8080", server))

}
