package main

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
)

const folder = "./folder"
const prefix = "/folder/"
const port = ":8080"
const url = "10.42.0.127"

var folderPath string

func main() {
	// Get share folder absolute path
	var err error
	folderPath, err = filepath.Abs(folder)
	if err != nil {
		log.Fatal(err)
	}

	// Set up the server
	server := http.NewServeMux()
	fileServer := http.StripPrefix(prefix, http.FileServer(http.Dir(folderPath)))
	server.Handle(prefix, fileServer)
	server.HandleFunc("/", handleGallery)
	server.HandleFunc("/api/upload", handleUpload)
	server.HandleFunc("/api/delete", handleDelete)

	fmt.Println("The server is listening on", url+port)
	log.Fatal(http.ListenAndServe(":8080", server))

}
