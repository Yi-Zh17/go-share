package main

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/disintegration/imaging"
)

const (
	thumbnailSize    = 256
	reservedCPUCores = 1
)

var thumbManager = newThumbnailManager(workerCount())

type thumbnailJob struct {
	originalPath string
	cachePath    string
	mediaType    string
}

type thumbnailTask struct {
	done chan struct{}
	err  error
}

type thumbnailManagerState struct {
	mu       sync.Mutex
	inflight map[string]*thumbnailTask
}

type thumbnailManager struct {
	foreground chan thumbnailJob
	background chan thumbnailJob
	state      thumbnailManagerState
}

func newThumbnailManager(workers int) *thumbnailManager {
	m := &thumbnailManager{
		foreground: make(chan thumbnailJob, workers*2),
		background: make(chan thumbnailJob, workers*8),
		state: thumbnailManagerState{
			inflight: make(map[string]*thumbnailTask),
		},
	}

	for range workers {
		go m.worker()
	}

	return m
}

func workerCount() int {
	cpus := runtime.NumCPU() - reservedCPUCores
	if cpus < 1 {
		return 1
	}
	return cpus
}

func (m *thumbnailManager) ensure(job thumbnailJob, priority bool) error {
	if cacheReady(job.cachePath) {
		return nil
	}

	task, enqueue := m.register(job)
	if enqueue {
		queue := m.background
		if priority {
			queue = m.foreground
		}
		queue <- job
	}

	<-task.done
	return task.err
}

func (m *thumbnailManager) schedule(job thumbnailJob) {
	if cacheReady(job.cachePath) {
		return
	}

	_, enqueue := m.register(job)
	if enqueue {
		m.background <- job
	}
}

func (m *thumbnailManager) register(job thumbnailJob) (*thumbnailTask, bool) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()

	if task, ok := m.state.inflight[job.cachePath]; ok {
		return task, false
	}

	task := &thumbnailTask{done: make(chan struct{})}
	m.state.inflight[job.cachePath] = task
	return task, true
}

func (m *thumbnailManager) worker() {
	for {
		select {
		case job := <-m.foreground:
			m.run(job)
			continue
		default:
		}

		select {
		case job := <-m.foreground:
			m.run(job)
		case job := <-m.background:
			m.run(job)
		}
	}
}

func (m *thumbnailManager) run(job thumbnailJob) {
	m.state.mu.Lock()
	task := m.state.inflight[job.cachePath]
	m.state.mu.Unlock()

	err := generateThumbnail(job)

	m.state.mu.Lock()
	if task != nil {
		task.err = err
		close(task.done)
	}
	delete(m.state.inflight, job.cachePath)
	m.state.mu.Unlock()
}

func buildThumbnailJob(originalPath string) (thumbnailJob, error) {
	mediaType := getCategory(originalPath)
	if mediaType != "image" && mediaType != "video" {
		return thumbnailJob{}, errors.New("no thumbnail for this type")
	}

	relPath, err := filepath.Rel(folderPath, originalPath)
	if err != nil {
		return thumbnailJob{}, err
	}

	flatName := strings.ReplaceAll(relPath, string(filepath.Separator), "_")
	flatName = strings.TrimSuffix(flatName, filepath.Ext(flatName)) + ".jpg"

	return thumbnailJob{
		originalPath: originalPath,
		cachePath:    filepath.Join(cachePath, flatName),
		mediaType:    mediaType,
	}, nil
}

func cacheReady(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func invalidateBrokenCache(path string) {
	info, err := os.Stat(path)
	if err == nil && info.Size() == 0 {
		log.Printf("Detected broken 0-byte cache file, regenerating: %s", path)
		_ = os.Remove(path)
	}
}

func generateThumbnail(job thumbnailJob) error {
	invalidateBrokenCache(job.cachePath)

	switch job.mediaType {
	case "image":
		src, err := imaging.Open(job.originalPath)
		if err != nil {
			return err
		}

		thumb := imaging.Thumbnail(src, thumbnailSize, thumbnailSize, imaging.Lanczos)
		if err := imaging.Save(thumb, job.cachePath); err != nil {
			return err
		}
	case "video":
		cmd := exec.Command("ffmpeg",
			"-v", "error",
			"-ss", "00:00:01.000",
			"-i", job.originalPath,
			"-vframes", "1",
			"-vf", "scale=256:-1",
			"-threads", "1",
			"-y", job.cachePath,
		)
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	if !cacheReady(job.cachePath) {
		_ = os.Remove(job.cachePath)
		return errors.New("generated empty thumbnail")
	}

	return nil
}
