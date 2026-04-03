package services

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AppReleaseManifest struct {
	Version     string            `json:"version"`
	Notes       string            `json:"notes,omitempty"`
	PublishedAt string            `json:"publishedAt"`
	Downloads   map[string]string `json:"downloads"`
}

type AppReleaseService struct {
	storageRoot   string
	publicBaseURL string
}

func NewAppReleaseService(storageRoot string, publicBaseURL string) *AppReleaseService {
	return &AppReleaseService{
		storageRoot:   storageRoot,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

func (s *AppReleaseService) Upload(version string, notes string, fileHeader *multipart.FileHeader) (*AppReleaseManifest, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}
	if fileHeader == nil {
		return nil, fmt.Errorf("file is required")
	}

	platformKey, err := detectReleasePlatform(fileHeader.Filename)
	if err != nil {
		return nil, err
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	downloadsDir := s.downloadsDir()
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return nil, err
	}

	targetPath := filepath.Join(downloadsDir, filepath.Base(fileHeader.Filename))
	if err := os.WriteFile(targetPath, content, 0o644); err != nil {
		return nil, err
	}

	manifest, err := s.loadManifest()
	if err != nil {
		return nil, err
	}
	if manifest.Downloads == nil {
		manifest.Downloads = map[string]string{}
	}

	manifest.Version = version
	if trimmedNotes := strings.TrimSpace(notes); trimmedNotes != "" {
		manifest.Notes = trimmedNotes
	}
	manifest.PublishedAt = time.Now().UTC().Format(time.RFC3339)
	manifest.Downloads[platformKey] = s.publicDownloadURL(filepath.Base(fileHeader.Filename))

	if err := s.writeManifest(manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (s *AppReleaseService) downloadsDir() string {
	return filepath.Join(s.storageRoot, "downloads", "cliproxyapp")
}

func (s *AppReleaseService) manifestPath() string {
	return filepath.Join(s.downloadsDir(), "latest.json")
}

func (s *AppReleaseService) loadManifest() (*AppReleaseManifest, error) {
	path := s.manifestPath()
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AppReleaseManifest{Downloads: map[string]string{}}, nil
		}
		return nil, err
	}
	var manifest AppReleaseManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, err
	}
	if manifest.Downloads == nil {
		manifest.Downloads = map[string]string{}
	}
	return &manifest, nil
}

func (s *AppReleaseService) writeManifest(manifest *AppReleaseManifest) error {
	if err := os.MkdirAll(s.downloadsDir(), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(s.manifestPath(), content, 0o644)
}

func (s *AppReleaseService) publicDownloadURL(fileName string) string {
	relativePath := "/downloads/cliproxyapp/" + url.PathEscape(fileName)
	if s.publicBaseURL == "" {
		return relativePath
	}
	return s.publicBaseURL + relativePath
}

func detectReleasePlatform(fileName string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case strings.HasSuffix(lower, ".exe"):
		return "windows", nil
	case strings.HasSuffix(lower, ".dmg"), strings.HasSuffix(lower, ".app.zip"):
		return "macos", nil
	default:
		return "", fmt.Errorf("unsupported package type: %s", fileName)
	}
}
