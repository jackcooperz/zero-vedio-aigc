package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	defaultCodexImageProviderRef = "codex/image"
	defaultEdgeTTSVoiceName      = "zh-CN-XiaoxiaoNeural"
	defaultEdgeTTSProviderRef    = "edge-tts/" + defaultEdgeTTSVoiceName
	defaultKokoroVoiceName       = "zf_xiaoyi"
	defaultKokoroProviderRef     = "kokoro/" + defaultKokoroVoiceName
	defaultChildSpeakingRate     = "-30%"
	baseCharsPerSec              = 5.6
)

type app struct {
	rootDir             string
	projectEnv          map[string]string
	projectsDir         string
	zeroTokenBridgePath string
	zeroTokenBridgeURL  string
	zeroTokenBridgePort string
	zeroTokenBridgeMu   sync.Mutex
	zeroTokenBridgeCmd  *exec.Cmd
	eventsMu            sync.Mutex
}

type createProjectRequest struct {
	Title                  string `json:"title"`
	Story                  string `json:"story"`
	ProviderRef            string `json:"provider_ref"`
	TargetDurationSec      int    `json:"target_duration_sec,omitempty"`
	ImageSwitchIntervalSec int    `json:"image_switch_interval_sec,omitempty"`
	AspectRatio            string `json:"aspect_ratio,omitempty"`
}

type createStoryVideoFromThemeRequest struct {
	Theme                  string `json:"theme"`
	Title                  string `json:"title,omitempty"`
	Audience               string `json:"audience,omitempty"`
	Tone                   string `json:"tone,omitempty"`
	CycleMode              string `json:"cycle_mode,omitempty"`
	TargetDurationSec      int    `json:"target_duration_sec,omitempty"`
	ImageSwitchIntervalSec int    `json:"image_switch_interval_sec,omitempty"`
	AspectRatio            string `json:"aspect_ratio,omitempty"`
	TimeoutMs              int    `json:"timeout_ms,omitempty"`
	AudioProviderRef       string `json:"audio_provider_ref,omitempty"`
	VoiceName              string `json:"voice_name,omitempty"`
	SpeakingRate           string `json:"speaking_rate,omitempty"`
	Pitch                  string `json:"pitch,omitempty"`
	Width                  int    `json:"width,omitempty"`
	Height                 int    `json:"height,omitempty"`
	FPS                    int    `json:"fps,omitempty"`
	TransitionDurationMs   int    `json:"transition_duration_ms,omitempty"`
	Force                  bool   `json:"force,omitempty"`
}

type generateStoryboardRequest struct {
	ProviderRef      string `json:"provider_ref"`
	BrowserProfileID string `json:"browser_profile_id"`
	TimeoutMs        int    `json:"timeout_ms"`
}

type generateSceneImageRequest struct {
	ProviderRef      string `json:"provider_ref"`
	BrowserProfileID string `json:"browser_profile_id"`
	TimeoutMs        int    `json:"timeout_ms"`
	Force            bool   `json:"force"`
}

type generateSceneKeyframesRequest struct {
	ProviderRef      string `json:"provider_ref"`
	BrowserProfileID string `json:"browser_profile_id"`
	TimeoutMs        int    `json:"timeout_ms"`
	Force            bool   `json:"force"`
}

type generateSceneAudioRequest struct {
	ProviderRef  string `json:"provider_ref"`
	VoiceName    string `json:"voice_name"`
	SpeakingRate string `json:"speaking_rate"`
	Pitch        string `json:"pitch"`
	Force        bool   `json:"force"`
}

type composeSceneVideoRequest struct {
	Width  int  `json:"width"`
	Height int  `json:"height"`
	Force  bool `json:"force"`
}

type composeFinalVideoRequest struct {
	Width                int  `json:"width"`
	Height               int  `json:"height"`
	FPS                  int  `json:"fps"`
	TransitionDurationMs int  `json:"transition_duration_ms"`
	Force                bool `json:"force"`
}

type runProjectPipelineRequest struct {
	ProviderRef          string `json:"provider_ref"`
	BrowserProfileID     string `json:"browser_profile_id"`
	TimeoutMs            int    `json:"timeout_ms"`
	AudioProviderRef     string `json:"audio_provider_ref"`
	VoiceName            string `json:"voice_name"`
	SpeakingRate         string `json:"speaking_rate"`
	Pitch                string `json:"pitch"`
	Width                int    `json:"width"`
	Height               int    `json:"height"`
	FPS                  int    `json:"fps"`
	TransitionDurationMs int    `json:"transition_duration_ms"`
	Force                bool   `json:"force"`
}

type selectImageCandidateRequest struct {
	CandidateIndex int `json:"candidate_index"`
}

type projectFile struct {
	ProjectID                string `json:"project_id"`
	Title                    string `json:"title"`
	Story                    string `json:"story"`
	ProviderRef              string `json:"provider_ref"`
	TargetDurationSec        int    `json:"target_duration_sec,omitempty"`
	ImageSwitchIntervalSec   int    `json:"image_switch_interval_sec,omitempty"`
	AspectRatio              string `json:"aspect_ratio,omitempty"`
	Status                   string `json:"status"`
	StoryboardPath           string `json:"storyboard_path,omitempty"`
	StoryboardValid          bool   `json:"storyboard_valid,omitempty"`
	StoryboardValidationPath string `json:"storyboard_validation_path,omitempty"`
	StoryboardGeneratedAt    string `json:"storyboard_generated_at,omitempty"`
	SceneCount               int    `json:"scene_count,omitempty"`
	FinalVideoStatus         string `json:"final_video_status,omitempty"`
	FinalVideoLocalPath      string `json:"final_video_local_path,omitempty"`
	FinalVideoPreviewURL     string `json:"final_video_preview_url,omitempty"`
	FinalVideoMimeType       string `json:"final_video_mime_type,omitempty"`
	FinalVideoDurationMs     int    `json:"final_video_duration_ms,omitempty"`
	FinalVideoError          string `json:"final_video_error,omitempty"`
	FinalVideoGeneratedAt    string `json:"final_video_generated_at,omitempty"`
	CreatedAt                string `json:"created_at"`
	UpdatedAt                string `json:"updated_at"`
}

type taskFile struct {
	TaskID     string `json:"task_id"`
	Kind       string `json:"kind"`
	SceneID    string `json:"scene_id,omitempty"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type projectEvent struct {
	Seq       int            `json:"seq"`
	ProjectID string         `json:"project_id"`
	Phase     string         `json:"phase"`
	Status    string         `json:"status"`
	Message   string         `json:"message"`
	SceneID   string         `json:"scene_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	CreatedAt string         `json:"created_at"`
}

type projectDetailResponse struct {
	Project              projectFile                 `json:"project"`
	Storyboard           json.RawMessage             `json:"storyboard,omitempty"`
	StoryboardValidation *storyboardValidationResult `json:"storyboard_validation,omitempty"`
	Scenes               []sceneFile                 `json:"scenes,omitempty"`
	Tasks                []taskFile                  `json:"tasks"`
	Files                map[string]string           `json:"files"`
}

type bridgeResponse struct {
	OK     bool              `json:"ok"`
	Result zeroTokenGenerate `json:"result"`
	Error  string            `json:"error"`
	Name   string            `json:"name"`
}

type zeroTokenBridgePayload struct {
	RequestID      string         `json:"requestId,omitempty"`
	ProjectID      string         `json:"projectId,omitempty"`
	SceneID        string         `json:"sceneId,omitempty"`
	ProviderRef    string         `json:"providerRef"`
	Capability     string         `json:"capability,omitempty"`
	Input          map[string]any `json:"input"`
	RuntimeOptions map[string]any `json:"runtimeOptions,omitempty"`
}

type zeroTokenGenerate struct {
	RequestID string                  `json:"requestId"`
	Output    zeroTokenGenerateOutput `json:"output"`
}

type zeroTokenGenerateOutput struct {
	Text   string                    `json:"text"`
	JSON   json.RawMessage           `json:"json"`
	Images []zeroTokenGeneratedImage `json:"images"`
}

type zeroTokenGeneratedImage struct {
	URL       string `json:"url"`
	LocalPath string `json:"localPath,omitempty"`
	MimeType  string `json:"mimeType,omitempty"`
}

type storyboardValidationResult struct {
	Valid       bool     `json:"valid"`
	Errors      []string `json:"errors"`
	SceneCount  int      `json:"scene_count,omitempty"`
	ValidatedAt string   `json:"validated_at,omitempty"`
}

type imageCandidate struct {
	CandidateID     string `json:"candidate_id,omitempty"`
	SourceIndex     int    `json:"source_index,omitempty"`
	ImageURL        string `json:"image_url,omitempty"`
	ImageLocalPath  string `json:"image_local_path,omitempty"`
	ImagePreviewURL string `json:"image_preview_url,omitempty"`
	ImageMimeType   string `json:"image_mime_type,omitempty"`
}

type keyframe struct {
	FrameID          string           `json:"frame_id"`
	Sequence         int              `json:"sequence"`
	Characters       []string         `json:"characters,omitempty"`
	Prompt           map[string]any   `json:"prompt"`
	Visual           map[string]any   `json:"visual"`
	ImageCandidates  []imageCandidate `json:"image_candidates,omitempty"`
	SelectedImageIdx int              `json:"selected_image_index,omitempty"`
	ImageLocalPath   string           `json:"image_local_path,omitempty"`
	ImageURL         string           `json:"image_url,omitempty"`
	ImagePreviewURL  string           `json:"image_preview_url,omitempty"`
	ImageMimeType    string           `json:"image_mime_type,omitempty"`
	ImageError       string           `json:"image_error,omitempty"`
	ImageGeneratedAt string           `json:"image_generated_at,omitempty"`
}

type sceneFile struct {
	ProjectID             string           `json:"project_id"`
	SceneID               string           `json:"scene_id"`
	Sequence              int              `json:"sequence"`
	Title                 string           `json:"title"`
	StoryFunction         string           `json:"story_function"`
	Narration             string           `json:"narration"`
	Subtitle              string           `json:"subtitle"`
	DurationHintSec       int              `json:"duration_hint_sec"`
	Characters            []string         `json:"characters"`
	Objects               []string         `json:"objects"`
	Environment           map[string]any   `json:"environment"`
	Visual                map[string]any   `json:"visual"`
	Prompt                map[string]any   `json:"prompt"`
	Audio                 map[string]any   `json:"audio"`
	Effects               map[string]any   `json:"effects"`
	Status                string           `json:"status"`
	ImageStatus           string           `json:"image_status"`
	ImageProviderRef      string           `json:"image_provider_ref,omitempty"`
	ImagePrompt           string           `json:"image_prompt,omitempty"`
	ImageCandidates       []imageCandidate `json:"image_candidates,omitempty"`
	SelectedImageIdx      int              `json:"selected_image_index,omitempty"`
	ImageURL              string           `json:"image_url,omitempty"`
	ImageLocalPath        string           `json:"image_local_path,omitempty"`
	ImagePreviewURL       string           `json:"image_preview_url,omitempty"`
	ImageMimeType         string           `json:"image_mime_type,omitempty"`
	ImageError            string           `json:"image_error,omitempty"`
	ImageGeneratedAt      string           `json:"image_generated_at,omitempty"`
	Keyframes             []keyframe       `json:"keyframes,omitempty"`
	AudioStatus           string           `json:"audio_status"`
	AudioProviderRef      string           `json:"audio_provider_ref,omitempty"`
	VoiceName             string           `json:"voice_name,omitempty"`
	SpeakingRate          string           `json:"speaking_rate,omitempty"`
	Pitch                 string           `json:"pitch,omitempty"`
	AudioLocalPath        string           `json:"audio_local_path,omitempty"`
	AudioPreviewURL       string           `json:"audio_preview_url,omitempty"`
	AudioMimeType         string           `json:"audio_mime_type,omitempty"`
	AudioDurationMs       int              `json:"audio_duration_ms,omitempty"`
	AudioError            string           `json:"audio_error,omitempty"`
	AudioGeneratedAt      string           `json:"audio_generated_at,omitempty"`
	SubtitleLocalPath     string           `json:"subtitle_local_path,omitempty"`
	SubtitlePreviewURL    string           `json:"subtitle_preview_url,omitempty"`
	SubtitleASSLocalPath  string           `json:"subtitle_ass_local_path,omitempty"`
	SubtitleASSPreviewURL string           `json:"subtitle_ass_preview_url,omitempty"`
	ComposeStatus         string           `json:"compose_status"`
	ComposeMode           string           `json:"compose_mode,omitempty"`
	SceneVideoLocalPath   string           `json:"scene_video_local_path,omitempty"`
	SceneVideoPreviewURL  string           `json:"scene_video_preview_url,omitempty"`
	SceneVideoMimeType    string           `json:"scene_video_mime_type,omitempty"`
	SceneDurationMs       int              `json:"scene_duration_ms,omitempty"`
	ComposeError          string           `json:"compose_error,omitempty"`
	ComposedAt            string           `json:"composed_at,omitempty"`
	CreatedAt             string           `json:"created_at"`
	UpdatedAt             string           `json:"updated_at"`
}

type storyboardValidationError struct {
	Errors []string
}

func (e *storyboardValidationError) Error() string {
	return "storyboard validation failed: " + strings.Join(e.Errors, "; ")
}

func main() {
	rootDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("resolve working directory: %v", err)
	}
	projectEnv, err := loadProjectEnv(rootDir)
	if err != nil {
		log.Printf("load project .env failed: %v", err)
	}

	server := &app{
		rootDir:             rootDir,
		projectEnv:          projectEnv,
		projectsDir:         filepath.Join(rootDir, "projects"),
		zeroTokenBridgePath: filepath.Join(rootDir, "dist", "zero-token", "bridge-server.js"),
		zeroTokenBridgePort: envOrDefault("ZERO_TOKEN_BRIDGE_PORT", "4390"),
	}
	server.zeroTokenBridgeURL = "http://127.0.0.1:" + server.zeroTokenBridgePort

	if err := os.MkdirAll(server.projectsDir, 0o755); err != nil {
		log.Fatalf("create projects dir: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleHome)
	mux.HandleFunc("/healthz", server.handleHealthz)
	mux.HandleFunc("/api/story-video/from-theme", server.handleStoryVideoFromTheme)
	mux.HandleFunc("/api/projects", server.handleProjects)
	mux.HandleFunc("/api/projects/", server.handleProjectRoutes)
	mux.Handle("/local/projects/", http.StripPrefix("/local/projects/", http.FileServer(http.Dir(server.projectsDir))))

	port := envOrDefault("PORT", "4388")
	log.Printf("story video server listening on http://127.0.0.1:%s", port)
	log.Fatal(http.ListenAndServe("127.0.0.1:"+port, mux))
}

func (a *app) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                      true,
		"projects_dir":            a.projectsDir,
		"zero_token_bridge":       a.zeroTokenBridgePath,
		"zero_token_bridge_url":   a.zeroTokenBridgeURL,
		"zero_token_bridge_built": fileExists(a.zeroTokenBridgePath),
		"codex":                   codexCLIHealth(a.projectEnv),
		"kokoro_tts":              a.kokoroTTSHealth(),
		"edge_tts":                lookupCommand("edge-tts"),
		"ffmpeg":                  lookupCommand("ffmpeg"),
		"ffprobe":                 lookupCommand("ffprobe"),
	})
}

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, errors.New("page not found"))
		return
	}

	writeHTML(w, http.StatusOK, buildHomeHTML(a.projectsDir, fileExists(a.zeroTokenBridgePath)))
}

func (a *app) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projects, err := a.listProjects()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
	case http.MethodPost:
		var req createProjectRequest
		if err := decodeJSONBody(r.Body, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Story) == "" {
			writeError(w, http.StatusBadRequest, errors.New("title and story are required"))
			return
		}
		project, err := a.createProject(req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"project": project})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) handleStoryVideoFromTheme(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req createStoryVideoFromThemeRequest
	if err := decodeJSONBody(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Theme) == "" {
		writeError(w, http.StatusBadRequest, errors.New("theme is required"))
		return
	}

	projectReq := buildProjectRequestFromTheme(req)
	project, err := a.createProject(projectReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	detail, err := a.runProjectPipeline(project.ProjectID, buildPipelineRequestFromTheme(req))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		var validationErr *storyboardValidationError
		if errors.As(err, &validationErr) {
			status = http.StatusUnprocessableEntity
		}
		detail, _ := a.getProjectDetail(project.ProjectID)
		writeJSON(w, status, map[string]any{
			"error":      err.Error(),
			"project_id": project.ProjectID,
			"detail":     detail,
		})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *app) handleProjectRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, errors.New("project route not found"))
		return
	}

	projectID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		detail, err := a.getProjectDetail(projectID)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
		return
	}

	if len(parts) == 2 && parts[1] == "storyboard" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req generateStoryboardRequest
		if err := decodeJSONBody(r.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		detail, err := a.generateStoryboard(projectID, req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			var validationErr *storyboardValidationError
			if errors.As(err, &validationErr) {
				status = http.StatusUnprocessableEntity
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
		return
	}

	if len(parts) == 2 && parts[1] == "scenes" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		scenes, err := a.listScenes(projectID)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"project_id": projectID,
			"scenes":     scenes,
		})
		return
	}

	if len(parts) == 2 && parts[1] == "assets" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		assets, err := a.getProjectAssets(projectID)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, assets)
		return
	}

	if len(parts) == 2 && parts[1] == "events" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		after, _ := strconv.Atoi(r.URL.Query().Get("after"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		events, err := a.readProjectEvents(projectID, after, limit)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"project_id": projectID,
			"events":     events,
		})
		return
	}

	if len(parts) == 2 && parts[1] == "run-all" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req runProjectPipelineRequest
		if err := decodeJSONBody(r.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		detail, err := a.runProjectPipeline(projectID, req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			var validationErr *storyboardValidationError
			if errors.As(err, &validationErr) {
				status = http.StatusUnprocessableEntity
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
		return
	}

	if len(parts) == 2 && parts[1] == "final-video" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req composeFinalVideoRequest
		if err := decodeJSONBody(r.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		project, err := a.composeFinalVideo(projectID, req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"project": project})
		return
	}

	if len(parts) == 3 && parts[1] == "scenes" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		scene, err := a.getSceneDetail(projectID, parts[2])
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, scene)
		return
	}

	if len(parts) == 4 && parts[1] == "scenes" && parts[3] == "image" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req generateSceneImageRequest
		if err := decodeJSONBody(r.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		scene, err := a.generateSceneImage(projectID, parts[2], req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, scene)
		return
	}

	if len(parts) == 4 && parts[1] == "scenes" && parts[3] == "keyframes" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req generateSceneKeyframesRequest
		if err := decodeJSONBody(r.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		scene, err := a.generateSceneKeyframes(projectID, parts[2], req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, scene)
		return
	}

	if len(parts) == 4 && parts[1] == "scenes" && parts[3] == "audio" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req generateSceneAudioRequest
		if err := decodeJSONBody(r.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		scene, err := a.generateSceneAudio(projectID, parts[2], req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, scene)
		return
	}

	if len(parts) == 4 && parts[1] == "scenes" && parts[3] == "video" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req composeSceneVideoRequest
		if err := decodeJSONBody(r.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		scene, err := a.composeSceneVideo(projectID, parts[2], req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, scene)
		return
	}

	if len(parts) == 5 && parts[1] == "scenes" && parts[3] == "image" && parts[4] == "select" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req selectImageCandidateRequest
		if err := decodeJSONBody(r.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		scene, err := a.selectSceneImageCandidate(projectID, parts[2], req.CandidateIndex)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, scene)
		return
	}

	writeError(w, http.StatusNotFound, errors.New("project route not found"))
}

func buildProjectRequestFromTheme(req createStoryVideoFromThemeRequest) createProjectRequest {
	targetDurationSec := req.TargetDurationSec
	if targetDurationSec <= 0 {
		targetDurationSec = 60
	}
	imageSwitchIntervalSec := req.ImageSwitchIntervalSec
	if imageSwitchIntervalSec <= 0 {
		imageSwitchIntervalSec = 20
	}
	aspectRatio := strings.TrimSpace(req.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = "16:9"
	}
	return createProjectRequest{
		Title:                  titleFromTheme(req),
		Story:                  buildThemeStoryInput(req),
		ProviderRef:            defaultCodexImageProviderRef,
		TargetDurationSec:      targetDurationSec,
		ImageSwitchIntervalSec: imageSwitchIntervalSec,
		AspectRatio:            aspectRatio,
	}
}

func buildPipelineRequestFromTheme(req createStoryVideoFromThemeRequest) runProjectPipelineRequest {
	return runProjectPipelineRequest{
		ProviderRef:          defaultCodexImageProviderRef,
		TimeoutMs:            defaultInt(req.TimeoutMs, 1200000),
		AudioProviderRef:     fallbackString(req.AudioProviderRef, defaultKokoroProviderRef),
		VoiceName:            fallbackString(req.VoiceName, defaultKokoroVoiceName),
		SpeakingRate:         fallbackString(req.SpeakingRate, defaultChildSpeakingRate),
		Pitch:                fallbackString(req.Pitch, "0Hz"),
		Width:                req.Width,
		Height:               req.Height,
		FPS:                  req.FPS,
		TransitionDurationMs: req.TransitionDurationMs,
		Force:                req.Force,
	}
}

func titleFromTheme(req createStoryVideoFromThemeRequest) string {
	if strings.TrimSpace(req.Title) != "" {
		return strings.TrimSpace(req.Title)
	}
	theme := []rune(strings.TrimSpace(req.Theme))
	if len(theme) <= 20 {
		return string(theme)
	}
	return string(theme[:20])
}

func buildThemeStoryInput(req createStoryVideoFromThemeRequest) string {
	audience := fallbackString(req.Audience, "3-6岁儿童")
	tone := fallbackString(req.Tone, "温暖、清晰、适合睡前和绘本视频")
	cycleMode := normalizeCycleMode(req.CycleMode)
	return strings.TrimSpace(fmt.Sprintf(`主题：%s
受众：%s
语气：%s
故事周期模式：%s

请基于这个主题从零规划一个完整绘本故事。不要把主题当成已有故事全文；需要先判断适合 3 个还是 5 个周期，再生成对应的故事线、分镜、旁白、字幕、图片提示词和视频设定。`, strings.TrimSpace(req.Theme), audience, tone, cycleMode))
}

func normalizeCycleMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "3", "3-cycle", "3_cycles", "three":
		return "3"
	case "5", "5-cycle", "5_cycles", "five":
		return "5"
	default:
		return "auto"
	}
}

func storyPlanCycleSummary(raw []byte) (int, string) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return 0, ""
	}
	plan, _ := root["story_plan"].(map[string]any)
	if plan == nil {
		return 0, ""
	}
	cycleCount := intFromAny(plan["cycle_count"])
	reason, _ := plan["cycle_reason"].(string)
	return cycleCount, strings.TrimSpace(reason)
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func defaultInt(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func (a *app) createProject(req createProjectRequest) (projectFile, error) {
	projectID := fmt.Sprintf("pv_%d", time.Now().UnixMilli())
	now := time.Now().UTC().Format(time.RFC3339)
	aspectRatio := strings.TrimSpace(req.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = "9:16"
	}
	project := projectFile{
		ProjectID:              projectID,
		Title:                  strings.TrimSpace(req.Title),
		Story:                  strings.TrimSpace(req.Story),
		ProviderRef:            defaultProviderRef(req.ProviderRef),
		TargetDurationSec:      req.TargetDurationSec,
		ImageSwitchIntervalSec: req.ImageSwitchIntervalSec,
		AspectRatio:            aspectRatio,
		Status:                 "created",
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	projectDir := filepath.Join(a.projectsDir, projectID)
	if err := os.MkdirAll(filepath.Join(projectDir, "scenes"), 0o755); err != nil {
		return projectFile{}, err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "assets", "images"), 0o755); err != nil {
		return projectFile{}, err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "assets", "audio"), 0o755); err != nil {
		return projectFile{}, err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "assets", "subtitles"), 0o755); err != nil {
		return projectFile{}, err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "assets", "scene_videos"), 0o755); err != nil {
		return projectFile{}, err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "final"), 0o755); err != nil {
		return projectFile{}, err
	}
	if err := writeJSONFile(filepath.Join(projectDir, "project.json"), project); err != nil {
		return projectFile{}, err
	}
	if err := writeJSONFile(filepath.Join(projectDir, "tasks.json"), []taskFile{}); err != nil {
		return projectFile{}, err
	}
	if err := a.resetSceneFiles(projectID); err != nil {
		return projectFile{}, err
	}

	a.recordProjectEvent(projectID, "project", "done", "项目已创建，等待生成绘本视频流水线", map[string]any{
		"title":          project.Title,
		"provider_ref":   project.ProviderRef,
		"aspect_ratio":   project.AspectRatio,
		"target_seconds": project.TargetDurationSec,
	})
	return project, nil
}

func (a *app) listProjects() ([]projectFile, error) {
	entries, err := os.ReadDir(a.projectsDir)
	if err != nil {
		return nil, err
	}

	projects := make([]projectFile, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		project, err := a.readProject(entry.Name())
		if err != nil {
			continue
		}
		projects = append(projects, project)
	}
	sort.SliceStable(projects, func(i, j int) bool {
		left := projectSortTime(projects[i])
		right := projectSortTime(projects[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return projects[i].ProjectID > projects[j].ProjectID
	})
	return projects, nil
}

func projectSortTime(project projectFile) time.Time {
	for _, value := range []string{project.UpdatedAt, project.CreatedAt} {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func (a *app) getProjectDetail(projectID string) (projectDetailResponse, error) {
	project, err := a.readProject(projectID)
	if err != nil {
		return projectDetailResponse{}, err
	}
	tasks, err := a.readTasks(projectID)
	if err != nil {
		return projectDetailResponse{}, err
	}

	files := map[string]string{
		"project": filepath.Join(a.projectsDir, projectID, "project.json"),
		"tasks":   filepath.Join(a.projectsDir, projectID, "tasks.json"),
		"events":  a.projectEventsPath(projectID),
	}

	var storyboard json.RawMessage
	var validation *storyboardValidationResult
	scenes, err := a.readSceneFiles(projectID)
	if err != nil {
		return projectDetailResponse{}, err
	}
	if project.StoryboardPath != "" && fileExists(project.StoryboardPath) {
		raw, err := os.ReadFile(project.StoryboardPath)
		if err != nil {
			return projectDetailResponse{}, err
		}
		storyboard = json.RawMessage(raw)
		files["storyboard"] = project.StoryboardPath
	}
	if project.StoryboardValidationPath != "" && fileExists(project.StoryboardValidationPath) {
		result, err := a.readStoryboardValidation(projectID)
		if err != nil {
			return projectDetailResponse{}, err
		}
		validation = &result
		files["storyboard_validation"] = project.StoryboardValidationPath
	}
	if len(scenes) > 0 {
		files["scenes_index"] = a.sceneIndexPath(projectID)
	}
	if project.FinalVideoLocalPath != "" {
		files["final_video"] = project.FinalVideoLocalPath
	}

	return projectDetailResponse{
		Project:              project,
		Storyboard:           storyboard,
		StoryboardValidation: validation,
		Scenes:               scenes,
		Tasks:                tasks,
		Files:                files,
	}, nil
}

func (a *app) generateStoryboard(projectID string, req generateStoryboardRequest) (projectDetailResponse, error) {
	project, err := a.readProject(projectID)
	if err != nil {
		return projectDetailResponse{}, err
	}

	tasks, err := a.readTasks(projectID)
	if err != nil {
		return projectDetailResponse{}, err
	}
	tasks = pruneDerivedTasks(tasks)

	now := time.Now().UTC().Format(time.RFC3339)
	task := newTask("storyboard_generation", "", "running", "Calling local zero-token bridge server", now)
	tasks = append(tasks, task)
	if writeErr := a.writeTasks(projectID, tasks); writeErr != nil {
		return projectDetailResponse{}, writeErr
	}

	project.Status = "storyboard_generating"
	project.UpdatedAt = now
	if writeErr := a.writeProject(project); writeErr != nil {
		return projectDetailResponse{}, writeErr
	}
	a.recordProjectEvent(projectID, "story", "running", "Codex 正在判断 3/5 周期并规划故事线", map[string]any{
		"provider_ref": defaultCodexImageProviderRef,
		"title":        project.Title,
	})
	a.recordProjectEvent(projectID, "storyboard", "running", "Codex 正在生成分镜、旁白、字幕和图片提示词", nil)

	storyboardJSON, err := a.runZeroTokenStoryboard(project, req)
	if err != nil {
		tasks[len(tasks)-1].Status = "failed"
		tasks[len(tasks)-1].Error = err.Error()
		tasks[len(tasks)-1].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		tasks[len(tasks)-1].FinishedAt = tasks[len(tasks)-1].UpdatedAt
		_ = a.writeTasks(projectID, tasks)

		project.Status = "storyboard_failed"
		project.UpdatedAt = tasks[len(tasks)-1].UpdatedAt
		_ = a.writeProject(project)
		a.recordProjectEvent(projectID, "storyboard", "error", "故事线/分镜生成失败："+err.Error(), nil)
		return projectDetailResponse{}, err
	}

	storyboardPath := filepath.Join(a.projectsDir, projectID, "storyboard.json")
	if writeErr := writeRawJSONFile(storyboardPath, storyboardJSON); writeErr != nil {
		return projectDetailResponse{}, writeErr
	}

	finishedAt := time.Now().UTC().Format(time.RFC3339)
	project.StoryboardPath = storyboardPath
	project.StoryboardGeneratedAt = finishedAt
	project.StoryboardValidationPath = a.storyboardValidationPath(projectID)
	project.UpdatedAt = finishedAt

	validation, scenes := validateStoryboard(storyboardJSON, projectID, finishedAt)
	if writeErr := a.writeStoryboardValidation(projectID, validation); writeErr != nil {
		return projectDetailResponse{}, writeErr
	}
	tasks[len(tasks)-1].Status = "success"
	tasks[len(tasks)-1].UpdatedAt = finishedAt
	tasks[len(tasks)-1].FinishedAt = finishedAt

	if !validation.Valid {
		tasks[len(tasks)-1].Message = "Storyboard saved, but validation failed"
		tasks = append(tasks, newTask("storyboard_validation", "", "failed", "Storyboard validation failed", finishedAt))
		tasks[len(tasks)-1].Error = strings.Join(validation.Errors, "; ")
		tasks[len(tasks)-1].FinishedAt = finishedAt
		if resetErr := a.resetSceneFiles(projectID); resetErr != nil {
			return projectDetailResponse{}, resetErr
		}
		project.Status = "storyboard_invalid"
		project.StoryboardValid = false
		project.SceneCount = validation.SceneCount
		project.UpdatedAt = finishedAt
		if writeErr := a.writeProject(project); writeErr != nil {
			return projectDetailResponse{}, writeErr
		}
		if writeErr := a.writeTasks(projectID, tasks); writeErr != nil {
			return projectDetailResponse{}, writeErr
		}
		a.recordProjectEvent(projectID, "storyboard", "error", "分镜校验失败："+strings.Join(validation.Errors, "；"), map[string]any{
			"scene_count": validation.SceneCount,
		})
		return projectDetailResponse{}, &storyboardValidationError{Errors: validation.Errors}
	}

	sceneTasks, err := a.writeScenePlan(projectID, scenes, finishedAt)
	if err != nil {
		return projectDetailResponse{}, err
	}

	tasks[len(tasks)-1].Message = "Storyboard saved and validated"
	tasks = append(tasks, newTask("storyboard_validation", "", "success", fmt.Sprintf("Storyboard validation passed with %d scenes", len(scenes)), finishedAt))
	tasks[len(tasks)-1].FinishedAt = finishedAt
	tasks = append(tasks, newTask("scene_task_split", "", "success", fmt.Sprintf("Generated %d scene tasks", len(sceneTasks)), finishedAt))
	tasks[len(tasks)-1].FinishedAt = finishedAt
	tasks = append(tasks, sceneTasks...)

	project.Status = "scene_tasks_ready"
	project.StoryboardValid = true
	project.SceneCount = len(scenes)
	project.UpdatedAt = finishedAt
	if err := a.writeProject(project); err != nil {
		return projectDetailResponse{}, err
	}
	if err := a.writeTasks(projectID, tasks); err != nil {
		return projectDetailResponse{}, err
	}

	cycleCount, cycleReason := storyPlanCycleSummary(storyboardJSON)
	a.recordProjectEvent(projectID, "story", "done", fmt.Sprintf("故事线规划完成：%d 个周期", cycleCount), map[string]any{
		"cycle_count":  cycleCount,
		"cycle_reason": cycleReason,
	})
	a.recordProjectEvent(projectID, "storyboard", "done", fmt.Sprintf("分镜已生成并拆成 %d 页绘本", len(scenes)), map[string]any{
		"scene_count": len(scenes),
	})
	return a.getProjectDetail(projectID)
}

func (a *app) runZeroTokenStoryboard(project projectFile, req generateStoryboardRequest) ([]byte, error) {
	providerRef := defaultProviderRef(req.ProviderRef)
	timeoutMs := req.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 300000
	}
	browserProfileID := req.BrowserProfileID
	if browserProfileID == "" {
		browserProfileID = "chrome_main"
	}

	// 第一步：生成基础 storyboard
	baseStoryboardJSON, err := a.generateBaseStoryboard(project, providerRef, browserProfileID, timeoutMs)
	if err != nil {
		return nil, err
	}

	// 第二步：解析基础 storyboard
	var baseStoryboard map[string]any
	if err := json.Unmarshal(baseStoryboardJSON, &baseStoryboard); err != nil {
		return nil, fmt.Errorf("parse base storyboard: %w", err)
	}

	// 第三步：计算时长预算并补充关键帧
	enhancedStoryboard, err := a.enhanceStoryboardWithKeyframes(baseStoryboard, project)
	if err != nil {
		return nil, err
	}

	// 第四步：序列化增强后的 storyboard
	enhancedJSON, err := json.Marshal(enhancedStoryboard)
	if err != nil {
		return nil, fmt.Errorf("marshal enhanced storyboard: %w", err)
	}

	// 保存增强后的 storyboard
	normalized, err := normalizeJSON(enhancedJSON)
	if err != nil {
		return nil, err
	}
	if writeErr := os.WriteFile(a.storyboardExtractedPath(project.ProjectID), append(normalized, '\n'), 0o644); writeErr != nil {
		log.Printf("write storyboard extracted json failed for %s: %v", project.ProjectID, writeErr)
	}

	return normalized, nil
}

func (a *app) zeroTokenBridgeHealthy(client *http.Client) bool {
	resp, err := client.Get(a.zeroTokenBridgeURL + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (a *app) startZeroTokenBridgeLocked() error {
	cmd := exec.Command("node", a.zeroTokenBridgePath)
	cmd.Dir = a.rootDir
	cmd.Env = append(os.Environ(), "ZERO_TOKEN_BRIDGE_PORT="+a.zeroTokenBridgePort)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	a.zeroTokenBridgeCmd = cmd
	go func() {
		err := cmd.Wait()
		a.zeroTokenBridgeMu.Lock()
		if a.zeroTokenBridgeCmd == cmd {
			a.zeroTokenBridgeCmd = nil
		}
		a.zeroTokenBridgeMu.Unlock()
		if err != nil {
			log.Printf("zero-token bridge server exited: %v", err)
		}
	}()
	return nil
}

func (a *app) ensureZeroTokenBridge(ctx context.Context) error {
	if !fileExists(a.zeroTokenBridgePath) {
		return fmt.Errorf("zero-token bridge server not found at %s; run npm run build first", a.zeroTokenBridgePath)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	if a.zeroTokenBridgeHealthy(client) {
		return nil
	}

	a.zeroTokenBridgeMu.Lock()
	defer a.zeroTokenBridgeMu.Unlock()
	if a.zeroTokenBridgeHealthy(client) {
		return nil
	}
	if a.zeroTokenBridgeCmd == nil {
		if err := a.startZeroTokenBridgeLocked(); err != nil {
			return fmt.Errorf("start zero-token bridge server: %w", err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if a.zeroTokenBridgeHealthy(client) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("zero-token bridge server at %s did not become healthy", a.zeroTokenBridgeURL)
}

func (a *app) runZeroTokenGenerate(ctx context.Context, payload zeroTokenBridgePayload, timeoutMs int) (bridgeResponse, error) {
	if timeoutMs <= 0 {
		timeoutMs = 300000
	}
	if err := a.ensureZeroTokenBridge(ctx); err != nil {
		return bridgeResponse{}, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return bridgeResponse{}, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs+30000)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, a.zeroTokenBridgeURL+"/generate", bytes.NewReader(body))
	if err != nil {
		return bridgeResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return bridgeResponse{}, fmt.Errorf("call zero-token bridge server: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return bridgeResponse{}, fmt.Errorf("read zero-token bridge response: %w", err)
	}

	var bridgeResp bridgeResponse
	if err := json.Unmarshal(raw, &bridgeResp); err != nil {
		return bridgeResponse{}, fmt.Errorf("decode zero-token bridge response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if bridgeResp.Error != "" {
			return bridgeResponse{}, fmt.Errorf("zero-token bridge error: %s %s", bridgeResp.Name, bridgeResp.Error)
		}
		return bridgeResponse{}, fmt.Errorf("zero-token bridge server returned %d", resp.StatusCode)
	}
	if !bridgeResp.OK {
		return bridgeResponse{}, fmt.Errorf("zero-token bridge error: %s %s", bridgeResp.Name, bridgeResp.Error)
	}
	return bridgeResp, nil
}

func (a *app) generateBaseStoryboard(project projectFile, providerRef, browserProfileID string, timeoutMs int) ([]byte, error) {
	normalized, rawText, err := a.generateCodexStoryboardJSON(project, timeoutMs)
	if err != nil {
		return nil, err
	}
	if writeErr := a.writeStoryboardDebugOutput(project.ProjectID, zeroTokenGenerateOutput{Text: rawText, JSON: normalized}); writeErr != nil {
		log.Printf("write storyboard debug output failed for %s: %v", project.ProjectID, writeErr)
	}
	return normalized, nil
}

type codexExecOutput struct {
	Text     string
	ThreadID string
	Errors   []string
	Usage    any
}

func (a *app) generateCodexStoryboardJSON(project projectFile, timeoutMs int) ([]byte, string, error) {
	output, err := a.runCodexStoryboardExec(project, timeoutMs, false)
	if err != nil {
		return nil, "", err
	}
	text := strings.TrimSpace(output.Text)
	if text == "" {
		return nil, "", errors.New("codex storyboard planner returned empty output")
	}
	normalized, err := normalizeJSON([]byte(text))
	if err == nil {
		return normalized, text, nil
	}
	extracted, err := extractJSONObject(text)
	if err != nil {
		return nil, text, fmt.Errorf("parse codex storyboard JSON: %w", err)
	}
	return extracted, text, nil
}

func (a *app) runCodexStoryboardExec(project projectFile, timeoutMs int, useOutputSchema bool) (codexExecOutput, error) {
	if timeoutMs <= 0 {
		timeoutMs = 600000
	}
	codexBin, binSource, err := resolveCodexCLIBin()
	if err != nil {
		return codexExecOutput{}, err
	}

	workDir, err := os.MkdirTemp("", "story-video-codex-story-")
	if err != nil {
		return codexExecOutput{}, err
	}
	defer func() {
		_ = os.RemoveAll(workDir)
	}()

	args := []string{"exec", "--json"}
	if useOutputSchema {
		schemaPath := filepath.Join(workDir, "storyboard.schema.json")
		if err := os.WriteFile(schemaPath, []byte(codexStoryboardOutputSchema()), 0o644); err != nil {
			return codexExecOutput{}, err
		}
		args = append(args, "--output-schema", schemaPath)
	}
	args = append(args,
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
		buildBaseStoryboardPrompt(project),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs+30000)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, codexBin, args...)
	cmd.Dir = workDir
	cmd.Env = codexCommandEnv()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return codexExecOutput{}, fmt.Errorf("open codex storyboard stdout: %w", err)
	}

	a.recordProjectEvent(project.ProjectID, "story", "running", fmt.Sprintf("Codex CLI 已启动：%s=%s", binSource, codexBin), nil)
	if err := cmd.Start(); err != nil {
		return codexExecOutput{}, fmt.Errorf("start codex storyboard planner from %s=%s: %w", binSource, codexBin, err)
	}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		stdout.WriteString(line)
		stdout.WriteByte('\n')
		a.recordCodexStoryboardEvent(project.ProjectID, line)
	}
	scanErr := scanner.Err()
	err = cmd.Wait()
	parsed := parseCodexExecJSONL(stdout.String())
	if ctx.Err() == context.DeadlineExceeded {
		return parsed, fmt.Errorf("codex storyboard planner timed out after %dms", timeoutMs)
	}
	if scanErr != nil {
		return parsed, fmt.Errorf("read codex storyboard stream: %w", scanErr)
	}
	if err != nil {
		return parsed, fmt.Errorf("codex storyboard planner failed from %s=%s: %w; stderr=%s; stdout=%s", binSource, codexBin, err, tailString(stderr.String(), 1600), tailString(stdout.String(), 1600))
	}
	if len(parsed.Errors) > 0 && strings.TrimSpace(parsed.Text) == "" {
		return parsed, fmt.Errorf("codex storyboard planner returned errors: %s", strings.Join(parsed.Errors, "; "))
	}
	return parsed, nil
}

func resolveCodexCLIBin() (string, string, error) {
	if value := strings.TrimSpace(os.Getenv("ZERO_TOKEN_CODEX_PATH")); value != "" {
		if commandRefExists(value) {
			return value, "ZERO_TOKEN_CODEX_PATH", nil
		}
		return "", "ZERO_TOKEN_CODEX_PATH", fmt.Errorf("codex cli path does not exist: %s", value)
	}
	if value := strings.TrimSpace(os.Getenv("ZERO_TOKEN_CODEX_BIN")); value != "" {
		if commandRefExists(value) {
			return value, "ZERO_TOKEN_CODEX_BIN", nil
		}
		return "", "ZERO_TOKEN_CODEX_BIN", fmt.Errorf("codex cli binary does not exist: %s", value)
	}
	if path := lookupCommand("codex"); path != "" {
		return path, "PATH", nil
	}
	return "", "PATH", errors.New("codex cli not found; set ZERO_TOKEN_CODEX_PATH or ZERO_TOKEN_CODEX_BIN")
}

func codexCommandEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, item := range env {
		if strings.HasPrefix(item, "CODEX_CLI=") || strings.HasPrefix(item, "CLAUDECODE=") || strings.HasPrefix(item, "CLAUDE_CODE_ENTRYPOINT=") {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func parseCodexExecJSONL(raw string) codexExecOutput {
	var output codexExecOutput
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if eventType, _ := event["type"].(string); eventType == "thread.started" {
			output.ThreadID, _ = event["thread_id"].(string)
		}
		if eventType, _ := event["type"].(string); eventType == "turn.completed" {
			output.Usage = event["usage"]
		}
		if eventType, _ := event["type"].(string); eventType == "error" {
			if message, _ := event["message"].(string); message != "" {
				output.Errors = append(output.Errors, message)
			} else if message, _ := event["error"].(string); message != "" {
				output.Errors = append(output.Errors, message)
			}
		}
		if eventType, _ := event["type"].(string); eventType != "item.completed" {
			continue
		}
		item, ok := event["item"].(map[string]any)
		if !ok {
			continue
		}
		if text := extractCodexItemText(item); text != "" {
			output.Text += text
		}
	}
	return output
}

func (a *app) recordCodexStoryboardEvent(projectID string, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return
	}
	eventType, _ := event["type"].(string)
	switch eventType {
	case "thread.started":
		threadID, _ := event["thread_id"].(string)
		a.recordProjectEvent(projectID, "story", "running", "Codex 会话已创建，开始规划故事", map[string]any{
			"thread_id": threadID,
		})
	case "turn.started":
		a.recordProjectEvent(projectID, "story", "running", "Codex 正在构思故事线、角色和绘本页节奏", nil)
	case "item.started":
		item, _ := event["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		if itemType == "command_execution" {
			a.recordProjectEvent(projectID, "story", "running", "Codex 开始执行辅助命令", map[string]any{
				"command": item["command"],
			})
		}
	case "item.completed":
		item, _ := event["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		switch itemType {
		case "agent_message":
			text := extractCodexItemText(item)
			a.recordProjectEvent(projectID, "story", "running", "Codex 已返回故事规划草稿，正在解析结构化 JSON", map[string]any{
				"chars": utf8.RuneCountInString(text),
			})
		case "reasoning":
			if text := strings.TrimSpace(extractCodexItemText(item)); text != "" {
				a.recordProjectEvent(projectID, "story", "running", "Codex 正在推理故事结构："+tailString(text, 120), nil)
			}
		case "command_execution":
			a.recordProjectEvent(projectID, "story", "running", "Codex 辅助命令执行完成", map[string]any{
				"exit_code": item["exit_code"],
			})
		}
	case "turn.completed":
		a.recordProjectEvent(projectID, "story", "done", "Codex 故事规划回合完成", map[string]any{
			"usage": event["usage"],
		})
	case "error":
		message, _ := event["message"].(string)
		if message == "" {
			message, _ = event["error"].(string)
		}
		if message == "" {
			message = "Codex 返回未知错误"
		}
		a.recordProjectEvent(projectID, "story", "error", message, nil)
	}
}

func extractCodexItemText(item map[string]any) string {
	itemType, _ := item["type"].(string)
	if itemType == "image_generation" {
		return ""
	}
	if text, _ := item["text"].(string); text != "" {
		return text
	}
	if result, _ := item["result"].(string); result != "" {
		return result
	}
	if contentItems, ok := item["content"].([]any); ok {
		var parts []string
		for _, contentItem := range contentItems {
			contentMap, ok := contentItem.(map[string]any)
			if !ok {
				continue
			}
			if text, _ := contentMap["text"].(string); text != "" {
				parts = append(parts, text)
			}
			if text, _ := contentMap["content"].(string); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func tailString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

func codexStoryboardOutputSchema() string {
	return `{
  "type": "object",
  "required": ["meta", "project", "story_plan", "global_style", "character_bible", "audio_profile", "video_profile", "render_rules", "scenes"],
  "properties": {
    "meta": { "type": "object" },
    "project": { "type": "object" },
    "story_plan": {
      "type": "object",
      "required": ["cycle_count", "cycle_reason", "cycles"],
      "properties": {
        "cycle_count": { "type": "integer", "enum": [3, 5] },
        "cycle_reason": { "type": "string" },
        "cycles": { "type": "array", "minItems": 3, "maxItems": 5, "items": { "type": "object" } }
      }
    },
    "global_style": { "type": "object" },
    "character_bible": { "type": ["object", "array"] },
    "audio_profile": { "type": "object" },
    "video_profile": { "type": "object" },
    "render_rules": { "type": "object" },
    "scenes": {
      "type": "array",
      "minItems": 3,
      "maxItems": 5,
      "items": {
        "type": "object",
        "required": ["scene_id", "sequence", "title", "story_function", "narration", "subtitle", "duration_hint_sec", "characters", "objects", "environment", "visual", "prompt", "audio", "effects"],
        "properties": {
          "scene_id": { "type": "string" },
          "sequence": { "type": "integer", "minimum": 1 },
          "title": { "type": "string" },
          "story_function": { "type": "string" },
          "narration": { "type": "string" },
          "subtitle": { "type": "string" },
          "duration_hint_sec": { "type": "integer", "minimum": 1 },
          "characters": { "type": "array", "items": { "type": "string" } },
          "objects": { "type": "array", "items": { "type": "string" } },
          "environment": { "type": "object" },
          "visual": { "type": "object" },
          "prompt": { "type": "object" },
          "audio": { "type": "object" },
          "effects": { "type": "object" },
          "keyframes": { "type": "array", "items": { "type": "object" } }
        }
      }
    }
  }
}`
}

func (a *app) enhanceStoryboardWithKeyframes(storyboard map[string]any, project projectFile) (map[string]any, error) {
	scenes, ok := storyboard["scenes"].([]any)
	if !ok {
		return storyboard, nil
	}

	enhancedScenes := make([]any, 0, len(scenes))
	for i, scene := range scenes {
		sceneMap, ok := scene.(map[string]any)
		if !ok {
			enhancedScenes = append(enhancedScenes, scene)
			continue
		}

		sceneDuration := a.calculateSceneDuration(sceneMap)
		imageCount := a.calculateImageCount(sceneDuration, project.ImageSwitchIntervalSec)

		if imageCount > 1 {
			enhancedScene, err := a.generateKeyframesForScene(sceneMap, imageCount, project, storyboard)
			if err != nil {
				log.Printf("generate keyframes for scene %d failed: %v", i, err)
				enhancedScenes = append(enhancedScenes, scene)
				continue
			}
			enhancedScenes = append(enhancedScenes, enhancedScene)
		} else {
			enhancedScenes = append(enhancedScenes, scene)
		}
	}

	storyboard["scenes"] = enhancedScenes
	return storyboard, nil
}

func (a *app) calculateSceneDuration(scene map[string]any) float64 {
	if durationHint, ok := requiredPositiveIntField(scene, "duration_hint_sec"); ok && durationHint > 0 {
		return float64(durationHint)
	}
	narration, _ := requiredStringField(scene, "narration")
	charCount := utf8.RuneCountInString(strings.TrimSpace(narration))
	if charCount <= 0 {
		return 3.0
	}
	return float64(charCount) / baseCharsPerSec
}

func (a *app) calculateImageCount(sceneDuration float64, switchInterval int) int {
	if switchInterval <= 0 {
		switchInterval = 3 // 默认 3 秒
	}

	imageCount := int(math.Ceil(sceneDuration / float64(switchInterval)))

	// 限制关键帧数量在 1-6 之间
	if imageCount < 1 {
		imageCount = 1
	} else if imageCount > 20 {
		imageCount = 20
	}

	return imageCount
}

func (a *app) generateKeyframesForScene(scene map[string]any, imageCount int, project projectFile, storyboard map[string]any) (map[string]any, error) {
	aspectRatio := project.AspectRatio
	if aspectRatio == "" {
		aspectRatio = "9:16"
	}
	scene["keyframes"] = buildDeterministicKeyframes(scene, imageCount, aspectRatio)
	return scene, nil
}

func buildDeterministicKeyframes(scene map[string]any, imageCount int, aspectRatio string) []any {
	if imageCount < 1 {
		imageCount = 1
	}
	sceneID, _ := requiredStringField(scene, "scene_id")
	if sceneID == "" {
		sceneID = "scene"
	}
	characters, _ := requiredStringSliceField(scene, "characters")
	prompt, _ := requiredObjectField(scene, "prompt")
	visual, _ := requiredObjectField(scene, "visual")
	narration, _ := requiredStringField(scene, "narration")
	narrationParts := splitNarrationForKeyframes(narration, imageCount)
	subjectPrompt := stringFieldOrFallback(prompt, "subject_prompt", "保持 storyboard 中角色圣经的稳定外貌锚点")
	scenePrompt := stringFieldOrFallback(prompt, "scene_prompt", "延续当前场景环境、镜头和氛围")
	shotType := stringFieldOrFallback(visual, "shot_type", "中景")
	cameraMotion := stringFieldOrFallback(visual, "camera_motion", "轻微推进")
	composition := stringFieldOrFallback(visual, "composition", "主体清晰，画面留出字幕空间")
	action := stringFieldOrFallback(visual, "action", "角色完成当前场景动作")

	keyframes := make([]any, 0, imageCount)
	for index := 0; index < imageCount; index++ {
		phase := keyframePhaseName(index, imageCount)
		narrationBeat := ""
		if index < len(narrationParts) {
			narrationBeat = narrationParts[index]
		}
		keyframes = append(keyframes, map[string]any{
			"frame_id":   fmt.Sprintf("%s_f%02d", sceneID, index+1),
			"sequence":   index + 1,
			"characters": characters,
			"prompt": map[string]any{
				"subject_prompt": subjectPrompt,
				"scene_prompt":   fmt.Sprintf("%s。关键帧 %d/%d：%s；旁白片段：%s；画面比例 %s，构图必须稳定适配。", scenePrompt, index+1, imageCount, phase, fallbackString(narrationBeat, "延续当前动作"), aspectRatio),
				"full_prompt":    "",
			},
			"visual": map[string]any{
				"shot_type":     shotType,
				"camera_motion": cameraMotion,
				"composition":   composition,
				"action":        fmt.Sprintf("%s；关键帧阶段：%s", action, phase),
			},
		})
	}
	return keyframes
}

func splitNarrationForKeyframes(narration string, count int) []string {
	if count <= 0 {
		return nil
	}
	chunks := splitSubtitleText(narration, maxInt(12, utf8.RuneCountInString(strings.TrimSpace(narration))/count))
	if len(chunks) == 0 {
		return nil
	}
	if len(chunks) >= count {
		return chunks[:count]
	}
	for len(chunks) < count {
		chunks = append(chunks, chunks[len(chunks)-1])
	}
	return chunks
}

func keyframePhaseName(index int, count int) string {
	if count <= 1 {
		return "定格展示当前场景核心情绪"
	}
	if index == 0 {
		return "起始画面，交代角色位置和环境"
	}
	if index == count-1 {
		return "收束画面，突出这一幕的结果和情绪"
	}
	return "推进画面，表现动作变化和情绪递进"
}

func stringFieldOrFallback(obj map[string]any, key string, fallback string) string {
	if obj == nil {
		return fallback
	}
	value, ok := requiredStringField(obj, key)
	if !ok {
		return fallback
	}
	return value
}

func (a *app) writeStoryboardDebugOutput(projectID string, output zeroTokenGenerateOutput) error {
	if err := os.WriteFile(a.storyboardRawTextPath(projectID), []byte(output.Text), 0o644); err != nil {
		return err
	}
	trimmedJSON := bytes.TrimSpace(output.JSON)
	if len(trimmedJSON) == 0 || string(trimmedJSON) == "null" {
		return nil
	}
	rawJSON, err := normalizeJSON(trimmedJSON)
	if err != nil {
		rawJSON = append([]byte{}, trimmedJSON...)
	}
	return os.WriteFile(a.storyboardRawJSONPath(projectID), append(rawJSON, '\n'), 0o644)
}

func (a *app) readProject(projectID string) (projectFile, error) {
	var project projectFile
	err := readJSONFile(filepath.Join(a.projectsDir, projectID, "project.json"), &project)
	return project, err
}

func (a *app) writeProject(project projectFile) error {
	return writeJSONFile(filepath.Join(a.projectsDir, project.ProjectID, "project.json"), project)
}

func (a *app) readTasks(projectID string) ([]taskFile, error) {
	var tasks []taskFile
	err := readJSONFile(filepath.Join(a.projectsDir, projectID, "tasks.json"), &tasks)
	return tasks, err
}

func (a *app) writeTasks(projectID string, tasks []taskFile) error {
	return writeJSONFile(filepath.Join(a.projectsDir, projectID, "tasks.json"), tasks)
}

func (a *app) projectEventsPath(projectID string) string {
	return filepath.Join(a.projectsDir, projectID, "events.jsonl")
}

func (a *app) recordProjectEvent(projectID string, phase string, status string, message string, details map[string]any) {
	if err := a.appendProjectEvent(projectID, phase, status, message, details); err != nil {
		log.Printf("write project event failed for %s: %v", projectID, err)
	}
}

func (a *app) appendProjectEvent(projectID string, phase string, status string, message string, details map[string]any) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("project id is empty")
	}
	if phase == "" {
		phase = "pipeline"
	}
	if status == "" {
		status = "info"
	}

	a.eventsMu.Lock()
	defer a.eventsMu.Unlock()

	projectDir := filepath.Join(a.projectsDir, projectID)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}
	eventsPath := a.projectEventsPath(projectID)
	seq, err := countJSONLLines(eventsPath)
	if err != nil {
		return err
	}
	event := projectEvent{
		Seq:       seq + 1,
		ProjectID: projectID,
		Phase:     phase,
		Status:    status,
		Message:   message,
		Details:   compactEventDetails(details),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if sceneID, _ := event.Details["scene_id"].(string); sceneID != "" {
		event.SceneID = sceneID
	}
	file, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetEscapeHTML(false)
	return enc.Encode(event)
}

func compactEventDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	cleaned := make(map[string]any, len(details))
	for key, value := range details {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		cleaned[key] = value
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func countJSONLLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func (a *app) readProjectEvents(projectID string, after int, limit int) ([]projectEvent, error) {
	if _, err := a.readProject(projectID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 300 {
		limit = 120
	}
	eventsPath := a.projectEventsPath(projectID)
	file, err := os.Open(eventsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []projectEvent{}, nil
		}
		return nil, err
	}
	defer file.Close()

	events := make([]projectEvent, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event projectEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Seq <= after {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

func (a *app) storyboardValidationPath(projectID string) string {
	return filepath.Join(a.projectsDir, projectID, "storyboard.validation.json")
}

func (a *app) storyboardRawTextPath(projectID string) string {
	return filepath.Join(a.projectsDir, projectID, "storyboard.raw.txt")
}

func (a *app) storyboardRawJSONPath(projectID string) string {
	return filepath.Join(a.projectsDir, projectID, "storyboard.raw.json")
}

func (a *app) storyboardExtractedPath(projectID string) string {
	return filepath.Join(a.projectsDir, projectID, "storyboard.extracted.json")
}

func (a *app) sceneIndexPath(projectID string) string {
	return filepath.Join(a.projectsDir, projectID, "scenes", "index.json")
}

func (a *app) readStoryboardValidation(projectID string) (storyboardValidationResult, error) {
	var result storyboardValidationResult
	path := a.storyboardValidationPath(projectID)
	if !fileExists(path) {
		return result, nil
	}
	err := readJSONFile(path, &result)
	return result, err
}

func (a *app) writeStoryboardValidation(projectID string, result storyboardValidationResult) error {
	return writeJSONFile(a.storyboardValidationPath(projectID), result)
}

func (a *app) readSceneFiles(projectID string) ([]sceneFile, error) {
	var scenes []sceneFile
	path := a.sceneIndexPath(projectID)
	if !fileExists(path) {
		return []sceneFile{}, nil
	}
	if err := readJSONFile(path, &scenes); err != nil {
		return nil, err
	}
	return scenes, nil
}

func (a *app) listScenes(projectID string) ([]sceneFile, error) {
	if _, err := a.readProject(projectID); err != nil {
		return nil, err
	}
	return a.readSceneFiles(projectID)
}

func (a *app) scenePath(projectID string, sceneID string) string {
	return filepath.Join(a.projectsDir, projectID, "scenes", sceneID+".json")
}

func (a *app) readSceneFile(projectID string, sceneID string) (sceneFile, error) {
	var scene sceneFile
	err := readJSONFile(a.scenePath(projectID, sceneID), &scene)
	return scene, err
}

func (a *app) writeSceneFile(projectID string, scene sceneFile) error {
	if err := writeJSONFile(a.scenePath(projectID, scene.SceneID), scene); err != nil {
		return err
	}
	scenes, err := a.readSceneFiles(projectID)
	if err != nil {
		return err
	}
	updated := false
	for index := range scenes {
		if scenes[index].SceneID == scene.SceneID {
			scenes[index] = scene
			updated = true
			break
		}
	}
	if !updated {
		scenes = append(scenes, scene)
	}
	return writeJSONFile(a.sceneIndexPath(projectID), scenes)
}

func (a *app) getSceneDetail(projectID string, sceneID string) (sceneFile, error) {
	if _, err := a.readProject(projectID); err != nil {
		return sceneFile{}, err
	}
	return a.readSceneFile(projectID, sceneID)
}

func (a *app) resetSceneFiles(projectID string) error {
	sceneDir := filepath.Join(a.projectsDir, projectID, "scenes")
	if err := os.MkdirAll(sceneDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(sceneDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(sceneDir, entry.Name())); err != nil {
			return err
		}
	}
	return writeJSONFile(a.sceneIndexPath(projectID), []sceneFile{})
}

func (a *app) writeScenePlan(projectID string, scenes []sceneFile, now string) ([]taskFile, error) {
	if err := a.resetSceneFiles(projectID); err != nil {
		return nil, err
	}

	sceneDir := filepath.Join(a.projectsDir, projectID, "scenes")
	tasks := make([]taskFile, 0, len(scenes)*3)
	for _, scene := range scenes {
		path := filepath.Join(sceneDir, scene.SceneID+".json")
		if err := writeJSONFile(path, scene); err != nil {
			return nil, err
		}
		tasks = append(tasks,
			newTask("scene_keyframe_prompt_generation", scene.SceneID, keyframePromptTaskStatus(scene), "Scene keyframe prompt generation queued", now),
		)
		// 为每个关键帧创建单独的图片生成任务
		if len(scene.Keyframes) > 0 {
			for i := range scene.Keyframes {
				taskID := fmt.Sprintf("%s_kf%d", scene.SceneID, i)
				tasks = append(tasks,
					newTask("scene_image_generation", taskID, "pending", fmt.Sprintf("Scene keyframe %d image generation queued", i+1), now),
				)
			}
		} else {
			// 保持原有逻辑，为没有关键帧的场景创建单个图片生成任务
			tasks = append(tasks,
				newTask("scene_image_generation", scene.SceneID, "pending", "Scene image generation queued", now),
			)
		}
		// 音频和视频合成任务保持不变
		tasks = append(tasks,
			newTask("scene_audio_generation", scene.SceneID, "pending", "Scene audio generation queued", now),
			newTask("scene_video_compositing", scene.SceneID, "pending", "Scene video compositing queued", now),
		)
	}
	if err := writeJSONFile(a.sceneIndexPath(projectID), scenes); err != nil {
		return nil, err
	}
	return tasks, nil
}

func keyframePromptTaskStatus(scene sceneFile) string {
	if len(scene.Keyframes) > 0 {
		return "success"
	}
	return "pending"
}

func sceneToStoryboardMap(scene sceneFile) map[string]any {
	sceneMap := map[string]any{
		"scene_id":          scene.SceneID,
		"sequence":          scene.Sequence,
		"title":             scene.Title,
		"story_function":    scene.StoryFunction,
		"narration":         scene.Narration,
		"subtitle":          scene.Subtitle,
		"duration_hint_sec": scene.DurationHintSec,
		"characters":        scene.Characters,
		"objects":           scene.Objects,
		"environment":       scene.Environment,
		"visual":            scene.Visual,
		"prompt":            scene.Prompt,
		"audio":             scene.Audio,
		"effects":           scene.Effects,
	}
	if len(scene.Keyframes) > 0 {
		keyframes := make([]map[string]any, 0, len(scene.Keyframes))
		for _, keyframe := range scene.Keyframes {
			keyframes = append(keyframes, map[string]any{
				"frame_id":   keyframe.FrameID,
				"sequence":   keyframe.Sequence,
				"characters": keyframe.Characters,
				"prompt":     keyframe.Prompt,
				"visual":     keyframe.Visual,
			})
		}
		sceneMap["keyframes"] = keyframes
	}
	return sceneMap
}

func clearKeyframeGeneratedMedia(keyframe *keyframe) {
	keyframe.ImageCandidates = nil
	keyframe.SelectedImageIdx = 0
	keyframe.ImageLocalPath = ""
	keyframe.ImageURL = ""
	keyframe.ImagePreviewURL = ""
	keyframe.ImageMimeType = ""
	keyframe.ImageError = ""
	keyframe.ImageGeneratedAt = ""
}

func clearSceneGeneratedImages(scene *sceneFile) {
	scene.ImageProviderRef = ""
	scene.ImagePrompt = ""
	scene.ImageCandidates = nil
	scene.SelectedImageIdx = 0
	scene.ImageURL = ""
	scene.ImageLocalPath = ""
	scene.ImagePreviewURL = ""
	scene.ImageMimeType = ""
	scene.ImageError = ""
	scene.ImageGeneratedAt = ""
	for index := range scene.Keyframes {
		clearKeyframeGeneratedMedia(&scene.Keyframes[index])
	}
}

func parseKeyframesFromAny(items []any) []keyframe {
	keyframes := make([]keyframe, 0, len(items))
	for _, item := range items {
		kfMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		frameID, _ := requiredStringField(kfMap, "frame_id")
		sequence, _ := requiredPositiveIntField(kfMap, "sequence")
		characters, _ := requiredStringSliceField(kfMap, "characters")
		prompt, _ := requiredObjectField(kfMap, "prompt")
		visual, _ := requiredObjectField(kfMap, "visual")
		keyframes = append(keyframes, keyframe{
			FrameID:    frameID,
			Sequence:   sequence,
			Characters: characters,
			Prompt:     prompt,
			Visual:     visual,
		})
	}
	return keyframes
}

func upsertSceneKeyframeTasks(tasks []taskFile, scene sceneFile, now string) []taskFile {
	filtered := make([]taskFile, 0, len(tasks))
	for _, task := range tasks {
		if task.Kind != "scene_image_generation" {
			filtered = append(filtered, task)
			continue
		}
		if task.SceneID == scene.SceneID || strings.HasPrefix(task.SceneID, scene.SceneID+"_kf") {
			continue
		}
		filtered = append(filtered, task)
	}
	if len(scene.Keyframes) > 0 {
		for i := range scene.Keyframes {
			taskID := fmt.Sprintf("%s_kf%d", scene.SceneID, i)
			filtered = append(filtered, newTask("scene_image_generation", taskID, "pending", fmt.Sprintf("Scene keyframe %d image generation queued", i+1), now))
		}
	} else {
		filtered = append(filtered, newTask("scene_image_generation", scene.SceneID, "pending", "Scene image generation queued", now))
	}
	return filtered
}

func (a *app) generateSceneImage(projectID string, sceneID string, req generateSceneImageRequest) (sceneFile, error) {
	project, err := a.readProject(projectID)
	if err != nil {
		return sceneFile{}, err
	}
	if !project.StoryboardValid || project.StoryboardPath == "" {
		return sceneFile{}, errors.New("storyboard is not ready or not valid")
	}

	// 解析 sceneID，判断是否是关键帧任务
	var baseSceneID string
	var keyframeIndex int
	if strings.Contains(sceneID, "_kf") {
		parts := strings.Split(sceneID, "_kf")
		baseSceneID = parts[0]
		keyframeIndex, _ = strconv.Atoi(parts[1])
	} else {
		baseSceneID = sceneID
		keyframeIndex = -1
	}

	scene, err := a.readSceneFile(projectID, baseSceneID)
	if err != nil {
		return sceneFile{}, err
	}

	// 检查是否是关键帧任务
	if keyframeIndex >= 0 && keyframeIndex < len(scene.Keyframes) {
		// 关键帧任务
		if scene.Keyframes[keyframeIndex].ImageLocalPath != "" && !req.Force {
			a.recordProjectEvent(projectID, "image", "skipped", fmt.Sprintf("关键帧图片已存在：%s 第 %d 张", sceneDisplayName(scene), keyframeIndex+1), map[string]any{
				"scene_id":      scene.SceneID,
				"task_scene_id": sceneID,
			})
			return scene, nil
		}
	} else if keyframeIndex == -1 {
		// 普通场景任务
		if scene.ImageStatus == "success" && !req.Force {
			a.recordProjectEvent(projectID, "image", "skipped", "图片已存在："+sceneDisplayName(scene), map[string]any{
				"scene_id": scene.SceneID,
			})
			return scene, nil
		}
	}

	tasks, err := a.readTasks(projectID)
	if err != nil {
		return sceneFile{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if keyframeIndex >= 0 && keyframeIndex < len(scene.Keyframes) {
		// 关键帧任务状态更新
		scene.Status = "image_generating"
		scene.UpdatedAt = now
	} else {
		// 普通场景任务状态更新
		scene.Status = "image_generating"
		scene.ImageStatus = "running"
		scene.ImageError = ""
		scene.UpdatedAt = now
	}
	if writeErr := a.writeSceneFile(projectID, scene); writeErr != nil {
		return sceneFile{}, writeErr
	}
	tasks = upsertTask(tasks, newTask("scene_image_generation", sceneID, "running", "Generating scene image", now))
	if writeErr := a.writeTasks(projectID, tasks); writeErr != nil {
		return sceneFile{}, writeErr
	}
	imageMessage := "Codex/image 正在生成绘本图：" + sceneDisplayName(scene)
	if keyframeIndex >= 0 {
		imageMessage = fmt.Sprintf("Codex/image 正在生成关键帧：%s 第 %d 张", sceneDisplayName(scene), keyframeIndex+1)
	}
	a.recordProjectEvent(projectID, "image", "running", imageMessage, map[string]any{
		"scene_id":      scene.SceneID,
		"task_scene_id": sceneID,
		"provider_ref":  resolveImageProviderRef(project.ProviderRef, req.ProviderRef),
	})

	storyboardRoot, err := a.readStoryboardRoot(project)
	if err != nil {
		return sceneFile{}, err
	}

	var prompt string
	if keyframeIndex >= 0 && keyframeIndex < len(scene.Keyframes) {
		// 为关键帧生成提示词
		keyframe := scene.Keyframes[keyframeIndex]
		prompt = buildKeyframeImagePrompt(storyboardRoot, scene, keyframe)
	} else {
		// 为普通场景生成提示词
		prompt = buildSceneImagePrompt(storyboardRoot, scene)
	}

	images, err := a.runZeroTokenSceneImage(project, sceneID, req, prompt, resolveSceneAspectRatio(storyboardRoot))
	if err != nil {
		failedAt := time.Now().UTC().Format(time.RFC3339)
		if keyframeIndex >= 0 && keyframeIndex < len(scene.Keyframes) {
			// 关键帧任务失败
			scene.Status = "scene_tasks_ready"
			scene.UpdatedAt = failedAt
		} else {
			// 普通场景任务失败
			scene.Status = "scene_tasks_ready"
			scene.ImageStatus = "failed"
			scene.ImageError = err.Error()
			scene.UpdatedAt = failedAt
		}
		_ = a.writeSceneFile(projectID, scene)
		tasks = updateTaskStatus(tasks, "scene_image_generation", sceneID, "failed", "Scene image generation failed", err.Error(), failedAt)
		_ = a.writeTasks(projectID, tasks)
		a.recordProjectEvent(projectID, "image", "error", "Codex/image 出图失败："+err.Error(), map[string]any{
			"scene_id":      scene.SceneID,
			"task_scene_id": sceneID,
		})
		return sceneFile{}, err
	}

	candidates, err := a.persistGeneratedImages(projectID, sceneID, images)
	if err != nil {
		failedAt := time.Now().UTC().Format(time.RFC3339)
		if keyframeIndex >= 0 && keyframeIndex < len(scene.Keyframes) {
			// 关键帧任务失败
			scene.Status = "scene_tasks_ready"
			scene.UpdatedAt = failedAt
		} else {
			// 普通场景任务失败
			scene.Status = "scene_tasks_ready"
			scene.ImageStatus = "failed"
			scene.ImageError = err.Error()
			scene.UpdatedAt = failedAt
		}
		_ = a.writeSceneFile(projectID, scene)
		tasks = updateTaskStatus(tasks, "scene_image_generation", sceneID, "failed", "Scene image download failed", err.Error(), failedAt)
		_ = a.writeTasks(projectID, tasks)
		a.recordProjectEvent(projectID, "image", "error", "图片保存失败："+err.Error(), map[string]any{
			"scene_id":      scene.SceneID,
			"task_scene_id": sceneID,
		})
		return sceneFile{}, err
	}

	finishedAt := time.Now().UTC().Format(time.RFC3339)
	if keyframeIndex >= 0 && keyframeIndex < len(scene.Keyframes) {
		// 关键帧任务成功
		if err := applySelectedKeyframeCandidate(&scene.Keyframes[keyframeIndex], candidates, 0); err != nil {
			return sceneFile{}, err
		}
		scene.Keyframes[keyframeIndex].ImageGeneratedAt = finishedAt
		scene.Keyframes[keyframeIndex].ImageError = ""
		scene.ImageGeneratedAt = finishedAt
		scene.ImageError = ""
		scene.Status = "image_ready"
		scene.UpdatedAt = finishedAt
		// 检查是否所有关键帧都已完成
		allKeyframesDone := true
		for _, kf := range scene.Keyframes {
			if kf.ImageLocalPath == "" {
				allKeyframesDone = false
				break
			}
		}
		if allKeyframesDone {
			scene.ImageStatus = "success"
		}
	} else {
		// 普通场景任务成功
		if err := applySelectedImageCandidate(&scene, candidates, 0); err != nil {
			return sceneFile{}, err
		}
		scene.Status = "image_ready"
		scene.ImageStatus = "success"
		scene.ImageProviderRef = resolveImageProviderRef(project.ProviderRef, req.ProviderRef)
		scene.ImagePrompt = prompt
		scene.ImageGeneratedAt = finishedAt
		scene.ImageError = ""
	}
	invalidateSceneDerivedMedia(&scene)
	if err := a.writeSceneFile(projectID, scene); err != nil {
		return sceneFile{}, err
	}

	tasks = updateTaskStatus(tasks, "scene_image_generation", sceneID, "success", "Scene image generated", "", finishedAt)
	if err := a.writeTasks(projectID, tasks); err != nil {
		return sceneFile{}, err
	}

	project.Status = "image_partial_ready"
	invalidateProjectFinalVideo(&project)
	project.UpdatedAt = finishedAt
	if err := a.writeProject(project); err != nil {
		return sceneFile{}, err
	}

	a.recordProjectEvent(projectID, "image", "done", "绘本图已保存："+sceneDisplayName(scene), map[string]any{
		"scene_id":      scene.SceneID,
		"task_scene_id": sceneID,
		"count":         len(candidates),
	})
	return scene, nil
}

func (a *app) generateSceneKeyframes(projectID string, sceneID string, req generateSceneKeyframesRequest) (sceneFile, error) {
	project, err := a.readProject(projectID)
	if err != nil {
		return sceneFile{}, err
	}
	if !project.StoryboardValid || project.StoryboardPath == "" {
		return sceneFile{}, errors.New("storyboard is not ready or not valid")
	}

	scene, err := a.readSceneFile(projectID, sceneID)
	if err != nil {
		return sceneFile{}, err
	}
	if len(scene.Keyframes) > 0 && !req.Force {
		a.recordProjectEvent(projectID, "keyframes", "skipped", "关键帧已存在："+sceneDisplayName(scene), map[string]any{
			"scene_id": scene.SceneID,
			"count":    len(scene.Keyframes),
		})
		return scene, nil
	}

	tasks, err := a.readTasks(projectID)
	if err != nil {
		return sceneFile{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tasks = upsertTask(tasks, newTask("scene_keyframe_prompt_generation", scene.SceneID, "running", "Generating scene keyframe prompts", now))
	if err := a.writeTasks(projectID, tasks); err != nil {
		return sceneFile{}, err
	}
	a.recordProjectEvent(projectID, "keyframes", "running", "正在计算图片切换节奏："+sceneDisplayName(scene), map[string]any{
		"scene_id": scene.SceneID,
	})

	storyboardRoot, err := a.readStoryboardRoot(project)
	if err != nil {
		return sceneFile{}, err
	}

	sceneMap := sceneToStoryboardMap(scene)
	imageCount := a.calculateImageCount(a.calculateSceneDuration(sceneMap), project.ImageSwitchIntervalSec)
	if imageCount <= 1 {
		finishedAt := time.Now().UTC().Format(time.RFC3339)
		scene.Keyframes = nil
		clearSceneGeneratedImages(&scene)
		scene.ImageStatus = "pending"
		invalidateSceneDerivedMedia(&scene)
		scene.UpdatedAt = finishedAt
		if err := a.writeSceneFile(projectID, scene); err != nil {
			return sceneFile{}, err
		}
		tasks = updateTaskStatus(tasks, "scene_keyframe_prompt_generation", scene.SceneID, "success", "Scene does not require keyframe prompts", "", finishedAt)
		tasks = upsertSceneKeyframeTasks(tasks, scene, finishedAt)
		if err := a.writeTasks(projectID, tasks); err != nil {
			return sceneFile{}, err
		}
		invalidateProjectFinalVideo(&project)
		project.UpdatedAt = finishedAt
		if err := a.writeProject(project); err != nil {
			return sceneFile{}, err
		}
		a.recordProjectEvent(projectID, "keyframes", "done", "该页时长较短，使用单张绘本图："+sceneDisplayName(scene), map[string]any{
			"scene_id": scene.SceneID,
		})
		return scene, nil
	}

	enhancedScene, err := a.generateKeyframesForScene(sceneMap, imageCount, project, storyboardRoot)
	if err != nil {
		failedAt := time.Now().UTC().Format(time.RFC3339)
		tasks = updateTaskStatus(tasks, "scene_keyframe_prompt_generation", scene.SceneID, "failed", "Scene keyframe prompt generation failed", err.Error(), failedAt)
		_ = a.writeTasks(projectID, tasks)
		a.recordProjectEvent(projectID, "keyframes", "error", "关键帧规划失败："+err.Error(), map[string]any{
			"scene_id": scene.SceneID,
		})
		return sceneFile{}, err
	}

	keyframesAny, ok := enhancedScene["keyframes"].([]any)
	if !ok {
		failedAt := time.Now().UTC().Format(time.RFC3339)
		err = errors.New("generated keyframes payload is invalid")
		tasks = updateTaskStatus(tasks, "scene_keyframe_prompt_generation", scene.SceneID, "failed", "Scene keyframe prompt generation failed", err.Error(), failedAt)
		_ = a.writeTasks(projectID, tasks)
		a.recordProjectEvent(projectID, "keyframes", "error", "关键帧规划返回格式无效", map[string]any{
			"scene_id": scene.SceneID,
		})
		return sceneFile{}, err
	}

	updatedScene := scene
	updatedScene.Keyframes = parseKeyframesFromAny(keyframesAny)
	if len(updatedScene.Keyframes) == 0 {
		failedAt := time.Now().UTC().Format(time.RFC3339)
		err = errors.New("generated keyframes payload did not contain valid keyframes")
		tasks = updateTaskStatus(tasks, "scene_keyframe_prompt_generation", scene.SceneID, "failed", "Scene keyframe prompt generation failed", err.Error(), failedAt)
		_ = a.writeTasks(projectID, tasks)
		a.recordProjectEvent(projectID, "keyframes", "error", "关键帧规划没有产出有效画面", map[string]any{
			"scene_id": scene.SceneID,
		})
		return sceneFile{}, err
	}

	clearSceneGeneratedImages(&updatedScene)
	updatedScene.ImageStatus = "pending"
	invalidateSceneDerivedMedia(&updatedScene)
	finishedAt := time.Now().UTC().Format(time.RFC3339)
	updatedScene.UpdatedAt = finishedAt
	if err := a.writeSceneFile(projectID, updatedScene); err != nil {
		return sceneFile{}, err
	}

	tasks = updateTaskStatus(tasks, "scene_keyframe_prompt_generation", scene.SceneID, "success", fmt.Sprintf("Generated %d keyframe prompts", len(keyframesAny)), "", finishedAt)
	tasks = upsertSceneKeyframeTasks(tasks, updatedScene, finishedAt)
	if err := a.writeTasks(projectID, tasks); err != nil {
		return sceneFile{}, err
	}

	invalidateProjectFinalVideo(&project)
	project.UpdatedAt = finishedAt
	if err := a.writeProject(project); err != nil {
		return sceneFile{}, err
	}

	a.recordProjectEvent(projectID, "keyframes", "done", fmt.Sprintf("已规划 %d 张切换图：%s", len(updatedScene.Keyframes), sceneDisplayName(updatedScene)), map[string]any{
		"scene_id": updatedScene.SceneID,
		"count":    len(updatedScene.Keyframes),
	})
	return updatedScene, nil
}

func (a *app) generateSceneAudio(projectID string, sceneID string, req generateSceneAudioRequest) (sceneFile, error) {
	project, err := a.readProject(projectID)
	if err != nil {
		return sceneFile{}, err
	}
	if !project.StoryboardValid || project.StoryboardPath == "" {
		return sceneFile{}, errors.New("storyboard is not ready or not valid")
	}

	scene, err := a.readSceneFile(projectID, sceneID)
	if err != nil {
		return sceneFile{}, err
	}
	if scene.AudioStatus == "success" && !req.Force {
		a.recordProjectEvent(projectID, "audio", "skipped", "语音已存在："+sceneDisplayName(scene), map[string]any{
			"scene_id": scene.SceneID,
		})
		a.recordProjectEvent(projectID, "subtitle", "skipped", "字幕已存在："+sceneDisplayName(scene), map[string]any{
			"scene_id": scene.SceneID,
		})
		return scene, nil
	}

	tasks, err := a.readTasks(projectID)
	if err != nil {
		return sceneFile{}, err
	}
	storyboardRoot, err := a.readStoryboardRoot(project)
	if err != nil {
		return sceneFile{}, err
	}

	providerRef := a.resolveAudioProviderRef(storyboardRoot, req.ProviderRef)
	if shouldUseEdgeTTS(providerRef) && lookupCommand("edge-tts") == "" {
		providerRef = "builtin/mock-tts"
	}
	if shouldUseKokoroTTS(providerRef) && !a.kokoroTTSAvailable() {
		return sceneFile{}, fmt.Errorf("kokoro tts is not available: %s", a.kokoroTTSUnavailableReason())
	}
	voiceName, speakingRate, pitch := resolveSceneVoiceSettings(storyboardRoot, scene, req, providerRef)
	durationMs := estimateSpeechDurationMs(scene.Narration, scene.DurationHintSec, speakingRate)

	now := time.Now().UTC().Format(time.RFC3339)
	scene.Status = "audio_generating"
	scene.AudioStatus = "running"
	scene.AudioProviderRef = providerRef
	scene.VoiceName = voiceName
	scene.SpeakingRate = speakingRate
	scene.Pitch = pitch
	scene.AudioError = ""
	scene.UpdatedAt = now
	if writeErr := a.writeSceneFile(projectID, scene); writeErr != nil {
		return sceneFile{}, writeErr
	}
	tasks = upsertTask(tasks, newTask("scene_audio_generation", sceneID, "running", "Generating scene audio", now))
	if writeErr := a.writeTasks(projectID, tasks); writeErr != nil {
		return sceneFile{}, writeErr
	}
	a.recordProjectEvent(projectID, "audio", "running", "正在合成旁白："+sceneDisplayName(scene), map[string]any{
		"scene_id":     scene.SceneID,
		"provider_ref": providerRef,
		"voice":        voiceName,
		"rate":         speakingRate,
	})

	audioPath := filepath.Join(a.projectsDir, projectID, "assets", "audio", sceneID+".wav")
	subtitlePath := filepath.Join(a.projectsDir, projectID, "assets", "subtitles", sceneID+".srt")
	subtitleASSPath := filepath.Join(a.projectsDir, projectID, "assets", "subtitles", sceneID+".ass")
	audioExt := ".wav"
	audioMimeType := "audio/wav"
	if shouldUseEdgeTTS(providerRef) {
		audioExt = ".mp3"
		audioMimeType = "audio/mpeg"
		audioPath = filepath.Join(a.projectsDir, projectID, "assets", "audio", sceneID+audioExt)
	}
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		return sceneFile{}, err
	}
	if err := os.MkdirAll(filepath.Dir(subtitlePath), 0o755); err != nil {
		return sceneFile{}, err
	}
	if err := a.generateSceneAudioMedia(audioPath, subtitlePath, scene, providerRef, voiceName, speakingRate, pitch, durationMs); err != nil {
		failedAt := time.Now().UTC().Format(time.RFC3339)
		scene.Status = deriveSceneStatus(scene.ImageStatus, "failed", scene.ComposeStatus)
		scene.AudioStatus = "failed"
		scene.AudioError = err.Error()
		scene.UpdatedAt = failedAt
		_ = a.writeSceneFile(projectID, scene)
		tasks = updateTaskStatus(tasks, "scene_audio_generation", sceneID, "failed", "Scene audio generation failed", err.Error(), failedAt)
		_ = a.writeTasks(projectID, tasks)
		a.recordProjectEvent(projectID, "audio", "error", "旁白生成失败："+err.Error(), map[string]any{
			"scene_id": scene.SceneID,
		})
		return sceneFile{}, err
	}
	if probedDurationMs := probeMediaDurationMs(audioPath); probedDurationMs > 0 {
		durationMs = probedDurationMs
	}
	if err := writeSceneSubtitleFiles(subtitlePath, subtitleASSPath, preferredSubtitleText(scene), durationMs); err != nil {
		failedAt := time.Now().UTC().Format(time.RFC3339)
		scene.Status = deriveSceneStatus(scene.ImageStatus, "failed", scene.ComposeStatus)
		scene.AudioStatus = "failed"
		scene.AudioError = err.Error()
		scene.UpdatedAt = failedAt
		_ = a.writeSceneFile(projectID, scene)
		tasks = updateTaskStatus(tasks, "scene_audio_generation", sceneID, "failed", "Scene subtitle generation failed", err.Error(), failedAt)
		_ = a.writeTasks(projectID, tasks)
		a.recordProjectEvent(projectID, "subtitle", "error", "字幕生成失败："+err.Error(), map[string]any{
			"scene_id": scene.SceneID,
		})
		return sceneFile{}, err
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339)
	scene.AudioStatus = "success"
	scene.AudioLocalPath = audioPath
	scene.AudioPreviewURL = appendCacheBuster(a.projectStaticURL(projectID, filepath.Join("assets", "audio", sceneID+audioExt)), finishedAt)
	scene.AudioMimeType = audioMimeType
	scene.AudioDurationMs = durationMs
	scene.AudioError = ""
	scene.AudioGeneratedAt = finishedAt
	scene.SubtitleLocalPath = subtitlePath
	scene.SubtitlePreviewURL = appendCacheBuster(a.projectStaticURL(projectID, filepath.Join("assets", "subtitles", sceneID+".srt")), finishedAt)
	scene.SubtitleASSLocalPath = subtitleASSPath
	scene.SubtitleASSPreviewURL = appendCacheBuster(a.projectStaticURL(projectID, filepath.Join("assets", "subtitles", sceneID+".ass")), finishedAt)
	scene.SceneDurationMs = maxInt(scene.SceneDurationMs, durationMs)
	invalidateSceneDerivedMedia(&scene)
	scene.UpdatedAt = finishedAt
	if err := a.writeSceneFile(projectID, scene); err != nil {
		return sceneFile{}, err
	}

	tasks = updateTaskStatus(tasks, "scene_audio_generation", sceneID, "success", "Scene audio generated", "", finishedAt)
	if err := a.writeTasks(projectID, tasks); err != nil {
		return sceneFile{}, err
	}

	project.Status = "audio_partial_ready"
	invalidateProjectFinalVideo(&project)
	project.UpdatedAt = finishedAt
	if err := a.writeProject(project); err != nil {
		return sceneFile{}, err
	}
	a.recordProjectEvent(projectID, "audio", "done", "旁白已生成："+sceneDisplayName(scene), map[string]any{
		"scene_id":    scene.SceneID,
		"duration_ms": durationMs,
		"audio_path":  audioPath,
	})
	a.recordProjectEvent(projectID, "subtitle", "done", "逐字高亮字幕已生成："+sceneDisplayName(scene), map[string]any{
		"scene_id": scene.SceneID,
		"srt_path": subtitlePath,
		"ass_path": subtitleASSPath,
	})
	return scene, nil
}

func (a *app) ensureSceneSubtitleAssets(projectID string, scene *sceneFile) error {
	if scene == nil {
		return errors.New("scene is nil")
	}
	subtitleDir := filepath.Join(a.projectsDir, projectID, "assets", "subtitles")
	subtitlePath := strings.TrimSpace(scene.SubtitleLocalPath)
	if subtitlePath == "" {
		subtitlePath = filepath.Join(subtitleDir, scene.SceneID+".srt")
	}
	subtitleASSPath := strings.TrimSpace(scene.SubtitleASSLocalPath)
	if subtitleASSPath == "" {
		subtitleASSPath = filepath.Join(subtitleDir, scene.SceneID+".ass")
	}

	durationMs := scene.AudioDurationMs
	if durationMs <= 0 && scene.AudioLocalPath != "" && fileExists(scene.AudioLocalPath) {
		durationMs = probeMediaDurationMs(scene.AudioLocalPath)
	}
	if durationMs <= 0 {
		durationMs = estimateSpeechDurationMs(scene.Narration, scene.DurationHintSec, scene.SpeakingRate)
	}

	if fileExists(subtitlePath) && fileExists(subtitleASSPath) {
		scene.SubtitleLocalPath = subtitlePath
		scene.SubtitleASSLocalPath = subtitleASSPath
		return nil
	}

	if err := writeSceneSubtitleFiles(subtitlePath, subtitleASSPath, preferredSubtitleText(*scene), durationMs); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	scene.SubtitleLocalPath = subtitlePath
	scene.SubtitlePreviewURL = appendCacheBuster(a.projectStaticURL(projectID, filepath.Join("assets", "subtitles", scene.SceneID+".srt")), now)
	scene.SubtitleASSLocalPath = subtitleASSPath
	scene.SubtitleASSPreviewURL = appendCacheBuster(a.projectStaticURL(projectID, filepath.Join("assets", "subtitles", scene.SceneID+".ass")), now)
	scene.AudioDurationMs = maxInt(scene.AudioDurationMs, durationMs)
	scene.SceneDurationMs = maxInt(scene.SceneDurationMs, durationMs)
	return nil
}

func (a *app) composeSceneVideo(projectID string, sceneID string, req composeSceneVideoRequest) (sceneFile, error) {
	project, err := a.readProject(projectID)
	if err != nil {
		return sceneFile{}, err
	}
	scene, err := a.readSceneFile(projectID, sceneID)
	if err != nil {
		return sceneFile{}, err
	}
	if (scene.ComposeStatus == "success" || scene.ComposeStatus == "preview_ready") && !req.Force && !isSceneDerivedMediaStale(scene) {
		a.recordProjectEvent(projectID, "scene_video", "skipped", "绘本页片段已存在："+sceneDisplayName(scene), map[string]any{
			"scene_id": scene.SceneID,
		})
		return scene, nil
	}
	// 检查图片是否就绪（支持关键帧和普通场景）
	if len(scene.Keyframes) > 0 {
		for _, kf := range scene.Keyframes {
			if kf.ImageLocalPath == "" || !fileExists(kf.ImageLocalPath) {
				return sceneFile{}, errors.New("scene keyframe image is not ready")
			}
		}
	} else {
		if scene.ImageLocalPath == "" || !fileExists(scene.ImageLocalPath) {
			return sceneFile{}, errors.New("scene image is not ready")
		}
	}
	if scene.AudioLocalPath == "" || !fileExists(scene.AudioLocalPath) {
		return sceneFile{}, errors.New("scene audio is not ready")
	}
	if err := a.ensureSceneSubtitleAssets(projectID, &scene); err != nil {
		return sceneFile{}, err
	}
	a.recordProjectEvent(projectID, "subtitle", "done", "字幕资产已确认："+sceneDisplayName(scene), map[string]any{
		"scene_id": scene.SceneID,
	})

	tasks, err := a.readTasks(projectID)
	if err != nil {
		return sceneFile{}, err
	}
	storyboardRoot, err := a.readStoryboardRoot(project)
	if err != nil {
		return sceneFile{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	scene.Status = "video_compositing"
	scene.ComposeStatus = "running"
	scene.ComposeError = ""
	scene.UpdatedAt = now
	if writeErr := a.writeSceneFile(projectID, scene); writeErr != nil {
		return sceneFile{}, writeErr
	}
	tasks = upsertTask(tasks, newTask("scene_video_compositing", sceneID, "running", "Compositing scene video", now))
	if writeErr := a.writeTasks(projectID, tasks); writeErr != nil {
		return sceneFile{}, writeErr
	}
	a.recordProjectEvent(projectID, "scene_video", "running", "正在合成绘本页片段："+sceneDisplayName(scene), map[string]any{
		"scene_id": scene.SceneID,
	})

	width, height := resolveVideoDimensions(resolveSceneAspectRatio(storyboardRoot), req.Width, req.Height)
	targetDir := filepath.Join(a.projectsDir, projectID, "assets", "scene_videos")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return sceneFile{}, err
	}

	var localPath string
	var previewURL string
	var mimeType string
	var composeMode string
	var composeStatus string
	ffmpegPath, ffmpegErr := exec.LookPath("ffmpeg")
	if ffmpegErr == nil {
		localPath = filepath.Join(targetDir, sceneID+".mp4")
		if err := runFFmpegSceneCompose(ffmpegPath, scene, localPath, width, height, resolveSceneFPS(storyboardRoot)); err != nil {
			failedAt := time.Now().UTC().Format(time.RFC3339)
			scene.Status = deriveSceneStatus(scene.ImageStatus, scene.AudioStatus, "failed")
			scene.ComposeStatus = "failed"
			scene.ComposeError = err.Error()
			scene.UpdatedAt = failedAt
			_ = a.writeSceneFile(projectID, scene)
			tasks = updateTaskStatus(tasks, "scene_video_compositing", sceneID, "failed", "Scene video compositing failed", err.Error(), failedAt)
			_ = a.writeTasks(projectID, tasks)
			a.recordProjectEvent(projectID, "scene_video", "error", "绘本页片段合成失败："+err.Error(), map[string]any{
				"scene_id": scene.SceneID,
			})
			return sceneFile{}, err
		}
		previewURL = a.projectStaticURL(projectID, filepath.Join("assets", "scene_videos", sceneID+".mp4"))
		mimeType = "video/mp4"
		composeMode = "ffmpeg_mp4"
		composeStatus = "success"
	} else {
		localPath = filepath.Join(targetDir, sceneID+".html")
		if err := writeScenePreviewHTML(localPath, projectID, scene); err != nil {
			failedAt := time.Now().UTC().Format(time.RFC3339)
			scene.Status = deriveSceneStatus(scene.ImageStatus, scene.AudioStatus, "failed")
			scene.ComposeStatus = "failed"
			scene.ComposeError = err.Error()
			scene.UpdatedAt = failedAt
			_ = a.writeSceneFile(projectID, scene)
			tasks = updateTaskStatus(tasks, "scene_video_compositing", sceneID, "failed", "Scene preview generation failed", err.Error(), failedAt)
			_ = a.writeTasks(projectID, tasks)
			a.recordProjectEvent(projectID, "scene_video", "error", "绘本页预览生成失败："+err.Error(), map[string]any{
				"scene_id": scene.SceneID,
			})
			return sceneFile{}, err
		}
		previewURL = a.projectStaticURL(projectID, filepath.Join("assets", "scene_videos", sceneID+".html"))
		mimeType = "text/html"
		composeMode = "html_preview_fallback"
		composeStatus = "preview_ready"
	}

	finishedAt := time.Now().UTC().Format(time.RFC3339)
	if mimeType == "video/mp4" {
		if probedDurationMs := probeMediaDurationMs(localPath); probedDurationMs > 0 {
			scene.SceneDurationMs = maxInt(scene.SceneDurationMs, probedDurationMs)
		}
	}
	scene.Status = deriveSceneStatus(scene.ImageStatus, scene.AudioStatus, composeStatus)
	scene.ComposeStatus = composeStatus
	scene.ComposeMode = composeMode
	scene.SceneVideoLocalPath = localPath
	scene.SceneVideoPreviewURL = appendCacheBuster(previewURL, finishedAt)
	scene.SceneVideoMimeType = mimeType
	scene.SceneDurationMs = maxInt(scene.SceneDurationMs, scene.AudioDurationMs)
	scene.ComposeError = ""
	scene.ComposedAt = finishedAt
	scene.UpdatedAt = finishedAt
	if err := a.writeSceneFile(projectID, scene); err != nil {
		return sceneFile{}, err
	}

	successMessage := "Scene video composed"
	if composeMode == "html_preview_fallback" {
		successMessage = "Scene preview generated without ffmpeg"
	}
	tasks = updateTaskStatus(tasks, "scene_video_compositing", sceneID, "success", successMessage, "", finishedAt)
	if err := a.writeTasks(projectID, tasks); err != nil {
		return sceneFile{}, err
	}

	project.Status = "video_partial_ready"
	if composeStatus == "preview_ready" {
		project.Status = "video_preview_partial_ready"
	}
	project.UpdatedAt = finishedAt
	if err := a.writeProject(project); err != nil {
		return sceneFile{}, err
	}
	a.recordProjectEvent(projectID, "scene_video", "done", "绘本页片段已合成："+sceneDisplayName(scene), map[string]any{
		"scene_id": scene.SceneID,
		"mode":     composeMode,
		"path":     localPath,
	})
	return scene, nil
}

func (a *app) composeFinalVideo(projectID string, req composeFinalVideoRequest) (projectFile, error) {
	project, err := a.readProject(projectID)
	if err != nil {
		return projectFile{}, err
	}
	scenes, err := a.readSceneFiles(projectID)
	if err != nil {
		return projectFile{}, err
	}
	if len(scenes) == 0 {
		return projectFile{}, errors.New("no scenes found")
	}
	if project.FinalVideoStatus == "success" && !req.Force && !isFinalVideoStale(project, scenes) {
		a.recordProjectEvent(projectID, "final", "skipped", "最终视频已存在，跳过合成", map[string]any{
			"preview_url": project.FinalVideoPreviewURL,
		})
		return project, nil
	}
	ffmpegPath := lookupCommand("ffmpeg")

	for _, scene := range scenes {
		if ffmpegPath != "" {
			if !isSceneVideoReadyForFinalCompose(scene) {
				return projectFile{}, fmt.Errorf("scene video is not ready for final mp4 compose: %s (status=%s mode=%s)", scene.SceneID, scene.ComposeStatus, scene.ComposeMode)
			}
			continue
		}
		if !isSceneVideoPreviewReady(scene) {
			return projectFile{}, fmt.Errorf("scene video is not ready: %s", scene.SceneID)
		}
	}

	tasks, err := a.readTasks(projectID)
	if err != nil {
		return projectFile{}, err
	}
	storyboardRoot, err := a.readStoryboardRoot(project)
	if err != nil {
		return projectFile{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	project.Status = "final_video_compositing"
	project.FinalVideoStatus = "running"
	project.FinalVideoError = ""
	project.UpdatedAt = now
	if err := a.writeProject(project); err != nil {
		return projectFile{}, err
	}
	tasks = upsertTask(tasks, newTask("final_video_compositing", "", "running", "Compositing final video", now))
	if err := a.writeTasks(projectID, tasks); err != nil {
		return projectFile{}, err
	}
	a.recordProjectEvent(projectID, "final", "running", fmt.Sprintf("正在拼接 %d 个绘本页片段", len(scenes)), nil)

	finalDir := filepath.Join(a.projectsDir, projectID, "final")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		return projectFile{}, err
	}

	width, height := resolveVideoDimensions(resolveSceneAspectRatio(storyboardRoot), req.Width, req.Height)
	fps := resolveFinalFPS(storyboardRoot, req.FPS)
	transitionDurationMs := resolveTransitionDurationMs(storyboardRoot, req.TransitionDurationMs)

	var localPath string
	var previewURL string
	var mimeType string
	durationMs := estimateFinalVideoDurationMs(scenes, transitionDurationMs)
	if ffmpegPath != "" {
		localPath = filepath.Join(finalDir, "final_video.mp4")
		if err := runFFmpegFinalCompose(ffmpegPath, scenes, localPath, width, height, fps, resolveTransitionName(storyboardRoot), transitionDurationMs); err != nil {
			failedAt := time.Now().UTC().Format(time.RFC3339)
			project.Status = "final_video_failed"
			project.FinalVideoStatus = "failed"
			project.FinalVideoError = err.Error()
			project.UpdatedAt = failedAt
			_ = a.writeProject(project)
			tasks = updateTaskStatus(tasks, "final_video_compositing", "", "failed", "Final video compositing failed", err.Error(), failedAt)
			_ = a.writeTasks(projectID, tasks)
			a.recordProjectEvent(projectID, "final", "error", "最终视频合成失败："+err.Error(), nil)
			return projectFile{}, err
		}
		previewURL = a.projectStaticURL(projectID, filepath.Join("final", "final_video.mp4"))
		mimeType = "video/mp4"
	} else {
		localPath = filepath.Join(finalDir, "final_video.html")
		if err := writeFinalVideoPreviewHTML(localPath, projectID, project.Title, scenes); err != nil {
			failedAt := time.Now().UTC().Format(time.RFC3339)
			project.Status = "final_video_failed"
			project.FinalVideoStatus = "failed"
			project.FinalVideoError = err.Error()
			project.UpdatedAt = failedAt
			_ = a.writeProject(project)
			tasks = updateTaskStatus(tasks, "final_video_compositing", "", "failed", "Final preview generation failed", err.Error(), failedAt)
			_ = a.writeTasks(projectID, tasks)
			a.recordProjectEvent(projectID, "final", "error", "最终预览生成失败："+err.Error(), nil)
			return projectFile{}, err
		}
		previewURL = a.projectStaticURL(projectID, filepath.Join("final", "final_video.html"))
		mimeType = "text/html"
	}

	finishedAt := time.Now().UTC().Format(time.RFC3339)
	if mimeType == "video/mp4" {
		if probedDurationMs := probeMediaDurationMs(localPath); probedDurationMs > 0 {
			durationMs = probedDurationMs
		}
	}
	project.Status = "final_video_ready"
	project.FinalVideoStatus = "success"
	if mimeType == "text/html" {
		project.Status = "final_video_preview_ready"
		project.FinalVideoStatus = "preview_ready"
	}
	project.FinalVideoLocalPath = localPath
	project.FinalVideoPreviewURL = appendCacheBuster(previewURL, finishedAt)
	project.FinalVideoMimeType = mimeType
	project.FinalVideoDurationMs = durationMs
	project.FinalVideoError = ""
	project.FinalVideoGeneratedAt = finishedAt
	project.UpdatedAt = finishedAt
	if err := a.writeProject(project); err != nil {
		return projectFile{}, err
	}
	successMessage := "Final video generated"
	if mimeType == "text/html" {
		successMessage = "Final preview generated without ffmpeg"
	}
	tasks = updateTaskStatus(tasks, "final_video_compositing", "", "success", successMessage, "", finishedAt)
	if err := a.writeTasks(projectID, tasks); err != nil {
		return projectFile{}, err
	}
	a.recordProjectEvent(projectID, "final", "done", successMessage, map[string]any{
		"preview_url": project.FinalVideoPreviewURL,
		"local_path":  project.FinalVideoLocalPath,
		"mime_type":   project.FinalVideoMimeType,
		"duration_ms": project.FinalVideoDurationMs,
	})
	return project, nil
}

func (a *app) runProjectPipeline(projectID string, req runProjectPipelineRequest) (detail projectDetailResponse, err error) {
	a.recordProjectEvent(projectID, "pipeline", "running", "开始完整绘本视频流水线", map[string]any{
		"provider_ref":       fallbackString(req.ProviderRef, defaultCodexImageProviderRef),
		"audio_provider_ref": fallbackString(req.AudioProviderRef, defaultKokoroProviderRef),
		"voice_name":         fallbackString(req.VoiceName, defaultKokoroVoiceName),
	})
	defer func() {
		if err != nil {
			a.recordProjectEvent(projectID, "pipeline", "error", "流水线中断："+err.Error(), nil)
			return
		}
		a.recordProjectEvent(projectID, "pipeline", "done", "完整绘本视频流水线完成", nil)
	}()

	project, err := a.readProject(projectID)
	if err != nil {
		return projectDetailResponse{}, err
	}
	if !project.StoryboardValid || project.StoryboardPath == "" {
		if _, err := a.generateStoryboard(projectID, generateStoryboardRequest{
			ProviderRef:      req.ProviderRef,
			BrowserProfileID: req.BrowserProfileID,
			TimeoutMs:        req.TimeoutMs,
		}); err != nil {
			return projectDetailResponse{}, err
		}
	}

	scenes, err := a.readSceneFiles(projectID)
	if err != nil {
		return projectDetailResponse{}, err
	}
	if len(scenes) == 0 {
		return projectDetailResponse{}, errors.New("no scenes found; generate storyboard first")
	}

	for _, scene := range scenes {
		sceneLabel := sceneDisplayName(scene)
		a.recordProjectEvent(projectID, "scene", "running", "开始处理绘本页："+sceneLabel, map[string]any{
			"scene_id": scene.SceneID,
			"sequence": scene.Sequence,
		})
		tasks, err := a.readTasks(projectID)
		if err != nil {
			return projectDetailResponse{}, err
		}
		keyframeTask, hasKeyframeTask := findTask(tasks, "scene_keyframe_prompt_generation", scene.SceneID)
		if req.Force || taskShouldRun(keyframeTask, hasKeyframeTask) {
			a.recordProjectEvent(projectID, "keyframes", "running", "规划图片切换关键帧："+sceneLabel, map[string]any{
				"scene_id": scene.SceneID,
			})
			if _, err := a.generateSceneKeyframes(projectID, scene.SceneID, generateSceneKeyframesRequest{
				ProviderRef:      req.ProviderRef,
				BrowserProfileID: req.BrowserProfileID,
				TimeoutMs:        req.TimeoutMs,
				Force:            req.Force,
			}); err != nil {
				return projectDetailResponse{}, err
			}
		} else {
			a.recordProjectEvent(projectID, "keyframes", "skipped", "关键帧已存在，跳过："+sceneLabel, map[string]any{
				"scene_id": scene.SceneID,
			})
		}

		scene, err = a.readSceneFile(projectID, scene.SceneID)
		if err != nil {
			return projectDetailResponse{}, err
		}
		tasks, err = a.readTasks(projectID)
		if err != nil {
			return projectDetailResponse{}, err
		}
		if len(scene.Keyframes) > 0 {
			for index, keyframe := range scene.Keyframes {
				taskSceneID := fmt.Sprintf("%s_kf%d", scene.SceneID, index)
				imageTask, hasImageTask := findTask(tasks, "scene_image_generation", taskSceneID)
				forceImage := req.Force || !keyframeImageReady(keyframe)
				if forceImage || taskShouldRun(imageTask, hasImageTask) {
					a.recordProjectEvent(projectID, "image", "running", fmt.Sprintf("Codex/image 出图：%s 第 %d 张", sceneLabel, index+1), map[string]any{
						"scene_id":       scene.SceneID,
						"task_scene_id":  taskSceneID,
						"keyframe_index": index + 1,
					})
					if _, err := a.generateSceneImage(projectID, taskSceneID, generateSceneImageRequest{
						ProviderRef:      req.ProviderRef,
						BrowserProfileID: req.BrowserProfileID,
						TimeoutMs:        req.TimeoutMs,
						Force:            forceImage,
					}); err != nil {
						return projectDetailResponse{}, err
					}
				} else {
					a.recordProjectEvent(projectID, "image", "skipped", fmt.Sprintf("图片已存在，跳过：%s 第 %d 张", sceneLabel, index+1), map[string]any{
						"scene_id":      scene.SceneID,
						"task_scene_id": taskSceneID,
					})
				}
			}
		} else {
			imageTask, hasImageTask := findTask(tasks, "scene_image_generation", scene.SceneID)
			forceImage := req.Force || !sceneMainImageReady(scene)
			if forceImage || taskShouldRun(imageTask, hasImageTask) {
				a.recordProjectEvent(projectID, "image", "running", "Codex/image 出图："+sceneLabel, map[string]any{
					"scene_id": scene.SceneID,
				})
				if _, err := a.generateSceneImage(projectID, scene.SceneID, generateSceneImageRequest{
					ProviderRef:      req.ProviderRef,
					BrowserProfileID: req.BrowserProfileID,
					TimeoutMs:        req.TimeoutMs,
					Force:            forceImage,
				}); err != nil {
					return projectDetailResponse{}, err
				}
			} else {
				a.recordProjectEvent(projectID, "image", "skipped", "图片已存在，跳过："+sceneLabel, map[string]any{
					"scene_id": scene.SceneID,
				})
			}
		}

		scene, err = a.readSceneFile(projectID, scene.SceneID)
		if err != nil {
			return projectDetailResponse{}, err
		}
		tasks, err = a.readTasks(projectID)
		if err != nil {
			return projectDetailResponse{}, err
		}
		audioTask, hasAudioTask := findTask(tasks, "scene_audio_generation", scene.SceneID)
		forceAudio := req.Force || sceneNeedsAudioGeneration(scene)
		if forceAudio || taskShouldRun(audioTask, hasAudioTask) {
			a.recordProjectEvent(projectID, "audio", "running", "生成儿童语速旁白："+sceneLabel, map[string]any{
				"scene_id": scene.SceneID,
				"voice":    fallbackString(req.VoiceName, defaultKokoroVoiceName),
				"rate":     fallbackString(req.SpeakingRate, defaultChildSpeakingRate),
			})
			if _, err := a.generateSceneAudio(projectID, scene.SceneID, generateSceneAudioRequest{
				ProviderRef:  fallbackString(req.AudioProviderRef, defaultKokoroProviderRef),
				VoiceName:    fallbackString(req.VoiceName, defaultKokoroVoiceName),
				SpeakingRate: fallbackString(req.SpeakingRate, defaultChildSpeakingRate),
				Pitch:        fallbackString(req.Pitch, "0Hz"),
				Force:        forceAudio,
			}); err != nil {
				return projectDetailResponse{}, err
			}
		} else {
			a.recordProjectEvent(projectID, "audio", "skipped", "语音和字幕已存在，跳过："+sceneLabel, map[string]any{
				"scene_id": scene.SceneID,
			})
		}

		scene, err = a.readSceneFile(projectID, scene.SceneID)
		if err != nil {
			return projectDetailResponse{}, err
		}
		tasks, err = a.readTasks(projectID)
		if err != nil {
			return projectDetailResponse{}, err
		}
		videoTask, hasVideoTask := findTask(tasks, "scene_video_compositing", scene.SceneID)
		forceVideo := req.Force || isSceneDerivedMediaStale(scene)
		if forceVideo || taskShouldRun(videoTask, hasVideoTask) {
			a.recordProjectEvent(projectID, "scene_video", "running", "合成绘本页片段："+sceneLabel, map[string]any{
				"scene_id": scene.SceneID,
			})
			if _, err := a.composeSceneVideo(projectID, scene.SceneID, composeSceneVideoRequest{
				Width:  req.Width,
				Height: req.Height,
				Force:  forceVideo,
			}); err != nil {
				return projectDetailResponse{}, err
			}
		} else {
			a.recordProjectEvent(projectID, "scene_video", "skipped", "绘本页片段已存在，跳过："+sceneLabel, map[string]any{
				"scene_id": scene.SceneID,
			})
		}
		a.recordProjectEvent(projectID, "scene", "done", "绘本页处理完成："+sceneLabel, map[string]any{
			"scene_id": scene.SceneID,
		})
	}

	a.recordProjectEvent(projectID, "final", "running", "开始拼接最终绘本故事视频", nil)
	if _, err := a.composeFinalVideo(projectID, composeFinalVideoRequest{
		Width:                req.Width,
		Height:               req.Height,
		FPS:                  req.FPS,
		TransitionDurationMs: req.TransitionDurationMs,
		Force:                req.Force,
	}); err != nil {
		return projectDetailResponse{}, err
	}
	detail, err = a.getProjectDetail(projectID)
	if err != nil {
		return projectDetailResponse{}, err
	}
	a.recordProjectEvent(projectID, "final", "done", "最终绘本故事视频已生成", map[string]any{
		"preview_url": detail.Project.FinalVideoPreviewURL,
		"local_path":  detail.Project.FinalVideoLocalPath,
	})
	return detail, nil
}

func findTask(tasks []taskFile, kind string, sceneID string) (taskFile, bool) {
	for _, task := range tasks {
		if task.Kind == kind && task.SceneID == sceneID {
			return task, true
		}
	}
	return taskFile{}, false
}

func sceneDisplayName(scene sceneFile) string {
	title := strings.TrimSpace(scene.Title)
	if title == "" {
		title = scene.SceneID
	}
	if scene.Sequence > 0 {
		return fmt.Sprintf("第 %d 页《%s》", scene.Sequence, title)
	}
	return title
}

func taskShouldRun(task taskFile, exists bool) bool {
	if !exists {
		return false
	}
	switch task.Status {
	case "", "pending", "failed":
		return true
	default:
		return false
	}
}

func keyframeImageReady(keyframe keyframe) bool {
	return keyframe.ImageLocalPath != "" && fileExists(keyframe.ImageLocalPath)
}

func sceneMainImageReady(scene sceneFile) bool {
	return scene.ImageStatus == "success" && scene.ImageLocalPath != "" && fileExists(scene.ImageLocalPath)
}

func sceneNeedsAudioGeneration(scene sceneFile) bool {
	if scene.AudioStatus != "success" || scene.AudioLocalPath == "" || !fileExists(scene.AudioLocalPath) {
		return true
	}
	if scene.SubtitleLocalPath == "" || !fileExists(scene.SubtitleLocalPath) {
		return true
	}
	if scene.SubtitleASSLocalPath == "" || !fileExists(scene.SubtitleASSLocalPath) {
		return true
	}
	return false
}

func (a *app) readStoryboardRoot(project projectFile) (map[string]any, error) {
	if project.StoryboardPath == "" {
		return nil, errors.New("storyboard path is empty")
	}
	var storyboard map[string]any
	if err := readJSONFile(project.StoryboardPath, &storyboard); err != nil {
		return nil, err
	}
	return storyboard, nil
}

func (a *app) runZeroTokenSceneImage(project projectFile, taskSceneID string, req generateSceneImageRequest, prompt string, aspectRatio string) ([]zeroTokenGeneratedImage, error) {
	if !fileExists(a.zeroTokenBridgePath) {
		return nil, fmt.Errorf("zero-token bridge server not found at %s; run npm run build first", a.zeroTokenBridgePath)
	}

	timeoutMs := req.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 300000
	}
	browserProfileID := req.BrowserProfileID
	if browserProfileID == "" {
		browserProfileID = "chrome_main"
	}
	providerRef := resolveImageProviderRef(project.ProviderRef, req.ProviderRef)

	payload := zeroTokenBridgePayload{
		RequestID:   fmt.Sprintf("image_%s_%d", taskSceneID, time.Now().UnixMilli()),
		ProjectID:   project.ProjectID,
		SceneID:     taskSceneID,
		ProviderRef: providerRef,
		Capability:  "text_image",
		Input: map[string]any{
			"prompt":      prompt,
			"count":       1,
			"aspectRatio": aspectRatio,
		},
		RuntimeOptions: map[string]any{
			"browserProfileId":   browserProfileID,
			"timeoutMs":          timeoutMs,
			"retryLimit":         1,
			"saveDebugArtifacts": false,
		},
	}
	a.recordProjectEvent(project.ProjectID, "image", "running", "zero-token 已向 Codex/image 提交出图请求："+taskSceneID, map[string]any{
		"scene_id":       taskSceneID,
		"provider_ref":   providerRef,
		"aspect_ratio":   aspectRatio,
		"prompt_chars":   utf8.RuneCountInString(prompt),
		"request_id":     payload.RequestID,
		"browserProfile": browserProfileID,
	})
	resp, err := a.runZeroTokenGenerate(context.Background(), payload, timeoutMs)
	if err != nil {
		a.recordProjectEvent(project.ProjectID, "image", "error", "Codex/image 请求失败："+err.Error(), map[string]any{
			"scene_id":   taskSceneID,
			"request_id": payload.RequestID,
		})
		return nil, err
	}
	if len(resp.Result.Output.Images) == 0 {
		a.recordProjectEvent(project.ProjectID, "image", "error", "Codex/image 没有返回图片："+taskSceneID, map[string]any{
			"scene_id":   taskSceneID,
			"request_id": payload.RequestID,
		})
		return nil, errors.New("zero-token did not return any image")
	}
	a.recordProjectEvent(project.ProjectID, "image", "running", fmt.Sprintf("Codex/image 已返回 %d 张图片，正在落盘", len(resp.Result.Output.Images)), map[string]any{
		"scene_id":   taskSceneID,
		"request_id": payload.RequestID,
	})
	return resp.Result.Output.Images, nil
}

func (a *app) loadGeneratedImageBytes(image zeroTokenGeneratedImage) ([]byte, error) {
	var raw []byte
	switch {
	case image.LocalPath != "" && fileExists(image.LocalPath):
		blob, readErr := os.ReadFile(image.LocalPath)
		if readErr != nil {
			return nil, readErr
		}
		raw = blob
	case image.URL != "":
		req, reqErr := http.NewRequest("GET", image.URL, nil)
		if reqErr != nil {
			return nil, fmt.Errorf("create download request: %w", reqErr)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
		req.Header.Set("Referer", image.URL)

		var resp *http.Response
		var fetchErr error
		for attempt := 1; attempt <= 3; attempt++ {
			resp, fetchErr = http.DefaultClient.Do(req)
			if fetchErr == nil {
				break
			}
			log.Printf("download image attempt %d/3 failed: %v, url=%s", attempt, fetchErr, image.URL)
			if attempt < 3 {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
		}
		if fetchErr != nil {
			return nil, fmt.Errorf("download generated image after 3 attempts: %w", fetchErr)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("download generated image: unexpected HTTP %d", resp.StatusCode)
		}
		blob, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		raw = blob
	default:
		return nil, errors.New("generated image has neither url nor localPath")
	}
	return raw, nil
}

func (a *app) removeGeneratedImageArtifacts(projectID string, sceneID string) {
	patterns := []string{
		filepath.Join(a.projectsDir, projectID, "assets", "images", sceneID+".*"),
		filepath.Join(a.projectsDir, projectID, "assets", "images", sceneID+"__*"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			_ = os.Remove(match)
		}
	}
}

func (a *app) persistGeneratedImages(projectID string, sceneID string, images []zeroTokenGeneratedImage) ([]imageCandidate, error) {
	if len(images) == 0 {
		return nil, errors.New("no generated images to persist")
	}
	targetDir := filepath.Join(a.projectsDir, projectID, "assets", "images")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	a.removeGeneratedImageArtifacts(projectID, sceneID)

	candidates := make([]imageCandidate, 0, len(images))
	for index, image := range images {
		raw, err := a.loadGeneratedImageBytes(image)
		if err != nil {
			return nil, err
		}
		ext := detectImageExtension(image.URL, image.LocalPath, image.MimeType)
		fileName := fmt.Sprintf("%s__%02d%s", sceneID, index+1, ext)
		targetPath := filepath.Join(targetDir, fileName)
		if err := os.WriteFile(targetPath, raw, 0o644); err != nil {
			return nil, err
		}
		candidates = append(candidates, imageCandidate{
			CandidateID:     fmt.Sprintf("%s-%02d", sceneID, index+1),
			SourceIndex:     index,
			ImageURL:        image.URL,
			ImageLocalPath:  targetPath,
			ImagePreviewURL: a.projectStaticURL(projectID, filepath.Join("assets", "images", fileName)),
			ImageMimeType:   image.MimeType,
		})
	}
	return candidates, nil
}

func applySelectedImageCandidate(target *sceneFile, candidates []imageCandidate, selectedIndex int) error {
	if len(candidates) == 0 {
		return errors.New("image candidates are empty")
	}
	if selectedIndex < 0 || selectedIndex >= len(candidates) {
		return fmt.Errorf("candidate_index %d is out of range", selectedIndex)
	}
	selected := candidates[selectedIndex]
	target.SelectedImageIdx = selectedIndex
	target.ImageCandidates = candidates
	target.ImageURL = selected.ImageURL
	target.ImageLocalPath = selected.ImageLocalPath
	target.ImagePreviewURL = selected.ImagePreviewURL
	target.ImageMimeType = selected.ImageMimeType
	return nil
}

func applySelectedKeyframeCandidate(target *keyframe, candidates []imageCandidate, selectedIndex int) error {
	if len(candidates) == 0 {
		return errors.New("image candidates are empty")
	}
	if selectedIndex < 0 || selectedIndex >= len(candidates) {
		return fmt.Errorf("candidate_index %d is out of range", selectedIndex)
	}
	selected := candidates[selectedIndex]
	target.SelectedImageIdx = selectedIndex
	target.ImageCandidates = candidates
	target.ImageURL = selected.ImageURL
	target.ImageLocalPath = selected.ImageLocalPath
	target.ImagePreviewURL = selected.ImagePreviewURL
	target.ImageMimeType = selected.ImageMimeType
	return nil
}

func (a *app) getProjectAssets(projectID string) (map[string]any, error) {
	project, err := a.readProject(projectID)
	if err != nil {
		return nil, err
	}
	scenes, err := a.readSceneFiles(projectID)
	if err != nil {
		return nil, err
	}

	images := make([]map[string]any, 0, len(scenes))
	audioAssets := make([]map[string]any, 0, len(scenes))
	subtitleAssets := make([]map[string]any, 0, len(scenes))
	sceneVideos := make([]map[string]any, 0, len(scenes))
	for _, scene := range scenes {
		if scene.ImageLocalPath == "" && scene.ImageURL == "" {
		} else {
			images = append(images, map[string]any{
				"scene_id":         scene.SceneID,
				"status":           scene.ImageStatus,
				"image_url":        scene.ImageURL,
				"image_local_path": scene.ImageLocalPath,
				"preview_url":      scene.ImagePreviewURL,
			})
		}
		if scene.AudioLocalPath != "" || scene.AudioPreviewURL != "" {
			audioAssets = append(audioAssets, map[string]any{
				"scene_id":         scene.SceneID,
				"status":           scene.AudioStatus,
				"audio_local_path": scene.AudioLocalPath,
				"preview_url":      scene.AudioPreviewURL,
				"mime_type":        scene.AudioMimeType,
				"duration_ms":      scene.AudioDurationMs,
			})
		}
		if scene.SubtitleLocalPath != "" || scene.SubtitlePreviewURL != "" {
			subtitleAssets = append(subtitleAssets, map[string]any{
				"scene_id":                 scene.SceneID,
				"subtitle_local_path":      scene.SubtitleLocalPath,
				"preview_url":              scene.SubtitlePreviewURL,
				"subtitle_ass_local_path":  scene.SubtitleASSLocalPath,
				"subtitle_ass_preview_url": scene.SubtitleASSPreviewURL,
			})
		}
		if scene.SceneVideoLocalPath != "" || scene.SceneVideoPreviewURL != "" {
			sceneVideos = append(sceneVideos, map[string]any{
				"scene_id":          scene.SceneID,
				"status":            scene.ComposeStatus,
				"compose_mode":      scene.ComposeMode,
				"video_local_path":  scene.SceneVideoLocalPath,
				"preview_url":       scene.SceneVideoPreviewURL,
				"mime_type":         scene.SceneVideoMimeType,
				"scene_duration_ms": scene.SceneDurationMs,
			})
		}
	}

	storyboardURL := ""
	if project.StoryboardPath != "" && fileExists(project.StoryboardPath) {
		storyboardURL = a.projectStaticURL(projectID, "storyboard.json")
	}

	return map[string]any{
		"project_id": projectID,
		"assets": map[string]any{
			"storyboard":   storyboardURL,
			"images":       images,
			"audio":        audioAssets,
			"subtitles":    subtitleAssets,
			"scene_videos": sceneVideos,
			"final_video": map[string]any{
				"status":       project.FinalVideoStatus,
				"local_path":   project.FinalVideoLocalPath,
				"preview_url":  project.FinalVideoPreviewURL,
				"mime_type":    project.FinalVideoMimeType,
				"duration_ms":  project.FinalVideoDurationMs,
				"generated_at": project.FinalVideoGeneratedAt,
				"error":        project.FinalVideoError,
			},
			"preview_root": a.projectStaticURL(projectID, ""),
		},
	}, nil
}

func (a *app) selectSceneImageCandidate(projectID string, sceneID string, candidateIndex int) (sceneFile, error) {
	project, err := a.readProject(projectID)
	if err != nil {
		return sceneFile{}, err
	}

	baseSceneID := sceneID
	keyframeIndex := -1
	if strings.Contains(sceneID, "_kf") {
		parts := strings.Split(sceneID, "_kf")
		baseSceneID = parts[0]
		keyframeIndex, _ = strconv.Atoi(parts[1])
	}

	scene, err := a.readSceneFile(projectID, baseSceneID)
	if err != nil {
		return sceneFile{}, err
	}

	if keyframeIndex >= 0 {
		if keyframeIndex >= len(scene.Keyframes) {
			return sceneFile{}, errors.New("keyframe does not exist")
		}
		if err := applySelectedKeyframeCandidate(&scene.Keyframes[keyframeIndex], scene.Keyframes[keyframeIndex].ImageCandidates, candidateIndex); err != nil {
			return sceneFile{}, err
		}
	} else {
		if err := applySelectedImageCandidate(&scene, scene.ImageCandidates, candidateIndex); err != nil {
			return sceneFile{}, err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	invalidateSceneDerivedMedia(&scene)
	scene.UpdatedAt = now
	if err := a.writeSceneFile(projectID, scene); err != nil {
		return sceneFile{}, err
	}

	invalidateProjectFinalVideo(&project)
	project.UpdatedAt = now
	if err := a.writeProject(project); err != nil {
		return sceneFile{}, err
	}

	return scene, nil
}

func newTask(kind string, sceneID string, status string, message string, now string) taskFile {
	return taskFile{
		TaskID:    fmt.Sprintf("task_%d", time.Now().UnixNano()),
		Kind:      kind,
		SceneID:   sceneID,
		Status:    status,
		Message:   message,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func upsertTask(tasks []taskFile, task taskFile) []taskFile {
	for index := range tasks {
		if tasks[index].Kind == task.Kind && tasks[index].SceneID == task.SceneID {
			task.TaskID = tasks[index].TaskID
			task.CreatedAt = tasks[index].CreatedAt
			tasks[index] = task
			return tasks
		}
	}
	return append(tasks, task)
}

func updateTaskStatus(tasks []taskFile, kind string, sceneID string, status string, message string, errorMessage string, now string) []taskFile {
	for index := range tasks {
		if tasks[index].Kind == kind && tasks[index].SceneID == sceneID {
			tasks[index].Status = status
			tasks[index].Message = message
			tasks[index].Error = errorMessage
			tasks[index].UpdatedAt = now
			tasks[index].FinishedAt = now
			return tasks
		}
	}
	task := newTask(kind, sceneID, status, message, now)
	task.Error = errorMessage
	task.FinishedAt = now
	return append(tasks, task)
}

func pruneDerivedTasks(tasks []taskFile) []taskFile {
	keep := make([]taskFile, 0, len(tasks))
	for _, task := range tasks {
		switch task.Kind {
		case "storyboard_validation", "scene_task_split", "scene_keyframe_prompt_generation", "scene_image_generation", "scene_audio_generation", "scene_video_compositing", "final_video_compositing":
			continue
		default:
			keep = append(keep, task)
		}
	}
	return keep
}

func resolveImageProviderRef(projectProviderRef string, requestProviderRef string) string {
	return defaultCodexImageProviderRef
}

func (a *app) resolveAudioProviderRef(storyboard map[string]any, requestProviderRef string) string {
	if strings.TrimSpace(requestProviderRef) != "" {
		return strings.TrimSpace(requestProviderRef)
	}
	if audioProfile, ok := storyboard["audio_profile"].(map[string]any); ok {
		if providerRef, ok := requiredStringField(audioProfile, "tts_provider_ref"); ok {
			return providerRef
		}
	}
	if a.kokoroTTSAvailable() {
		return defaultKokoroProviderRef
	}
	if lookupCommand("edge-tts") != "" {
		return defaultEdgeTTSProviderRef
	}
	return "builtin/mock-tts"
}

func shouldUseEdgeTTS(providerRef string) bool {
	providerRef = strings.ToLower(strings.TrimSpace(providerRef))
	return strings.HasPrefix(providerRef, "edge-tts/")
}

func shouldUseKokoroTTS(providerRef string) bool {
	providerRef = strings.ToLower(strings.TrimSpace(providerRef))
	return strings.HasPrefix(providerRef, "kokoro/")
}

func edgeTTSVoiceNameFromProviderRef(providerRef string) string {
	providerRef = strings.TrimSpace(providerRef)
	if !shouldUseEdgeTTS(providerRef) {
		return ""
	}
	voiceName := strings.TrimSpace(strings.TrimPrefix(providerRef, "edge-tts/"))
	if voiceName == "" {
		return defaultEdgeTTSVoiceName
	}
	return voiceName
}

func kokoroVoiceNameFromProviderRef(providerRef string) string {
	providerRef = strings.TrimSpace(providerRef)
	if !shouldUseKokoroTTS(providerRef) {
		return ""
	}
	voiceName := strings.TrimSpace(strings.TrimPrefix(providerRef, "kokoro/"))
	if voiceName == "" {
		return defaultKokoroVoiceName
	}
	return voiceName
}

func resolveSceneVoiceSettings(storyboard map[string]any, scene sceneFile, req generateSceneAudioRequest, providerRef string) (string, string, string) {
	voiceName := strings.TrimSpace(req.VoiceName)
	speakingRate := strings.TrimSpace(req.SpeakingRate)
	pitch := strings.TrimSpace(req.Pitch)
	if voiceName == "" {
		voiceName, _ = requiredStringField(scene.Audio, "voice_name")
	}
	if speakingRate == "" {
		speakingRate, _ = requiredStringField(scene.Audio, "speaking_rate")
	}
	if pitch == "" {
		pitch, _ = requiredStringField(scene.Audio, "pitch")
	}
	if audioProfile, ok := storyboard["audio_profile"].(map[string]any); ok {
		if voiceName == "" {
			voiceName, _ = requiredStringField(audioProfile, "voice_name")
		}
		if speakingRate == "" {
			speakingRate, _ = requiredStringField(audioProfile, "speaking_rate")
		}
		if pitch == "" {
			pitch, _ = requiredStringField(audioProfile, "pitch")
		}
	}
	if voiceName == "" {
		if shouldUseEdgeTTS(providerRef) {
			voiceName = edgeTTSVoiceNameFromProviderRef(providerRef)
		} else if shouldUseKokoroTTS(providerRef) {
			voiceName = kokoroVoiceNameFromProviderRef(providerRef)
		} else {
			voiceName = "builtin-mock-voice"
		}
	}
	if shouldUseKokoroTTS(providerRef) && !looksLikeKokoroVoiceName(voiceName) {
		voiceName = kokoroVoiceNameFromProviderRef(providerRef)
	}
	if speakingRate == "" {
		speakingRate = defaultChildSpeakingRate
	}
	if pitch == "" {
		pitch = "0%"
	}
	return voiceName, speakingRate, pitch
}

func resolveSceneAspectRatio(storyboard map[string]any) string {
	if videoProfile, ok := storyboard["video_profile"].(map[string]any); ok {
		if aspectRatio, ok := requiredStringField(videoProfile, "aspect_ratio"); ok {
			return aspectRatio
		}
	}
	return "9:16"
}

func resolveSceneFPS(storyboard map[string]any) int {
	if videoProfile, ok := storyboard["video_profile"].(map[string]any); ok {
		if fps, ok := requiredPositiveIntField(videoProfile, "fps"); ok {
			return fps
		}
	}
	return 24
}

func resolveTransitionName(storyboard map[string]any) string {
	if videoProfile, ok := storyboard["video_profile"].(map[string]any); ok {
		if transition, ok := requiredStringField(videoProfile, "transition"); ok {
			return sanitizeFFmpegTransitionName(transition)
		}
	}
	return "fade"
}

func sanitizeFFmpegTransitionName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "fade", "dissolve", "wipeleft", "wiperight", "wipeup", "wipedown", "slideleft", "slideright", "slideup", "slidedown", "smoothleft", "smoothright", "smoothup", "smoothdown", "circleopen", "circleclose", "rectcrop", "distance", "fadeblack", "fadewhite", "radial", "pixelize", "diagtl", "diagtr", "diagbl", "diagbr", "hlslice", "hrslice", "vuslice", "vdslice", "hblur", "fadegrays", "squeezeh", "squeezev", "zoomin", "coverleft", "coverright", "coverup", "coverdown", "revealleft", "revealright", "revealup", "revealdown":
		return strings.ToLower(strings.TrimSpace(name))
	default:
		return "fade"
	}
}

func resolveTransitionDurationMs(storyboard map[string]any, requested int) int {
	if requested > 0 {
		return requested
	}
	if videoProfile, ok := storyboard["video_profile"].(map[string]any); ok {
		if duration, ok := requiredPositiveIntField(videoProfile, "transition_duration_ms"); ok {
			return duration
		}
	}
	return 500
}

func resolveFinalFPS(storyboard map[string]any, requested int) int {
	if requested > 0 {
		return requested
	}
	return resolveSceneFPS(storyboard)
}

func deriveSceneStatus(imageStatus string, audioStatus string, composeStatus string) string {
	switch {
	case composeStatus == "success":
		return "video_ready"
	case composeStatus == "preview_ready":
		return "video_preview_ready"
	case composeStatus == "running":
		return "video_compositing"
	case imageStatus == "success" && audioStatus == "success":
		return "media_ready"
	case imageStatus == "running":
		return "image_generating"
	case audioStatus == "running":
		return "audio_generating"
	case imageStatus == "success":
		return "image_ready"
	case audioStatus == "success":
		return "audio_ready"
	default:
		return "scene_tasks_ready"
	}
}

func invalidateSceneDerivedMedia(scene *sceneFile) {
	scene.ComposeStatus = ""
	scene.ComposeMode = ""
	scene.SceneVideoLocalPath = ""
	scene.SceneVideoPreviewURL = ""
	scene.SceneVideoMimeType = ""
	scene.ComposeError = ""
	scene.ComposedAt = ""
	scene.Status = deriveSceneStatus(scene.ImageStatus, scene.AudioStatus, scene.ComposeStatus)
}

func invalidateProjectFinalVideo(project *projectFile) {
	project.FinalVideoStatus = ""
	project.FinalVideoLocalPath = ""
	project.FinalVideoPreviewURL = ""
	project.FinalVideoMimeType = ""
	project.FinalVideoDurationMs = 0
	project.FinalVideoError = ""
	project.FinalVideoGeneratedAt = ""
}

func parseRFC3339Timestamp(value string) time.Time {
	text := strings.TrimSpace(value)
	if text == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func isSceneDerivedMediaStale(scene sceneFile) bool {
	composedAt := parseRFC3339Timestamp(scene.ComposedAt)
	if composedAt.IsZero() {
		return true
	}
	imageGeneratedAt := parseRFC3339Timestamp(scene.ImageGeneratedAt)
	if !imageGeneratedAt.IsZero() && imageGeneratedAt.After(composedAt) {
		return true
	}
	audioGeneratedAt := parseRFC3339Timestamp(scene.AudioGeneratedAt)
	if !audioGeneratedAt.IsZero() && audioGeneratedAt.After(composedAt) {
		return true
	}
	if scene.AudioStatus == "success" {
		if scene.SubtitleLocalPath == "" || !fileExists(scene.SubtitleLocalPath) {
			return true
		}
		if scene.SubtitleASSLocalPath == "" || !fileExists(scene.SubtitleASSLocalPath) {
			return true
		}
	}
	if scene.SceneVideoLocalPath == "" || !fileExists(scene.SceneVideoLocalPath) {
		return true
	}
	return false
}

func isFinalVideoStale(project projectFile, scenes []sceneFile) bool {
	finalGeneratedAt := parseRFC3339Timestamp(project.FinalVideoGeneratedAt)
	if finalGeneratedAt.IsZero() {
		return true
	}
	if project.FinalVideoLocalPath == "" || !fileExists(project.FinalVideoLocalPath) {
		return true
	}
	for _, scene := range scenes {
		if isSceneDerivedMediaStale(scene) {
			return true
		}
		composedAt := parseRFC3339Timestamp(scene.ComposedAt)
		if !composedAt.IsZero() && composedAt.After(finalGeneratedAt) {
			return true
		}
	}
	return false
}

func extractCharacterBibleEntries(characterBible any) []map[string]any {
	switch typed := characterBible.(type) {
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if entry, ok := item.(map[string]any); ok {
				result = append(result, entry)
			}
		}
		return result
	case map[string]any:
		if nested, ok := typed["characters"].([]any); ok {
			return extractCharacterBibleEntries(nested)
		}
		result := make([]map[string]any, 0, len(typed))
		for _, value := range typed {
			if entry, ok := value.(map[string]any); ok {
				result = append(result, entry)
			}
		}
		return result
	default:
		return nil
	}
}

func filterCharacterBible(characterBible any, characterIDs []string) []map[string]any {
	entries := extractCharacterBibleEntries(characterBible)
	if len(entries) == 0 {
		return nil
	}
	if len(characterIDs) == 0 {
		return entries
	}
	allowed := make(map[string]struct{}, len(characterIDs))
	for _, characterID := range characterIDs {
		characterID = strings.TrimSpace(characterID)
		if characterID != "" {
			allowed[characterID] = struct{}{}
		}
	}
	filtered := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		charID, _ := requiredStringField(entry, "char_id")
		if _, ok := allowed[charID]; ok {
			filtered = append(filtered, entry)
		}
	}
	if len(filtered) > 0 {
		return filtered
	}
	return entries
}

func joinCharacterIDs(characterIDs []string) string {
	cleaned := make([]string, 0, len(characterIDs))
	for _, characterID := range characterIDs {
		characterID = strings.TrimSpace(characterID)
		if characterID != "" {
			cleaned = append(cleaned, characterID)
		}
	}
	if len(cleaned) == 0 {
		return "无"
	}
	return strings.Join(cleaned, ", ")
}

func buildCharacterConsistencyPrompt(characterBible any, characterIDs []string) string {
	entries := filterCharacterBible(characterBible, characterIDs)
	if len(entries) == 0 {
		return "[]"
	}
	blocks := make([]string, 0, len(entries))
	for _, entry := range entries {
		name, _ := requiredStringField(entry, "name")
		charID, _ := requiredStringField(entry, "char_id")
		typeName, _ := requiredStringField(entry, "type")
		appearance, _ := requiredStringField(entry, "appearance")
		personality, _ := requiredStringField(entry, "personality")
		style, _ := requiredStringField(entry, "style")
		age, _ := requiredStringField(entry, "age")
		face, _ := requiredStringField(entry, "face")
		faceFeatures, _ := requiredStringField(entry, "face_features")
		eyes, _ := requiredStringField(entry, "eyes")
		hairstyle, _ := requiredStringField(entry, "hairstyle")
		bodyType, _ := requiredStringField(entry, "body_type")
		outfit, ok := requiredStringField(entry, "signature_outfit")
		if !ok {
			outfit, _ = requiredStringField(entry, "outfit")
		}
		accessories, _ := requiredStringField(entry, "accessories")
		colorPalette, _ := requiredStringField(entry, "color_palette")
		temperament, _ := requiredStringField(entry, "temperament")
		consistencyNotes, _ := requiredStringField(entry, "consistency_notes")
		blocks = append(blocks, strings.TrimSpace(fmt.Sprintf(`
- 角色 %s (%s, %s)
  外貌锚点: %s
  脸部/五官: %s；%s；眼睛/表情: %s
  年龄/体态: %s；%s
  发型/头部特征: %s
  标志穿着/材质/配饰: %s；%s
  气质/性格: %s；%s
  风格/配色: %s；%s
  一致性要求: 所有画面必须保持同一角色的脸型、五官、年龄感、体型、服饰、材质、主色和辨识特征一致，不允许每一帧变脸、变装、变年龄、变物种。
  补充说明: %s
`, fallbackString(name, charID), fallbackString(charID, "unknown"), fallbackString(typeName, "未注明类型"), fallbackString(appearance, "严格沿用角色圣经，不得随意改写"),
			fallbackString(face, "需根据角色圣经固定脸型/脸蛋轮廓"), fallbackString(faceFeatures, "需固定五官细节"), fallbackString(eyes, "需保持稳定神情"),
			fallbackString(age, "需明确年龄感或幼态/成年态"), fallbackString(bodyType, "需固定体型比例"),
			fallbackString(hairstyle, "如非人角色则固定头部外形/轮廓"), fallbackString(outfit, "需固定标志性穿着、材质或表面纹理"), fallbackString(accessories, "无则明确不佩戴配饰"),
			fallbackString(temperament, "保持稳定气质"), fallbackString(personality, "保持稳定性格外化"), fallbackString(style, "保持统一视觉风格"), fallbackString(colorPalette, "保持统一主色"),
			fallbackString(consistencyNotes, "未提供时也要根据现有设定补全稳定锚点并全片复用"))))
	}
	return strings.Join(blocks, "\n")
}

func buildFilteredCharacterBibleJSON(characterBible any, characterIDs []string) string {
	typed, ok := characterBible.(map[string]any)
	if !ok {
		return "{}"
	}

	filtered := make(map[string]any)
	for _, characterID := range characterIDs {
		characterID = strings.TrimSpace(characterID)
		if characterID == "" {
			continue
		}
		if value, exists := typed[characterID]; exists {
			filtered[characterID] = value
		}
	}
	return compactJSONObject(filtered)
}

func fallbackString(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return fallback
}

func effectiveKeyframeCharacters(scene sceneFile, keyframe keyframe) []string {
	if len(keyframe.Characters) > 0 {
		return keyframe.Characters
	}
	return scene.Characters
}

func buildSceneImagePrompt(storyboard map[string]any, scene sceneFile) string {
	subjectPrompt, _ := requiredStringField(scene.Prompt, "subject_prompt")
	scenePrompt, _ := requiredStringField(scene.Prompt, "scene_prompt")
	fullPrompt, _ := requiredStringField(scene.Prompt, "full_prompt")

	globalStyle := compactJSONObject(storyboard["global_style"])
	characterAnchors := buildCharacterConsistencyPrompt(storyboard["character_bible"], scene.Characters)
	environment := compactJSONObject(scene.Environment)
	visual := compactJSONObject(scene.Visual)
	audio := compactJSONObject(scene.Audio)
	effects := compactJSONObject(scene.Effects)
	aspectRatio := resolveSceneAspectRatio(storyboard)

	return strings.TrimSpace(fmt.Sprintf(`
Generate one storyboard scene image for a children's story video.

Aspect ratio constraint (strict):
- Aspect ratio: %s
- The generated image must strictly follow this aspect ratio composition requirements.
- Composition, camera angle, and subject positioning must all adapt to the %s aspect ratio.

Scene title: %s
Story function: %s
Narration: %s
Only allowed characters in this frame: %s
Subject prompt: %s
Scene prompt: %s
Existing full prompt reference: %s
Global style: %s
Character consistency anchors:
%s
Environment: %s
Visual guidance: %s
Audio reference: %s
Effects mood: %s

Hard consistency rules:
- The same named character must keep the same appearance, face, age impression, clothing, accessories, body proportion, temperament, and signature colors across every generated frame.
- If the character is non-human, keep the same body silhouette, material/texture, facial layout, markings, glow, and recognizable features across all frames.
- Do not add any extra people or faces outside the allowed character list.

Return all generated image options for this scene, ensuring every option matches the same character identity and the %s aspect ratio.
`, aspectRatio, aspectRatio, scene.Title, scene.StoryFunction, scene.Narration, joinCharacterIDs(scene.Characters), subjectPrompt, scenePrompt, fallbackString(fullPrompt, "无"), globalStyle, characterAnchors, environment, visual, audio, effects, aspectRatio))
}

func compactJSONObject(value any) string {
	if value == nil {
		return "{}"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func detectImageExtension(imageURL string, localPath string, mimeType string) string {
	candidates := []string{imageURL, localPath}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		candidate = strings.Split(candidate, "?")[0]
		ext := strings.ToLower(filepath.Ext(candidate))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".webp", ".gif":
			return ext
		}
	}

	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func (a *app) projectStaticURL(projectID string, relativePath string) string {
	base := "/local/projects/" + strings.Trim(projectID, "/")
	trimmed := strings.Trim(filepath.ToSlash(relativePath), "/")
	if trimmed == "" {
		return base + "/"
	}
	return base + "/" + trimmed
}

func appendCacheBuster(rawURL string, version string) string {
	rawURL = strings.TrimSpace(rawURL)
	version = strings.TrimSpace(version)
	if rawURL == "" || version == "" {
		return rawURL
	}
	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
	}
	return rawURL + separator + "v=" + version
}

func preferredSubtitleText(scene sceneFile) string {
	if strings.TrimSpace(scene.Narration) != "" {
		return strings.TrimSpace(scene.Narration)
	}
	return strings.TrimSpace(scene.Subtitle)
}

func estimateSpeechDurationMs(text string, durationHintSec int, speakingRate string) int {
	baseDuration := durationHintSec * 1000
	if baseDuration <= 0 {
		baseDuration = utf8.RuneCountInString(strings.TrimSpace(text))*220 + 1200
	}
	if baseDuration < 2000 {
		baseDuration = 2000
	}
	rateFactor := 1.0 - parsePercent(speakingRate)
	if rateFactor < 0.5 {
		rateFactor = 0.5
	}
	if rateFactor > 2.0 {
		rateFactor = 2.0
	}
	return int(float64(baseDuration) * rateFactor)
}

func parsePercent(value string) float64 {
	text := strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if text == "" {
		return 0
	}
	var number float64
	if _, err := fmt.Sscanf(text, "%f", &number); err != nil {
		return 0
	}
	return number / 100.0
}

func writePlaceholderSpeechWAV(path string, text string, durationMs int) error {
	const sampleRate = 16000
	const channels = 1
	const bitsPerSample = 16

	if durationMs <= 0 {
		durationMs = 3000
	}
	sampleCount := sampleRate * durationMs / 1000
	if sampleCount <= 0 {
		sampleCount = sampleRate * 3
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		runes = []rune("...")
	}

	var buffer bytes.Buffer
	dataSize := sampleCount * channels * (bitsPerSample / 8)
	if _, err := buffer.WriteString("RIFF"); err != nil {
		return err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(36+dataSize)); err != nil {
		return err
	}
	if _, err := buffer.WriteString("WAVEfmt "); err != nil {
		return err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(16)); err != nil {
		return err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(1)); err != nil {
		return err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(channels)); err != nil {
		return err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(sampleRate)); err != nil {
		return err
	}
	byteRate := sampleRate * channels * (bitsPerSample / 8)
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(byteRate)); err != nil {
		return err
	}
	blockAlign := channels * (bitsPerSample / 8)
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(blockAlign)); err != nil {
		return err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint16(bitsPerSample)); err != nil {
		return err
	}
	if _, err := buffer.WriteString("data"); err != nil {
		return err
	}
	if err := binary.Write(&buffer, binary.LittleEndian, uint32(dataSize)); err != nil {
		return err
	}

	segmentSamples := maxInt(sampleCount/len(runes), sampleRate/8)
	for sampleIndex := 0; sampleIndex < sampleCount; sampleIndex++ {
		segmentIndex := (sampleIndex / segmentSamples) % len(runes)
		segmentRune := runes[segmentIndex]
		segmentPhase := float64(sampleIndex%segmentSamples) / float64(segmentSamples)
		envelope := math.Sin(math.Pi * segmentPhase)
		frequency := 220.0 + float64(int(segmentRune)%180)
		sampleTime := float64(sampleIndex) / float64(sampleRate)
		value := int16(7000 * envelope * math.Sin(2*math.Pi*frequency*sampleTime))
		if err := binary.Write(&buffer, binary.LittleEndian, value); err != nil {
			return err
		}
	}

	return os.WriteFile(path, buffer.Bytes(), 0o644)
}

type sceneSubtitleItem struct {
	StartMs int
	EndMs   int
	Text    string
}

func writeSceneSubtitleFiles(srtPath string, assPath string, text string, durationMs int) error {
	items := buildSceneSubtitleItems(text, durationMs)
	if err := os.MkdirAll(filepath.Dir(srtPath), 0o755); err != nil {
		return err
	}
	if err := writeSceneSRTFile(srtPath, items); err != nil {
		return err
	}
	return writeSceneASSFile(assPath, items)
}

func buildSceneSubtitleItems(text string, durationMs int) []sceneSubtitleItem {
	if durationMs <= 0 {
		durationMs = 3000
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = " "
	}
	chunks := splitSubtitleText(text, 0)
	if len(chunks) == 0 {
		chunks = []string{text}
	}

	usableStartMs := 100
	usableEndMs := durationMs - 80
	if durationMs < 900 {
		usableStartMs = 0
		usableEndMs = durationMs
	}
	if usableEndMs <= usableStartMs {
		usableStartMs = 0
		usableEndMs = maxInt(durationMs, 500)
	}

	usableDurationMs := usableEndMs - usableStartMs
	items := make([]sceneSubtitleItem, 0, len(chunks))
	totalWeight := 0
	weights := make([]int, len(chunks))
	for index, chunk := range chunks {
		weight := maxInt(1, countVisibleSubtitleRunes(chunk))
		weights[index] = weight
		totalWeight += weight
	}
	elapsedWeight := 0
	for index, chunk := range chunks {
		startMs := usableStartMs + usableDurationMs*elapsedWeight/totalWeight
		elapsedWeight += weights[index]
		endMs := usableStartMs + usableDurationMs*elapsedWeight/totalWeight
		if endMs <= startMs {
			endMs = startMs + 350
		}
		if endMs > durationMs {
			endMs = durationMs
		}
		items = append(items, sceneSubtitleItem{
			StartMs: startMs,
			EndMs:   endMs,
			Text:    wrapSubtitleText(chunk, 14),
		})
	}
	return items
}

func splitSubtitleText(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	sentences := make([]string, 0)
	var current strings.Builder
	for _, r := range text {
		if r == '\r' {
			continue
		}
		if r == '\n' {
			appendTrimmedSubtitleChunk(&sentences, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
		if strings.ContainsRune("。！？!?；;", r) {
			appendTrimmedSubtitleChunk(&sentences, current.String())
			current.Reset()
		}
	}
	appendTrimmedSubtitleChunk(&sentences, current.String())
	return sentences
}

func appendTrimmedSubtitleChunk(chunks *[]string, text string) {
	text = strings.TrimSpace(text)
	if text != "" {
		*chunks = append(*chunks, text)
	}
}

func wrapSubtitleText(text string, maxRunesPerLine int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunesPerLine {
		return string(runes)
	}
	lines := make([]string, 0, 2)
	for len(runes) > 0 && len(lines) < 2 {
		take := minInt(maxRunesPerLine, len(runes))
		lines = append(lines, strings.TrimSpace(string(runes[:take])))
		runes = runes[take:]
	}
	if len(runes) > 0 && len(lines) > 0 {
		lines[len(lines)-1] = strings.TrimSpace(lines[len(lines)-1] + string(runes))
	}
	return strings.Join(lines, "\n")
}

func writeSceneSRTFile(path string, items []sceneSubtitleItem) error {
	var builder strings.Builder
	for index, item := range items {
		if index > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(fmt.Sprintf("%d\n%s --> %s\n%s\n", index+1, formatSRTTimestamp(item.StartMs), formatSRTTimestamp(item.EndMs), item.Text))
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func writeSceneASSFile(path string, items []sceneSubtitleItem) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var builder strings.Builder
	builder.WriteString(`[Script Info]
ScriptType: v4.00+
PlayResX: 1080
PlayResY: 1920
WrapStyle: 2
ScaledBorderAndShadow: yes

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Main,STHeiti,58,&H00FFFFFF,&H000000FF,&HCC08111F,&H9908111F,-1,0,0,0,100,100,0,0,1,5,2,2,120,120,260,1
Style: PictureBook,STHeiti,64,&H0038D7FF,&H00FFFFFF,&HAA07101D,&H00000000,-1,0,0,0,100,100,0,0,1,5,1,2,120,120,150,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`)
	for _, item := range items {
		builder.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,PictureBook,,0,0,0,,{\\an2\\fad(80,80)}%s\n", formatASSTimestamp(item.StartMs), formatASSTimestamp(item.EndMs), buildASSKaraokeText(item)))
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func buildASSKaraokeText(item sceneSubtitleItem) string {
	visibleRuneCount := countVisibleSubtitleRunes(item.Text)
	if visibleRuneCount <= 0 {
		return escapeASSDialogueText(item.Text)
	}
	durationCs := maxInt(1, (item.EndMs-item.StartMs)/10)
	perRuneCs := maxInt(14, durationCs/visibleRuneCount)
	var builder strings.Builder
	for _, r := range item.Text {
		if r == '\n' {
			builder.WriteString(`\N`)
			continue
		}
		builder.WriteString(fmt.Sprintf("{\\k%d}%s", perRuneCs, escapeASSDialogueText(string(r))))
	}
	return builder.String()
}

func countVisibleSubtitleRunes(text string) int {
	count := 0
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			continue
		}
		count++
	}
	return count
}

func formatSRTTimestamp(durationMs int) string {
	if durationMs < 0 {
		durationMs = 0
	}
	hours := durationMs / 3600000
	minutes := (durationMs % 3600000) / 60000
	seconds := (durationMs % 60000) / 1000
	milliseconds := durationMs % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, milliseconds)
}

func formatASSTimestamp(durationMs int) string {
	if durationMs < 0 {
		durationMs = 0
	}
	centisecondsTotal := int(math.Round(float64(durationMs) / 10.0))
	hours := centisecondsTotal / 360000
	centisecondsTotal %= 360000
	minutes := centisecondsTotal / 6000
	centisecondsTotal %= 6000
	seconds := centisecondsTotal / 100
	centiseconds := centisecondsTotal % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", hours, minutes, seconds, centiseconds)
}

func escapeASSDialogueText(text string) string {
	text = strings.ReplaceAll(text, "\\", "\\\\")
	text = strings.ReplaceAll(text, "{", "")
	text = strings.ReplaceAll(text, "}", "")
	text = strings.ReplaceAll(text, "\n", "\\N")
	return text
}

func resolveVideoDimensions(aspectRatio string, requestedWidth int, requestedHeight int) (int, int) {
	if requestedWidth > 0 && requestedHeight > 0 {
		return requestedWidth, requestedHeight
	}
	switch strings.TrimSpace(aspectRatio) {
	case "9:16":
		return 720, 1280
	case "1:1":
		return 1080, 1080
	default:
		return 1280, 720
	}
}

func runFFmpegSceneCompose(ffmpegPath string, scene sceneFile, outputPath string, width int, height int, fps int) error {
	durationSec := float64(maxInt(scene.AudioDurationMs, scene.SceneDurationMs)) / 1000.0
	if durationSec <= 0 {
		durationSec = float64(maxInt(scene.DurationHintSec, 3))
	}
	useASSSubtitle := sceneHasASSSubtitle(scene) && ffmpegFilterAvailable(ffmpegPath, "ass")
	useCanvasSubtitle := !useASSSubtitle && sceneHasSRTSubtitle(scene)
	ffmpegOutputPath := outputPath
	if useCanvasSubtitle {
		ffmpegOutputPath = outputPath + ".clean.mp4"
		defer os.Remove(ffmpegOutputPath)
	}

	// 检查是否有多个关键帧
	var args []string
	var filter string
	audioInputIndex := 1

	if len(scene.Keyframes) > 0 {
		// 有多张图片，使用关键帧
		imageCount := len(scene.Keyframes)
		frameDurationSec := durationSec / float64(imageCount)
		audioInputIndex = imageCount

		// 构建输入参数
		args = []string{"-y"}
		for _, keyframe := range scene.Keyframes {
			args = append(args, "-loop", "1", "-i", keyframe.ImageLocalPath)
		}
		args = append(args, "-i", scene.AudioLocalPath)

		// 构建滤镜
		filterParts := make([]string, 0, imageCount*2+5)
		for i := 0; i < imageCount; i++ {
			// 为每个图片应用缩放和移动效果
			cameraMotion := resolveSceneCameraMotion(scene)
			motionFilter := buildCameraMotionFilter(width, height, fps, cameraMotion)
			filterParts = append(filterParts, fmt.Sprintf("[%d:v]scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,%s[v%d]", i, width, height, width, height, motionFilter, i))
			// 为每个图片设置时长
			filterParts = append(filterParts, fmt.Sprintf("[v%d]trim=duration=%.3f,setpts=PTS-STARTPTS[v%dtrim]", i, frameDurationSec, i))
		}

		// 拼接所有图片
		if imageCount == 1 {
			filterParts = appendSceneSubtitleFilter(filterParts, "v0trim", "vout", scene, useASSSubtitle)
		} else {
			// 拼接多个图片
			concatInputs := ""
			for i := 0; i < imageCount; i++ {
				concatInputs += fmt.Sprintf("[v%dtrim]", i)
			}
			filterParts = append(filterParts, fmt.Sprintf("%sconcat=n=%d:v=1:a=0[vconcat]", concatInputs, imageCount))
			filterParts = appendSceneSubtitleFilter(filterParts, "vconcat", "vout", scene, useASSSubtitle)
		}

		filter = strings.Join(filterParts, ";")
		args = append(args, "-filter_complex", filter)
	} else {
		// 只有一张图片，使用原有逻辑
		cameraMotion := resolveSceneCameraMotion(scene)
		filter = buildSceneVideoFilter(width, height, fps, cameraMotion, scene, useASSSubtitle)
		args = []string{
			"-y",
			"-loop", "1",
			"-i", scene.ImageLocalPath,
			"-i", scene.AudioLocalPath,
			"-t", fmt.Sprintf("%.3f", durationSec),
			"-filter_complex", filter,
		}
	}

	// 通用参数
	args = append(args, "-map", "[vout]", "-map", fmt.Sprintf("%d:a:0", audioInputIndex), "-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage", "-c:a", "aac", "-movflags", "+faststart", "-shortest", ffmpegOutputPath)

	cmd := exec.Command(ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg scene compose failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if useCanvasSubtitle {
		if err := runCanvasCaptionBurn(ffmpegPath, ffmpegOutputPath, scene.SubtitleLocalPath, outputPath, fps); err != nil {
			return err
		}
	}
	return nil
}

func buildSceneVideoFilter(width int, height int, fps int, cameraMotion string, scene sceneFile, useASSSubtitle bool) string {
	motionFilter := buildCameraMotionFilter(width, height, fps, cameraMotion)
	base := fmt.Sprintf("[0:v]scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,%s[vbase]", width, height, width, height, motionFilter)
	filterParts := appendSceneSubtitleFilter([]string{base}, "vbase", "vout", scene, useASSSubtitle)
	return strings.Join(filterParts, ";")
}

func appendSceneSubtitleFilter(filterParts []string, inputLabel string, outputLabel string, scene sceneFile, useASSSubtitle bool) []string {
	if useASSSubtitle && sceneHasASSSubtitle(scene) {
		assPath := escapeFFmpegFilterValue(scene.SubtitleASSLocalPath)
		return append(filterParts, fmt.Sprintf("[%s]ass=filename='%s',format=yuv420p[%s]", inputLabel, assPath, outputLabel))
	}
	return append(filterParts, fmt.Sprintf("[%s]format=yuv420p[%s]", inputLabel, outputLabel))
}

func escapeFFmpegFilterValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "'", "\\'")
	value = strings.ReplaceAll(value, ":", "\\:")
	value = strings.ReplaceAll(value, ",", "\\,")
	return value
}

func sceneHasASSSubtitle(scene sceneFile) bool {
	return scene.SubtitleASSLocalPath != "" && fileExists(scene.SubtitleASSLocalPath)
}

func sceneHasSRTSubtitle(scene sceneFile) bool {
	return scene.SubtitleLocalPath != "" && fileExists(scene.SubtitleLocalPath)
}

func ffmpegFilterAvailable(ffmpegPath string, filterName string) bool {
	cmd := exec.Command(ffmpegPath, "-hide_banner", "-filters")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		for _, field := range strings.Fields(line) {
			if field == filterName {
				return true
			}
		}
	}
	return false
}

func runCanvasCaptionBurn(ffmpegPath string, cleanVideoPath string, subtitlePath string, outputPath string, fps int) error {
	scriptPath, err := resolveCanvasCaptionBurnScriptPath()
	if err != nil {
		return err
	}
	nodePath := lookupCommand("node")
	if nodePath == "" {
		return errors.New("node not found for canvas caption burn")
	}
	workDir, err := os.MkdirTemp(filepath.Dir(outputPath), "caption_burn_*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	framesDir := filepath.Join(workDir, "frames")
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return err
	}
	extractCmd := exec.Command(ffmpegPath,
		"-y",
		"-hide_banner",
		"-loglevel", "error",
		"-i", cleanVideoPath,
		filepath.Join(framesDir, "%06d.png"),
	)
	var extractStderr bytes.Buffer
	extractCmd.Stderr = &extractStderr
	if err := extractCmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg frame extraction failed: %w: %s", err, strings.TrimSpace(extractStderr.String()))
	}

	captionedFramesDir := filepath.Join(workDir, "frames_captioned")
	nodeCmd := exec.Command(nodePath,
		scriptPath,
		"--frames-dir", framesDir,
		"--srt", subtitlePath,
		"--out-frames-dir", captionedFramesDir,
		"--audio-video", cleanVideoPath,
		"--output", outputPath,
		"--fps", strconv.Itoa(fps),
		"--ffmpeg", ffmpegPath,
	)
	var nodeOutput bytes.Buffer
	nodeCmd.Stdout = &nodeOutput
	nodeCmd.Stderr = &nodeOutput
	if err := nodeCmd.Run(); err != nil {
		return fmt.Errorf("canvas caption burn failed: %w: %s", err, strings.TrimSpace(nodeOutput.String()))
	}
	return nil
}

func resolveCanvasCaptionBurnScriptPath() (string, error) {
	if scriptPath := strings.TrimSpace(os.Getenv("STORY_VIDEO_CAPTION_BURN_SCRIPT")); scriptPath != "" {
		if fileExists(scriptPath) {
			return scriptPath, nil
		}
		return "", fmt.Errorf("caption burn script not found at %s", scriptPath)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(cwd, "scripts", "burn_canvas_captions.mjs"),
		filepath.Join(cwd, "..", "..", "scripts", "burn_canvas_captions.mjs"),
		filepath.Join(cwd, "..", "scripts", "burn_canvas_captions.mjs"),
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("caption burn script not found")
}

func buildCameraMotionFilter(width int, height int, fps int, cameraMotion string) string {
	switch strings.TrimSpace(cameraMotion) {
	case "slow_zoom_out":
		return fmt.Sprintf("zoompan=z='if(lte(on,1),1.10,max(1.0,zoom-0.0006))':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)':d=1:s=%dx%d:fps=%d", width, height, fps)
	case "pan_left":
		return fmt.Sprintf("zoompan=z='1.05':x='max(0,iw/8-on*0.4)':y='ih/2-(ih/zoom/2)':d=1:s=%dx%d:fps=%d", width, height, fps)
	case "pan_right":
		return fmt.Sprintf("zoompan=z='1.05':x='min(iw-iw/zoom, on*0.4)':y='ih/2-(ih/zoom/2)':d=1:s=%dx%d:fps=%d", width, height, fps)
	case "floating_drift":
		return fmt.Sprintf("zoompan=z='1.03+0.02*sin(on/24)':x='iw/2-(iw/zoom/2)+20*sin(on/30)':y='ih/2-(ih/zoom/2)+14*cos(on/28)':d=1:s=%dx%d:fps=%d", width, height, fps)
	default:
		return fmt.Sprintf("zoompan=z='min(zoom+0.0007,1.10)':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)':d=1:s=%dx%d:fps=%d", width, height, fps)
	}
}

func resolveSceneCameraMotion(scene sceneFile) string {
	if cameraMotion, ok := requiredStringField(scene.Visual, "camera_motion"); ok {
		return cameraMotion
	}
	return "slow_zoom_in"
}

func runFFmpegFinalCompose(ffmpegPath string, scenes []sceneFile, outputPath string, width int, height int, fps int, transition string, transitionDurationMs int) error {
	if len(scenes) == 1 {
		cmd := exec.Command(ffmpegPath,
			"-y",
			"-i", scenes[0].SceneVideoLocalPath,
			"-vf", fmt.Sprintf("fps=%d,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,format=yuv420p", fps, width, height, width, height),
			"-af", "aresample=async=1:first_pts=0",
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-c:a", "aac",
			"-movflags", "+faststart",
			outputPath,
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ffmpeg final compose failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}

	args := []string{"-y"}
	filterParts := make([]string, 0, len(scenes)*2)
	for index, scene := range scenes {
		args = append(args, "-i", scene.SceneVideoLocalPath)
		filterParts = append(filterParts,
			fmt.Sprintf("[%d:v]settb=AVTB,fps=%d,scale=%d:%d,format=yuv420p[v%d]", index, fps, width, height, index),
			fmt.Sprintf("[%d:a]aresample=async=1:first_pts=0[a%d]", index, index),
		)
	}

	transitionSec := float64(transitionDurationMs) / 1000.0
	if transitionSec <= 0 {
		transitionSec = 0.5
	}
	videoLabel := "v0"
	audioLabel := "a0"
	accumulatedDurationSec := float64(maxInt(scenes[0].SceneDurationMs, 1000)) / 1000.0
	for index := 1; index < len(scenes); index++ {
		offsetSec := accumulatedDurationSec - transitionSec
		if offsetSec < 0 {
			offsetSec = 0
		}
		nextVideoLabel := fmt.Sprintf("vx%d", index)
		nextAudioLabel := fmt.Sprintf("ax%d", index)
		filterParts = append(filterParts,
			fmt.Sprintf("[%s][v%d]xfade=transition=%s:duration=%.3f:offset=%.3f[%s]", videoLabel, index, transition, transitionSec, offsetSec, nextVideoLabel),
			fmt.Sprintf("[%s][a%d]acrossfade=d=%.3f[%s]", audioLabel, index, transitionSec, nextAudioLabel),
		)
		videoLabel = nextVideoLabel
		audioLabel = nextAudioLabel
		accumulatedDurationSec += float64(maxInt(scenes[index].SceneDurationMs, 1000))/1000.0 - transitionSec
	}

	args = append(args,
		"-filter_complex", strings.Join(filterParts, ";"),
		"-map", "["+videoLabel+"]",
		"-map", "["+audioLabel+"]",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-c:a", "aac",
		"-movflags", "+faststart",
		outputPath,
	)
	cmd := exec.Command(ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg final compose failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func writeScenePreviewHTML(path string, projectID string, scene sceneFile) error {
	// 构建图片轮播部分
	var imageSlider string
	if len(scene.Keyframes) > 0 {
		// 多张图片，生成轮播
		imageSlider = `<div class="image-slider">
`
		for i, keyframe := range scene.Keyframes {
			var displayStyle string
			if i == 0 {
				displayStyle = "style=\"display: block;\""
			} else {
				displayStyle = ""
			}
			imageSlider += fmt.Sprintf(`			<img src="%s" alt="关键帧 %d" class="slide" %s />
`, keyframe.ImagePreviewURL, i+1, displayStyle)
		}
		imageSlider += `		</div>
		<script>
			let currentSlide = 0;
			const slides = document.querySelectorAll('.slide');
			const totalSlides = slides.length;
			
			function nextSlide() {
				slides[currentSlide].style.display = 'none';
				currentSlide = (currentSlide + 1) % totalSlides;
				slides[currentSlide].style.display = 'block';
			}
			
			// 自动切换图片，每张图片显示3秒
			setInterval(nextSlide, 3000);
		</script>`
	} else {
		// 单张图片
		imageSlider = fmt.Sprintf(`<img src="%s" alt="%s" />`, scene.ImagePreviewURL, html.EscapeString(scene.Title))
	}

	body := fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>%s</title>
    <style>
      body { margin: 0; background: #050814; color: #fff; font-family: sans-serif; }
      .frame { min-height: 100vh; display: grid; place-items: center; padding: 24px; box-sizing: border-box; }
      .card { width: min(92vw, 820px); background: #0f172a; border: 1px solid #26324f; border-radius: 18px; overflow: hidden; box-shadow: 0 24px 80px rgba(0, 0, 0, 0.35); }
      img { width: 100%%; display: block; background: #111827; }
      .image-slider { position: relative; width: 100%%; overflow: hidden; }
      .slide { display: none; width: 100%%; }
      .meta { padding: 18px; }
      h1 { margin: 0 0 10px; font-size: 22px; }
      p { margin: 8px 0; line-height: 1.6; color: #d7def5; }
      audio { width: 100%%; margin-top: 12px; }
      .hint { font-size: 13px; color: #90a0c2; }
    </style>
  </head>
  <body>
    <div class="frame">
      <section class="card">
        %s
        <div class="meta">
          <h1>%s</h1>
          <p>%s</p>
          <audio controls autoplay src="%s"></audio>
          <p class="hint">当前环境未检测到 ffmpeg，已生成 HTML 预览 fallback。安装 ffmpeg 后再次调用 /video 接口即可生成 MP4。</p>
          <p class="hint">项目：%s / Scene：%s</p>
        </div>
      </section>
    </div>
  </body>
</html>
`, html.EscapeString(scene.Title), imageSlider, html.EscapeString(scene.Title), html.EscapeString(scene.Narration), scene.AudioPreviewURL, html.EscapeString(projectID), html.EscapeString(scene.SceneID))
	return os.WriteFile(path, []byte(body), 0o644)
}

func writeFinalVideoPreviewHTML(path string, projectID string, projectTitle string, scenes []sceneFile) error {
	var items strings.Builder
	for _, scene := range scenes {
		items.WriteString("<section class=\"scene\">")
		items.WriteString("<h2>" + html.EscapeString(scene.Title) + "</h2>")
		if strings.HasSuffix(strings.ToLower(scene.SceneVideoPreviewURL), ".mp4") {
			items.WriteString("<video controls preload=\"metadata\" src=\"" + html.EscapeString(scene.SceneVideoPreviewURL) + "\"></video>")
		} else if scene.SceneVideoPreviewURL != "" {
			items.WriteString("<iframe src=\"" + html.EscapeString(scene.SceneVideoPreviewURL) + "\" loading=\"lazy\"></iframe>")
		} else if scene.ImagePreviewURL != "" {
			items.WriteString("<img src=\"" + html.EscapeString(scene.ImagePreviewURL) + "\" alt=\"" + html.EscapeString(scene.Title) + "\" />")
			if scene.AudioPreviewURL != "" {
				items.WriteString("<audio controls src=\"" + html.EscapeString(scene.AudioPreviewURL) + "\"></audio>")
			}
		}
		items.WriteString("<p>" + html.EscapeString(scene.Narration) + "</p>")
		items.WriteString("</section>")
	}

	body := fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>%s</title>
    <style>
      body { margin: 0; font-family: sans-serif; background: #08101f; color: #fff; }
      main { max-width: 1080px; margin: 0 auto; padding: 24px; }
      h1 { margin-top: 0; }
      .hint { color: #97a5c6; }
      .scene { background: #101a30; border: 1px solid #25304b; border-radius: 16px; padding: 18px; margin-bottom: 18px; }
      video, iframe, img, audio { width: 100%%; border: 0; border-radius: 12px; display: block; margin-top: 12px; background: #000; }
      iframe { min-height: 640px; }
    </style>
  </head>
  <body>
    <main>
      <h1>%s</h1>
      <p class="hint">项目：%s。当前环境缺少 ffmpeg 或 scene mp4 不完整，已生成 final_video HTML fallback。</p>
      %s
    </main>
  </body>
</html>
`, html.EscapeString(projectTitle), html.EscapeString(projectTitle), html.EscapeString(projectID), items.String())
	return os.WriteFile(path, []byte(body), 0o644)
}

func (a *app) generateSceneAudioMedia(audioPath string, subtitlePath string, scene sceneFile, providerRef string, voiceName string, speakingRate string, pitch string, fallbackDurationMs int) error {
	if shouldUseKokoroTTS(providerRef) {
		return a.runKokoroTTS(audioPath, scene.Narration, voiceName, speakingRate)
	}
	if shouldUseEdgeTTS(providerRef) {
		return runEdgeTTS(audioPath, subtitlePath, scene.Narration, voiceName, speakingRate, pitch)
	}
	return writePlaceholderSpeechWAV(audioPath, scene.Narration, fallbackDurationMs)
}

func (a *app) runKokoroTTS(audioPath string, text string, voiceName string, speakingRate string) error {
	runtime, err := a.resolveKokoroRuntime()
	if err != nil {
		return err
	}
	textFile, err := os.CreateTemp(filepath.Dir(audioPath), "kokoro_text_*.txt")
	if err != nil {
		return err
	}
	textPath := textFile.Name()
	if _, err := textFile.WriteString(text); err != nil {
		_ = textFile.Close()
		_ = os.Remove(textPath)
		return err
	}
	if err := textFile.Close(); err != nil {
		_ = os.Remove(textPath)
		return err
	}
	defer os.Remove(textPath)

	args := []string{
		"run",
		"--no-project",
		"--python", runtime.pythonPath,
		"--with-editable", runtime.abogenPath,
		"python", runtime.scriptPath,
		"--text-file", textPath,
		"--voice", fallbackString(voiceName, defaultKokoroVoiceName),
		"--speed", fmt.Sprintf("%.2f", kokoroSpeedFromSpeakingRate(speakingRate)),
		"--output", audioPath,
	}
	cmd := exec.Command(runtime.uvPath, args...)
	cmd.Dir = a.rootDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kokoro tts failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

type kokoroRuntime struct {
	uvPath     string
	pythonPath string
	abogenPath string
	scriptPath string
}

func (a *app) kokoroTTSHealth() map[string]any {
	runtime, err := a.resolveKokoroRuntime()
	if err != nil {
		return map[string]any{
			"ok":    false,
			"error": err.Error(),
		}
	}
	return map[string]any{
		"ok":           true,
		"provider_ref": defaultKokoroProviderRef,
		"voice_name":   defaultKokoroVoiceName,
		"uv":           runtime.uvPath,
		"python":       runtime.pythonPath,
		"abogen_path":  runtime.abogenPath,
		"script":       runtime.scriptPath,
	}
}

func (a *app) kokoroTTSAvailable() bool {
	_, err := a.resolveKokoroRuntime()
	return err == nil
}

func (a *app) kokoroTTSUnavailableReason() string {
	_, err := a.resolveKokoroRuntime()
	if err == nil {
		return ""
	}
	return err.Error()
}

func (a *app) resolveKokoroRuntime() (kokoroRuntime, error) {
	uvPath := strings.TrimSpace(os.Getenv("KOKORO_UV_BIN"))
	if uvPath == "" {
		uvPath = lookupCommand("uv")
	}
	if uvPath == "" {
		return kokoroRuntime{}, errors.New("uv not found")
	}

	pythonPath := strings.TrimSpace(os.Getenv("KOKORO_PYTHON"))
	if pythonPath == "" {
		for _, candidate := range []string{
			"/Users/mac/.local/bin/python3.12",
			lookupCommand("python3.12"),
			lookupCommand("python3"),
		} {
			if candidate != "" && fileExists(candidate) {
				pythonPath = candidate
				break
			}
		}
	}
	if pythonPath == "" {
		return kokoroRuntime{}, errors.New("python3.12/python3 not found")
	}

	abogenPath := strings.TrimSpace(os.Getenv("KOKORO_ABOGEN_PATH"))
	if abogenPath == "" {
		abogenPath = strings.TrimSpace(os.Getenv("ABOGEN_PATH"))
	}
	if abogenPath == "" {
		abogenPath = filepath.Clean(filepath.Join(a.rootDir, "..", "abogen"))
	}
	if !fileExists(filepath.Join(abogenPath, "pyproject.toml")) {
		return kokoroRuntime{}, fmt.Errorf("abogen pyproject.toml not found at %s", abogenPath)
	}

	scriptPath := strings.TrimSpace(os.Getenv("KOKORO_TTS_SCRIPT"))
	if scriptPath == "" {
		scriptPath = filepath.Join(a.rootDir, "scripts", "kokoro_tts.py")
	}
	if !fileExists(scriptPath) {
		return kokoroRuntime{}, fmt.Errorf("kokoro script not found at %s", scriptPath)
	}

	return kokoroRuntime{
		uvPath:     uvPath,
		pythonPath: pythonPath,
		abogenPath: abogenPath,
		scriptPath: scriptPath,
	}, nil
}

func kokoroSpeedFromSpeakingRate(value string) float64 {
	speed := 1.0 + parsePercent(value)
	if speed < 0.1 {
		return 0.1
	}
	if speed > 2.0 {
		return 2.0
	}
	return speed
}

func looksLikeKokoroVoiceName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return strings.Contains(value, "_") && !strings.Contains(value, "-")
}

func runEdgeTTS(audioPath string, subtitlePath string, text string, voiceName string, speakingRate string, pitch string) error {
	edgeTTSPath, err := exec.LookPath("edge-tts")
	if err != nil {
		return err
	}
	args := []string{
		"--text", text,
		"--voice", voiceName,
		"--rate=" + normalizeEdgeRate(speakingRate),
		"--pitch=" + normalizeEdgePitch(pitch),
		"--write-media", audioPath,
		"--write-subtitles", subtitlePath,
	}
	cmd := exec.Command(edgeTTSPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("edge-tts failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func normalizeEdgeRate(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return "+0%"
	}
	if trimmed, ok := strings.CutSuffix(text, "%"); ok {
		return ensureSignedValue(trimmed) + "%"
	}
	return ensureSignedValue(text) + "%"
}

func normalizeEdgePitch(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return "+0Hz"
	}
	lowerText := strings.ToLower(text)
	if strings.HasSuffix(lowerText, "hz") {
		return ensureSignedValue(strings.TrimSpace(text[:len(text)-2])) + "Hz"
	}
	// Accept the old percent-shaped input as a backwards-compatible alias.
	if trimmed, ok := strings.CutSuffix(text, "%"); ok {
		return ensureSignedValue(trimmed) + "Hz"
	}
	return ensureSignedValue(text) + "Hz"
}

func ensureSignedValue(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return "+0"
	}
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
		return text
	}
	return "+" + text
}

func lookupCommand(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func probeMediaDurationMs(path string) int {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}
	cmd := exec.Command(ffprobePath, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return 0
	}
	value := strings.TrimSpace(stdout.String())
	durationSec, err := strconv.ParseFloat(value, 64)
	if err != nil || durationSec <= 0 {
		return 0
	}
	return int(math.Round(durationSec * 1000))
}

func isSceneVideoReadyForFinalCompose(scene sceneFile) bool {
	if scene.ComposeStatus != "success" {
		return false
	}
	if scene.SceneVideoLocalPath == "" || !fileExists(scene.SceneVideoLocalPath) {
		return false
	}
	if strings.ToLower(filepath.Ext(scene.SceneVideoLocalPath)) != ".mp4" {
		return false
	}
	return scene.ComposeMode != "html_preview_fallback"
}

func isSceneVideoPreviewReady(scene sceneFile) bool {
	if scene.ComposeStatus != "success" && scene.ComposeStatus != "preview_ready" {
		return false
	}
	return scene.SceneVideoLocalPath != "" && fileExists(scene.SceneVideoLocalPath)
}

func estimateFinalVideoDurationMs(scenes []sceneFile, transitionDurationMs int) int {
	if len(scenes) == 0 {
		return 0
	}
	total := 0
	for _, scene := range scenes {
		total += maxInt(scene.SceneDurationMs, 1000)
	}
	total -= transitionDurationMs * maxInt(len(scenes)-1, 0)
	if total < 0 {
		return 0
	}
	return total
}

func allSceneVideosAreMP4(scenes []sceneFile) bool {
	if len(scenes) == 0 {
		return false
	}
	for _, scene := range scenes {
		if strings.ToLower(filepath.Ext(scene.SceneVideoLocalPath)) != ".mp4" {
			return false
		}
	}
	return true
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func validateStoryboard(raw []byte, projectID string, validatedAt string) (storyboardValidationResult, []sceneFile) {
	result := storyboardValidationResult{
		Valid:       false,
		Errors:      []string{},
		ValidatedAt: validatedAt,
	}

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		result.Errors = append(result.Errors, "storyboard.json is not valid JSON")
		return result, nil
	}

	requiredTopLevel := []string{
		"meta",
		"project",
		"story_plan",
		"global_style",
		"character_bible",
		"audio_profile",
		"video_profile",
		"render_rules",
		"scenes",
	}
	for _, key := range requiredTopLevel {
		if _, ok := root[key]; !ok {
			result.Errors = append(result.Errors, fmt.Sprintf("missing top-level field: %s", key))
		}
	}

	scenesValue, ok := root["scenes"]
	if !ok {
		return result, nil
	}

	sceneItems, ok := scenesValue.([]any)
	if !ok {
		result.Errors = append(result.Errors, "scenes must be an array")
		return result, nil
	}

	result.SceneCount = len(sceneItems)
	if storyPlan, ok := root["story_plan"].(map[string]any); ok {
		if cycleCount, ok := requiredPositiveIntField(storyPlan, "cycle_count"); !ok {
			result.Errors = append(result.Errors, "story_plan.cycle_count must be 3 or 5")
		} else {
			if cycleCount != 3 && cycleCount != 5 {
				result.Errors = append(result.Errors, "story_plan.cycle_count must be 3 or 5")
			}
			if cycleCount != len(sceneItems) {
				result.Errors = append(result.Errors, "story_plan.cycle_count must match scenes count")
			}
		}
		if _, ok := requiredStringField(storyPlan, "cycle_reason"); !ok {
			result.Errors = append(result.Errors, "story_plan.cycle_reason is required")
		}
		if _, ok := storyPlan["cycles"].([]any); !ok {
			result.Errors = append(result.Errors, "story_plan.cycles must be an array")
		}
	} else {
		result.Errors = append(result.Errors, "story_plan must be an object")
	}
	if len(sceneItems) == 0 {
		result.Errors = append(result.Errors, "scenes must not be empty")
	}
	if len(sceneItems) != 3 && len(sceneItems) != 5 {
		result.Errors = append(result.Errors, "scenes count must be 3 or 5 for Story Video")
	}

	seenSceneIDs := make(map[string]struct{}, len(sceneItems))
	scenes := make([]sceneFile, 0, len(sceneItems))
	for index, item := range sceneItems {
		sceneMap, ok := item.(map[string]any)
		if !ok {
			result.Errors = append(result.Errors, fmt.Sprintf("scenes[%d] must be an object", index))
			continue
		}
		sceneErrors := make([]string, 0)

		sceneID, ok := requiredStringField(sceneMap, "scene_id")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].scene_id is required", index))
		} else {
			if _, exists := seenSceneIDs[sceneID]; exists {
				sceneErrors = append(sceneErrors, fmt.Sprintf("duplicate scene_id: %s", sceneID))
			}
			seenSceneIDs[sceneID] = struct{}{}
		}

		sequence, ok := requiredPositiveIntField(sceneMap, "sequence")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].sequence must be a positive integer", index))
		}
		title, ok := requiredStringField(sceneMap, "title")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].title is required", index))
		}
		storyFunction, ok := requiredStringField(sceneMap, "story_function")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].story_function is required", index))
		}
		narration, ok := requiredStringField(sceneMap, "narration")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].narration is required", index))
		}
		subtitle, ok := requiredStringField(sceneMap, "subtitle")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].subtitle is required", index))
		}
		durationHintSec, ok := requiredPositiveIntField(sceneMap, "duration_hint_sec")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].duration_hint_sec must be a positive integer", index))
		}
		characters, ok := requiredStringSliceField(sceneMap, "characters")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].characters must be a string array", index))
		}
		objects, ok := requiredStringSliceField(sceneMap, "objects")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].objects must be a string array", index))
		}
		environment, ok := requiredObjectField(sceneMap, "environment")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].environment must be an object", index))
		}
		visual, ok := requiredObjectField(sceneMap, "visual")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].visual must be an object", index))
		}
		prompt, ok := requiredObjectField(sceneMap, "prompt")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].prompt must be an object", index))
		} else {
			if _, hasSubjectPrompt := requiredStringField(prompt, "subject_prompt"); !hasSubjectPrompt {
				sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].prompt.subject_prompt is required", index))
			}
			if _, hasScenePrompt := requiredStringField(prompt, "scene_prompt"); !hasScenePrompt {
				sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].prompt.scene_prompt is required", index))
			}
			if _, exists := prompt["full_prompt"]; !exists {
				sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].prompt.full_prompt is required", index))
			}
		}
		audio, ok := requiredObjectField(sceneMap, "audio")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].audio must be an object", index))
		}
		effects, ok := requiredObjectField(sceneMap, "effects")
		if !ok {
			sceneErrors = append(sceneErrors, fmt.Sprintf("scenes[%d].effects must be an object", index))
		}

		if len(sceneErrors) > 0 {
			result.Errors = append(result.Errors, sceneErrors...)
			continue
		}

		// 处理 keyframes 字段
		var keyframes []keyframe
		if keyframesValue, ok := sceneMap["keyframes"]; ok {
			if keyframesArray, ok := keyframesValue.([]any); ok {
				for _, kfItem := range keyframesArray {
					if kfMap, ok := kfItem.(map[string]any); ok {
						frameID, _ := requiredStringField(kfMap, "frame_id")
						seq, _ := requiredPositiveIntField(kfMap, "sequence")
						characters, _ := requiredStringSliceField(kfMap, "characters")
						prompt, _ := requiredObjectField(kfMap, "prompt")
						visual, _ := requiredObjectField(kfMap, "visual")
						keyframes = append(keyframes, keyframe{
							FrameID:    frameID,
							Sequence:   seq,
							Characters: characters,
							Prompt:     prompt,
							Visual:     visual,
						})
					}
				}
			}
		}

		scenes = append(scenes, sceneFile{
			ProjectID:       projectID,
			SceneID:         sceneID,
			Sequence:        sequence,
			Title:           title,
			StoryFunction:   storyFunction,
			Narration:       narration,
			Subtitle:        subtitle,
			DurationHintSec: durationHintSec,
			Characters:      characters,
			Objects:         objects,
			Environment:     environment,
			Visual:          visual,
			Prompt:          prompt,
			Audio:           audio,
			Effects:         effects,
			Status:          "pending",
			ImageStatus:     "pending",
			AudioStatus:     "pending",
			ComposeStatus:   "pending",
			Keyframes:       keyframes,
			CreatedAt:       validatedAt,
			UpdatedAt:       validatedAt,
		})
	}

	result.Valid = len(result.Errors) == 0
	if !result.Valid {
		return result, nil
	}
	return result, scenes
}

func requiredStringField(obj map[string]any, key string) (string, bool) {
	value, ok := obj[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", false
	}
	return strings.TrimSpace(text), true
}

func requiredObjectField(obj map[string]any, key string) (map[string]any, bool) {
	value, ok := obj[key]
	if !ok {
		return nil, false
	}
	child, ok := value.(map[string]any)
	return child, ok
}

func requiredStringSliceField(obj map[string]any, key string) ([]string, bool) {
	value, ok := obj[key]
	if !ok {
		return nil, false
	}
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, false
		}
		result = append(result, strings.TrimSpace(text))
	}
	return result, true
}

func requiredPositiveIntField(obj map[string]any, key string) (int, bool) {
	value, ok := obj[key]
	if !ok {
		return 0, false
	}
	switch number := value.(type) {
	case float64:
		if number <= 0 {
			return 0, false
		}
		return int(number), true
	case int:
		if number <= 0 {
			return 0, false
		}
		return number, true
	default:
		return 0, false
	}
}

func buildBaseStoryboardPrompt(project projectFile) string {
	var durationInfo string
	if project.TargetDurationSec > 0 {
		durationInfo = fmt.Sprintf("- 目标视频总时长：%d 秒\n", project.TargetDurationSec)
	}
	var imageSwitchInfo string
	if project.ImageSwitchIntervalSec > 0 {
		imageSwitchInfo = fmt.Sprintf("- 图片切换间隔：%d 秒\n", project.ImageSwitchIntervalSec)
	}
	aspectRatio := project.AspectRatio
	if aspectRatio == "" {
		aspectRatio = "9:16"
	}
	var aspectRatioInfo string = fmt.Sprintf("- 画面比例：%s（所有场景的视觉描述和提示词必须符合此画面比例）\n", aspectRatio)

	// 根据目标时长和儿童慢速 TTS 计算 narration 总字数要求
	var narrationLengthInfo string
	if project.TargetDurationSec > 0 {
		totalChars := int(float64(project.TargetDurationSec) * baseCharsPerSec)
		narrationLengthInfo = fmt.Sprintf(`- TTS 语速基准：%.1f 字/秒（儿童慢速旁白，默认 speaking_rate=%s）。
	- 旁白总字数要求：所有 scene 的 narration 字段的中文字符数之和必须接近 %d 字（允许 ±10%% 误差）。这是硬性要求，请严格控制每个 scene 的 narration 长度，确保总和符合要求。
	- duration_hint_sec 计算规则：每个 scene 的 duration_hint_sec 必须根据该 scene 的 narration 字数 ÷ %.1f 字/秒 来计算，不要平均分配时长。narration 越长的场景 duration_hint_sec 越大，narration 越短的场景 duration_hint_sec 越小。所有 scene 的 duration_hint_sec 之和应接近 %d 秒。
	`, baseCharsPerSec, defaultChildSpeakingRate, totalChars, baseCharsPerSec, project.TargetDurationSec)
	}

	return strings.TrimSpace(fmt.Sprintf(`
请基于用户给出的主题或故事要求，规划并生成严格 JSON 的绘本故事视频 storyboard.json。

必须满足：
1. 只返回 JSON，不要返回 markdown，不要解释。
2. 顶层必须包含：meta, project, story_plan, global_style, character_bible, audio_profile, video_profile, render_rules, scenes。
3. scenes 必须是数组，每个 scene 必须包含：scene_id, sequence, title, story_function, narration, subtitle, duration_hint_sec, characters, objects, environment, visual, prompt, audio, effects。
4. environment 必须是 object，不能是 string。visual 必须是 object，不能是 string。effects 必须是 object，不能是 string。
5. prompt 必须是 object，并且必须包含：subject_prompt, scene_prompt, full_prompt。subject_prompt 和 scene_prompt 必须为非空字符串；full_prompt 先返回空字符串。
6. audio 必须是 object。
7. 内容适合儿童故事视频，语气温和，结构清晰。
8. JSON 的第一个字符必须是 {，最后一个字符必须是 }。
9. video_profile 中必须包含 aspect_ratio 字段，值为 %s。
10. story_plan 必须是 object，包含 cycle_count, cycle_reason, cycles。cycle_count 只能是 3 或 5。
11. 如果输入中的“故事周期模式”为 3，则 cycle_count 必须为 3；如果为 5，则 cycle_count 必须为 5；如果为 auto，则根据主题复杂度和目标时长自动判断：60 秒以内优先 3 周期，超过 60 秒或主题角色/冲突更丰富时优先 5 周期。
12. scenes 数量必须等于 story_plan.cycle_count。3 周期使用“起因、转折、解决”；5 周期使用“引入、问题、探索、低谷/转折、解决”。
	13. audio_profile.tts_provider_ref 必须使用 "%s"，audio_profile.voice_name 必须使用 "%s"，audio_profile.speaking_rate 必须使用 "%s"。
14. meta.image_provider_ref 必须使用 "%s"，不得推荐或填写其它图片模型。
15. character_bible 中每个角色都必须尽量具体，至少要稳定描述：char_id, name, type, appearance, personality, style，并且必须尽可能补充以下字段来锁定角色一致性：age、gender_presentation、face、face_shape、face_features、eyes、eyebrows、nose、mouth、hairstyle、hair_color、body_type、height_impression、skin_tone 或 body_surface、signature_outfit 或 outfit、upper_clothing、lower_clothing、shoes、accessories、color_palette、temperament、expression_habit、gesture_habit、consistency_notes。不要只写“可爱的小女孩”或“帅气少年”这种模糊描述，必须写到可以稳定复现同一角色的程度。
16. 如果角色是非人类、动物、精灵、云朵、星星、玩偶等，也必须把对应的“脸部布局/表情区域、轮廓比例、材质、表面纹理、发光方式、标志花纹、主色和辨识特征”写具体，保证跨场景生成时仍然是同一个角色。
17. scenes[*].prompt.subject_prompt 必须直接写出当前场景角色的稳定外貌锚点，不要只写角色编号，必须显式体现外貌长相、脸蛋/脸型、五官、年龄感、气质、特征、衣物穿着、配饰、体态，或非人角色的对应特征。

character_bible 中单个角色的推荐详细结构示例：
{
  "char_id": "c01",
  "name": "棉棉",
  "type": "主角小云朵",
  "age": "儿童感、约 6 岁的幼态气质",
  "gender_presentation": "中性偏女孩气质",
  "appearance": "圆润蓬松的小云朵，整体雪白，边缘像棉花糖一样柔软",
  "face": "圆圆的脸蛋轮廓，脸部区域位于云朵正前方中央",
  "face_shape": "圆脸、幼态",
  "face_features": "脸颊饱满，带淡淡粉晕，小下巴不明显",
  "eyes": "黑亮的大眼睛，眼距略宽，眼神温柔",
  "eyebrows": "短短的弯眉，表情柔和",
  "mouth": "小巧微笑嘴型",
  "hairstyle": "头顶有一缕轻轻翘起的云尖",
  "hair_color": "无，保持雪白云朵本体",
  "body_type": "圆滚滚、轻盈、小巧",
  "height_impression": "比身旁小星星略大一圈",
  "body_surface": "柔软云朵绒感，边缘带淡淡柔光",
  "signature_outfit": "系着浅蓝色小围巾",
  "accessories": ["浅蓝色小围巾"],
  "color_palette": ["雪白", "浅蓝", "淡粉"],
  "temperament": "温柔、治愈、勇敢",
  "expression_habit": "常带轻柔微笑和关切眼神",
  "gesture_habit": "说话时会轻轻前倾、靠近对方",
  "consistency_notes": "所有场景都保持圆脸幼态、雪白云朵体积、浅蓝围巾、温柔大眼和淡粉脸颊"
}

scene 的最小合法结构示例：
{
  "scene_id": "s01",
  "sequence": 1,
  "title": "场景标题",
  "story_function": "这一幕承担的叙事作用",
  "narration": "旁白全文",
  "subtitle": "字幕文本",
  "duration_hint_sec": 8,
  "characters": ["c01"],
  "objects": ["星星瓶"],
  "environment": {
    "location": "夜空",
    "time_of_day": "夜晚",
    "weather": "晴朗",
    "atmosphere": "梦幻温馨"
  },
  "visual": {
    "shot_type": "全景",
    "camera_motion": "缓慢推进",
    "composition": "主角位于画面中央",
    "action": "小云朵轻轻漂浮，望向小星星"
  },
  "prompt": {
    "subject_prompt": "主角与关键物体的画面描述",
    "scene_prompt": "场景环境、镜头和氛围描述",
    "full_prompt": ""
  },
  "audio": {
    "bgm": "背景音乐描述",
    "voice": "人声描述",
    "sound_effect": "音效描述"
  },
  "effects": {
    "motion": "元素运动效果",
    "lighting": "光效描述",
    "post_process": "后期风格描述"
  }
}

注意：
- 不要把 environment、visual、effects 写成一句话字符串。
- 不要遗漏 prompt.subject_prompt 或 prompt.scene_prompt。
- story_plan、character_bible、audio_profile、video_profile、render_rules 也要保持 object/array 结构，不要输出自然语言段落。
- 你要先规划故事线，再输出 scenes；但最终回答只允许是一个 JSON 对象。
- character_bible 的角色设定要足够具体，后续所有场景都要严格复用同一角色的脸、年龄感、服装/材质、主色和辨识特征。
- 角色描述必须尽量覆盖：外貌长相、脸蛋/脸型、五官、年纪、气质、体态、衣物、穿着、配饰、主色、材质、习惯表情。
- 根据目标视频总时长控制故事的长度和场景数量，确保 narration 的总字数适合目标时长。
- 所有场景的视觉描述和提示词必须符合画面比例 %s 的构图要求。
%s
项目信息：
- project_id: %s
- title: %s
%s%s%s
用户输入：
%s
	`, aspectRatio, defaultKokoroProviderRef, defaultKokoroVoiceName, defaultChildSpeakingRate, defaultCodexImageProviderRef, aspectRatio, narrationLengthInfo, project.ProjectID, project.Title, durationInfo, imageSwitchInfo, aspectRatioInfo, project.Story))
}

func buildKeyframesPrompt(scene map[string]any, imageCount int, aspectRatio string, characterBible any, globalStyle map[string]any, renderRules map[string]any) string {
	title := ""
	narration := ""
	storyFunction := ""
	characters := []string{}
	objects := []string{}
	environment := ""
	visual := ""

	if t, ok := scene["title"].(string); ok {
		title = t
	}
	if n, ok := scene["narration"].(string); ok {
		narration = n
	}
	if sf, ok := scene["story_function"].(string); ok {
		storyFunction = sf
	}
	if chars, ok := scene["characters"].([]any); ok {
		for _, c := range chars {
			if str, ok := c.(string); ok {
				characters = append(characters, str)
			}
		}
	}
	if objs, ok := scene["objects"].([]any); ok {
		for _, o := range objs {
			if str, ok := o.(string); ok {
				objects = append(objects, str)
			}
		}
	}
	if env, ok := scene["environment"].(map[string]any); ok {
		location := ""
		timeOfDay := ""
		weather := ""
		atmosphere := ""
		if l, ok := env["location"].(string); ok {
			location = l
		}
		if t, ok := env["time_of_day"].(string); ok {
			timeOfDay = t
		}
		if w, ok := env["weather"].(string); ok {
			weather = w
		}
		if a, ok := env["atmosphere"].(string); ok {
			atmosphere = a
		}
		environment = fmt.Sprintf("%s, %s, %s, %s", location, timeOfDay, weather, atmosphere)
	}
	if vis, ok := scene["visual"].(map[string]any); ok {
		shotType := ""
		cameraMotion := ""
		composition := ""
		action := ""
		if st, ok := vis["shot_type"].(string); ok {
			shotType = st
		}
		if cm, ok := vis["camera_motion"].(string); ok {
			cameraMotion = cm
		}
		if c, ok := vis["composition"].(string); ok {
			composition = c
		}
		if a, ok := vis["action"].(string); ok {
			action = a
		}
		visual = fmt.Sprintf("%s, %s, %s, %s", shotType, cameraMotion, composition, action)
	}

	relevantCharacterIDs := make([]string, 0, len(characters))
	relevantCharacterIDs = append(relevantCharacterIDs, characters...)

	// 构建角色圣经描述
	characterBibleStr := buildCharacterConsistencyPrompt(characterBible, relevantCharacterIDs)

	// 构建全局风格描述
	globalStyleStr := compactJSONObject(globalStyle)

	// 构建渲染规则描述
	renderRulesStr := compactJSONObject(renderRules)

	return strings.TrimSpace(fmt.Sprintf(`
为以下场景生成 %d 个关键帧，每个关键帧都应该有独立的视觉描述和提示词。

画面比例约束（强约束）：
- 画面比例：%s
- 所有关键帧的视觉描述和提示词必须符合此画面比例的构图要求。
- 构图、镜头角度、主体位置都必须适配 %s 的画面比例。

全局风格：
%s

渲染规则：
%s

角色圣经：
%s

	场景信息：
	- 场景标题：%s
	- 故事功能：%s
	- 旁白：%s
	- 场景涉及角色ID：%s
- 物体：%s
- 环境：%s
- 视觉描述：%s

请返回严格的 JSON 格式，只包含 keyframes 数组，每个 keyframe 必须包含：
- frame_id：唯一标识符，格式为 "scene_id_f01"
- sequence：序列编号，从 1 开始
- characters：数组，列出该关键帧涉及的角色ID（如 ["c01", "c02"]）
- prompt：对象，包含 subject_prompt、scene_prompt、full_prompt
- visual：对象，包含 shot_type、camera_motion、composition、action

示例输出格式：
{
  "keyframes": [
    {
      "frame_id": "s01_f01",
      "sequence": 1,
      "characters": ["c01"],
      "prompt": {
        "subject_prompt": "主角与关键物体的画面描述",
        "scene_prompt": "场景环境、镜头和氛围描述",
        "full_prompt": ""
      },
      "visual": {
        "shot_type": "全景",
        "camera_motion": "缓慢推进",
        "composition": "主角位于画面中央",
        "action": "小云朵轻轻漂浮，望向小星星"
      }
    }
  ]
}

	请确保：
1. 每个关键帧都有独特的视觉描述
2. 关键帧之间的动作有连贯性
3. 所有关键帧都符合场景的整体氛围
4. 所有关键帧的构图必须符合画面比例 %s
5. 每个关键帧必须包含 characters 字段，标明该帧涉及的角色ID
6. 每个关键帧的 characters 只能填写该帧实际出现的角色，不能漏填，也不能填入未出现的角色
7. prompt.subject_prompt 必须显式写出该帧角色的稳定外貌锚点，例如年龄感、脸型/脸蛋、五官、眼神、发型或头部轮廓、体型、衣物穿着、配饰、材质、主色、辨识特征
8. 同一个角色在所有关键帧中必须保持同一张脸、同一年龄感、同一套标志性穿着/材质、同一配色和同一气质，不允许帧间漂移
9. 如果角色是非人类，也必须固定体表材质、发光方式、表情布局、轮廓比例和标志特征
10. 提示词必须符合全局风格和渲染规则
11. 只返回 JSON，不要返回其他内容
`, imageCount, aspectRatio, aspectRatio, globalStyleStr, renderRulesStr, characterBibleStr, title, storyFunction, narration, strings.Join(characters, ", "), strings.Join(objects, ", "), environment, visual, aspectRatio))
}

func buildKeyframeImagePrompt(storyboardRoot map[string]any, scene sceneFile, keyframe keyframe) string {
	subjectPrompt := ""
	scenePrompt := ""

	if keyframe.Prompt != nil {
		if sp, ok := keyframe.Prompt["subject_prompt"].(string); ok {
			subjectPrompt = sp
		}
		if sp, ok := keyframe.Prompt["scene_prompt"].(string); ok {
			scenePrompt = sp
		}
	}

	if subjectPrompt == "" {
		subjectPrompt = scene.Prompt["subject_prompt"].(string)
	}
	if scenePrompt == "" {
		scenePrompt = scene.Prompt["scene_prompt"].(string)
	}

	aspectRatio := resolveSceneAspectRatio(storyboardRoot)

	// 从 storyboard 根提取角色圣经、全局风格和渲染规则
	characterBible := storyboardRoot["character_bible"]
	globalStyle, _ := storyboardRoot["global_style"].(map[string]any)
	renderRules, _ := storyboardRoot["render_rules"].(map[string]any)

	characterBibleStr := buildFilteredCharacterBibleJSON(characterBible, effectiveKeyframeCharacters(scene, keyframe))
	globalStyleStr := compactJSONObject(globalStyle)
	renderRulesStr := compactJSONObject(renderRules)

	return strings.TrimSpace(fmt.Sprintf(`
为儿童故事视频生成场景关键帧图片。

画面比例约束（强约束）：
- 画面比例：%s
- 生成的图片必须符合此画面比例的构图要求。
- 构图、镜头角度、主体位置都必须适配 %s 的画面比例。

全局风格：
%s

渲染规则：
%s

角色圣经（这是 storyboard 中原始角色信息，已过滤为当前关键帧涉及角色，必须原样参考，不要改写设定）：
%s

场景信息：
- 场景标题：%s
- 故事功能：%s
- 旁白：%s
- 当前关键帧允许出现的角色ID：%s
- 物体：%s
- 环境：%s
- 光影效果：%s

关键帧信息：
- 关键帧序号：%d
- 视觉描述：%s

请生成符合以下要求的图片：
1. 风格适合儿童故事，色彩明亮，画面温馨
2. 构图清晰，主体突出
3. 符合场景的环境和氛围
4. 展现关键帧的具体动作或细节
5. 图片构图必须符合画面比例 %s 的要求
6. 角色外貌必须严格按照角色圣经中的描述绘制，不能自行发挥
7. 画面必须符合全局风格和渲染规则
8. 画面中只能出现"当前关键帧允许出现的角色ID"中列出的角色，绝对不能出现任何未提及的角色
9. 严禁出现无关的人脸、无关的人物、无关的角色，只绘制场景涉及的角色
10. 同一个角色在所有关键帧中必须保持同一张脸、同一年龄感、同一服装/材质、同一主色和同一辨识特征
11. 提示词中的角色描述必须足够具体，必须锁定外貌长相、脸部特征、气质、体态、衣物穿着、配饰、年龄感

提示词：
- 主体提示：%s
- 场景提示：%s
`, aspectRatio, aspectRatio, globalStyleStr, renderRulesStr, characterBibleStr, scene.Title, scene.StoryFunction, scene.Narration, joinCharacterIDs(effectiveKeyframeCharacters(scene, keyframe)), strings.Join(scene.Objects, ", "), fmt.Sprintf("%s, %s, %s, %s", scene.Environment["location"], scene.Environment["time_of_day"], scene.Environment["weather"], scene.Environment["atmosphere"]), fmt.Sprintf("光照-%s, 动态-%s, 后处理-%s", scene.Effects["lighting"], scene.Effects["motion"], scene.Effects["post_process"]), keyframe.Sequence, fmt.Sprintf("%s, %s, %s, %s", keyframe.Visual["shot_type"], keyframe.Visual["camera_motion"], keyframe.Visual["composition"], keyframe.Visual["action"]), aspectRatio, subjectPrompt, scenePrompt))
}

func buildStoryboardPrompt(project projectFile) string {
	var durationInfo string
	if project.TargetDurationSec > 0 {
		durationInfo = fmt.Sprintf("- 目标视频总时长：%d 秒\n", project.TargetDurationSec)
	}
	var imageSwitchInfo string
	if project.ImageSwitchIntervalSec > 0 {
		imageSwitchInfo = fmt.Sprintf("- 图片切换间隔：%d 秒\n", project.ImageSwitchIntervalSec)
	}
	return strings.TrimSpace(fmt.Sprintf(`
请将下面的儿童故事转换为严格 JSON 的 storyboard.json。

必须满足：
1. 只返回 JSON，不要返回 markdown，不要解释。
2. 顶层必须包含：meta, project, global_style, character_bible, audio_profile, video_profile, render_rules, scenes。
3. scenes 必须是数组，每个 scene 必须包含：scene_id, sequence, title, story_function, narration, subtitle, duration_hint_sec, characters, objects, environment, visual, prompt, audio, effects。
4. environment 必须是 object，不能是 string。visual 必须是 object，不能是 string。effects 必须是 object，不能是 string。
5. prompt 必须是 object，并且必须包含：subject_prompt, scene_prompt, full_prompt。subject_prompt 和 scene_prompt 必须为非空字符串；full_prompt 先返回空字符串。
6. audio 必须是 object。
7. 内容适合儿童故事视频，语气温和，结构清晰。
8. JSON 的第一个字符必须是 {，最后一个字符必须是 }。

scene 的最小合法结构示例：
{
  "scene_id": "s01",
  "sequence": 1,
  "title": "场景标题",
  "story_function": "这一幕承担的叙事作用",
  "narration": "旁白全文",
  "subtitle": "字幕文本",
  "duration_hint_sec": 8,
  "characters": ["c01"],
  "objects": ["星星瓶"],
  "environment": {
    "location": "夜空",
    "time_of_day": "夜晚",
    "weather": "晴朗",
    "atmosphere": "梦幻温馨"
  },
  "visual": {
    "shot_type": "全景",
    "camera_motion": "缓慢推进",
    "composition": "主角位于画面中央",
    "action": "小云朵轻轻漂浮，望向小星星"
  },
  "prompt": {
    "subject_prompt": "主角与关键物体的画面描述",
    "scene_prompt": "场景环境、镜头和氛围描述",
    "full_prompt": ""
  },
  "audio": {
    "bgm": "背景音乐描述",
    "voice": "人声描述",
    "sound_effect": "音效描述"
  },
  "effects": {
    "motion": "元素运动效果",
    "lighting": "光效描述",
    "post_process": "后期风格描述"
  }
}

注意：
- 不要把 environment、visual、effects 写成一句话字符串。
- 不要遗漏 prompt.subject_prompt 或 prompt.scene_prompt。
- character_bible、audio_profile、video_profile、render_rules 也要保持 object/array 结构，不要输出自然语言段落。

项目信息：
- project_id: %s
- title: %s
%s%s
原始故事：
%s
`, project.ProjectID, project.Title, durationInfo, imageSwitchInfo, project.Story))
}

func decodeJSONBody(r io.Reader, target any) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return io.EOF
	}
	return json.Unmarshal(body, target)
}

func extractJSONObject(raw string) ([]byte, error) {
	candidate := strings.TrimSpace(raw)
	if strings.HasPrefix(candidate, "```") {
		lines := strings.Split(candidate, "\n")
		if len(lines) >= 3 {
			candidate = strings.Join(lines[1:len(lines)-1], "\n")
			candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "json")
			candidate = strings.TrimSpace(candidate)
		}
	}
	var (
		start      = -1
		depth      = 0
		inString   = false
		escapeNext = false
		lastErr    error
	)
	for i := 0; i < len(candidate); i++ {
		char := candidate[i]
		if inString {
			if escapeNext {
				escapeNext = false
				continue
			}
			if char == '\\' {
				escapeNext = true
				continue
			}
			if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				if normalized, err := normalizeJSON([]byte(candidate[start : i+1])); err == nil {
					return normalized, nil
				} else {
					lastErr = err
				}
				start = -1
			}
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("storyboard JSON object not found in zero-token output: %w", lastErr)
	}
	return nil, errors.New("storyboard JSON object not found in zero-token output")
}

func normalizeJSON(raw []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return json.MarshalIndent(value, "", "  ")
}

func readJSONFile(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func writeJSONFile(path string, value any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func writeRawJSONFile(path string, raw []byte) error {
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{
		"error": err.Error(),
	})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func loadProjectEnv(rootDir string) (map[string]string, error) {
	envPath := filepath.Join(rootDir, ".env")
	content, err := os.ReadFile(envPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return map[string]string{}, err
	}
	values := parseEnvContent(string(content))
	for key, value := range values {
		if os.Getenv(key) == "" {
			if err := os.Setenv(key, value); err != nil {
				return values, err
			}
		}
	}
	return values, nil
}

func parseEnvContent(content string) map[string]string {
	values := map[string]string{}
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		index := strings.Index(line, "=")
		if index <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:index])
		if !isEnvKey(key) {
			continue
		}
		values[key] = parseEnvValue(line[index+1:])
	}
	return values
}

func isEnvKey(key string) bool {
	for index, r := range key {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return key != ""
}

func parseEnvValue(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) >= 2 && value[0] == '"' {
		end := strings.LastIndex(value[1:], `"`)
		if end >= 0 {
			quoted := value[:end+2]
			if unquoted, err := strconv.Unquote(quoted); err == nil {
				return unquoted
			}
		}
	}
	if len(value) >= 2 && value[0] == '\'' {
		end := strings.LastIndex(value[1:], `'`)
		if end >= 0 {
			return value[1 : end+1]
		}
	}
	for index, r := range value {
		if r == '#' && (index == 0 || value[index-1] == ' ' || value[index-1] == '\t') {
			return strings.TrimSpace(value[:index])
		}
	}
	return value
}

func envSource(key string, value string, projectEnv map[string]string) string {
	if configured, ok := projectEnv[key]; ok && configured == value {
		return ".env:" + key
	}
	return "process:" + key
}

func codexCLIHealth(projectEnv map[string]string) map[string]any {
	if value := strings.TrimSpace(os.Getenv("ZERO_TOKEN_CODEX_PATH")); value != "" {
		return map[string]any{
			"path":   value,
			"source": envSource("ZERO_TOKEN_CODEX_PATH", value, projectEnv),
			"ok":     commandRefExists(value),
		}
	}
	if value := strings.TrimSpace(os.Getenv("ZERO_TOKEN_CODEX_BIN")); value != "" {
		return map[string]any{
			"path":   value,
			"source": envSource("ZERO_TOKEN_CODEX_BIN", value, projectEnv),
			"ok":     commandRefExists(value),
		}
	}
	path := lookupCommand("codex")
	return map[string]any{
		"path":   path,
		"source": "PATH",
		"ok":     path != "",
	}
}

func commandRefExists(value string) bool {
	if strings.Contains(value, string(os.PathSeparator)) {
		return fileExists(value)
	}
	return lookupCommand(value) != ""
}

func defaultProviderRef(providerRef string) string {
	return defaultCodexImageProviderRef
}

func buildHomeHTML(projectsDir string, zeroTokenBuilt bool) string {
	return strings.NewReplacer(
		"__PROJECTS_DIR__", html.EscapeString(projectsDir),
		"__ZERO_TOKEN_BUILT__", strconv.FormatBool(zeroTokenBuilt),
		"__DEFAULT_IMAGE_PROVIDER__", defaultCodexImageProviderRef,
		"__DEFAULT_AUDIO_PROVIDER__", defaultKokoroProviderRef,
		"__DEFAULT_VOICE__", defaultKokoroVoiceName,
		"__DEFAULT_SPEAKING_RATE__", defaultChildSpeakingRate,
	).Replace(storyVideoHomeHTML)
}

const storyVideoHomeHTML = `<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Story Video · Codex Pipeline</title>
    <style>
      :root {
        color-scheme: light dark;
        font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
        background: #f5f7fb;
        color: #172033;
      }
      html {
        width: 100%;
        overflow-x: hidden;
      }
      body {
        margin: 0;
        background: #f5f7fb;
        color: #172033;
        overflow-x: hidden;
      }
      main {
        box-sizing: border-box;
        width: 100%;
        max-width: none;
        margin: 0;
        padding: 20px;
        transition: padding-right 180ms ease, max-width 180ms ease;
      }
      body.progress-open main {
        padding-right: 404px;
      }
      body.progress-collapsed main {
        max-width: none;
      }
      header {
        display: grid;
        grid-template-columns: minmax(0, 1fr) auto;
        gap: 16px;
        align-items: end;
        padding: 12px 0 18px;
        border-bottom: 1px solid #d9e1ee;
      }
      h1, h2, h3 {
        margin: 0;
        letter-spacing: 0;
      }
      h1 {
        font-size: 26px;
        line-height: 1.15;
      }
      h2 {
        font-size: 17px;
      }
      h3 {
        font-size: 15px;
      }
      p {
        margin: 8px 0 0;
        color: #536176;
      }
      .badges, .toolbar, .form-grid, .stage-grid, .project-grid, .scene-grid {
        display: grid;
        gap: 12px;
      }
      .badges {
        grid-template-columns: repeat(3, max-content);
        justify-content: end;
      }
      .badge {
        border: 1px solid #cbd6e7;
        border-radius: 999px;
        padding: 7px 10px;
        background: #ffffff;
        color: #314158;
        font-size: 12px;
        white-space: nowrap;
      }
      .layout {
        display: grid;
        grid-template-columns: 390px minmax(0, 1fr);
        gap: 18px;
        margin-top: 18px;
        align-items: start;
      }
      section, .surface {
        min-width: 0;
        background: #ffffff;
        border: 1px solid #d9e1ee;
        border-radius: 8px;
        padding: 16px;
      }
      label {
        display: block;
        font-size: 12px;
        color: #536176;
        margin: 12px 0 6px;
      }
      .field-hint {
        margin-top: 5px;
        color: #64748b;
        font-size: 11px;
        line-height: 1.35;
      }
      input, textarea, select, button {
        width: 100%;
        box-sizing: border-box;
        border: 1px solid #c6d1e1;
        border-radius: 7px;
        background: #ffffff;
        color: #172033;
        font: inherit;
        padding: 10px 11px;
      }
      textarea {
        min-height: 106px;
        resize: vertical;
      }
      button {
        cursor: pointer;
        font-weight: 650;
        background: #1f6feb;
        color: #ffffff;
        border-color: #1f6feb;
      }
      button.secondary {
        background: #ffffff;
        color: #24344d;
        border-color: #c6d1e1;
      }
      button.small {
        width: auto;
        min-width: 64px;
        padding: 7px 10px;
        font-size: 12px;
      }
      button:disabled {
        cursor: not-allowed;
        opacity: 0.62;
      }
      .toolbar {
        grid-template-columns: repeat(2, minmax(0, 1fr));
        margin-top: 14px;
      }
      .form-grid {
        grid-template-columns: repeat(2, minmax(0, 1fr));
      }
      .full {
        grid-column: 1 / -1;
      }
      .status-line {
        min-height: 22px;
        margin-top: 12px;
        color: #536176;
        font-size: 13px;
      }
      .pipeline {
        display: grid;
        gap: 14px;
        min-width: 0;
      }
      .stage-grid {
        grid-template-columns: repeat(auto-fit, minmax(96px, 1fr));
      }
      .stage {
        min-height: 68px;
        border: 1px solid #d9e1ee;
        border-radius: 8px;
        padding: 10px;
        background: #fbfcff;
      }
      .stage strong {
        display: block;
        font-size: 13px;
      }
      .stage span {
        display: inline-block;
        margin-top: 10px;
        border-radius: 999px;
        padding: 4px 8px;
        font-size: 12px;
        background: #eef2f8;
        color: #475569;
      }
      .stage.success span {
        background: #dff7ea;
        color: #116337;
      }
      .stage.running span {
        background: #fff2cc;
        color: #875800;
      }
      .stage.failed span {
        background: #ffe2e0;
        color: #a32118;
      }
      .stage.skipped span {
        background: #eef2f8;
        color: #475569;
      }
      .progress-panel {
        position: fixed;
        top: 16px;
        right: 16px;
        width: 360px;
        height: calc(100vh - 32px);
        z-index: 30;
        box-sizing: border-box;
        display: grid;
        grid-template-rows: auto auto minmax(0, 1fr);
        gap: 14px;
        overflow: hidden;
        box-shadow: 0 18px 42px rgba(39, 52, 78, 0.16);
        transition: transform 180ms ease, opacity 180ms ease;
      }
      body.progress-collapsed .progress-panel {
        pointer-events: none;
        opacity: 0;
        transform: translateX(calc(100% + 24px));
      }
      .progress-head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 10px;
      }
      .progress-title-row {
        display: flex;
        align-items: center;
        gap: 8px;
        min-width: 0;
      }
      .icon-button {
        width: 32px;
        height: 32px;
        display: inline-grid;
        place-items: center;
        padding: 0;
        border-radius: 7px;
        border-color: #c6d1e1;
        background: #ffffff;
        color: #24344d;
        line-height: 1;
      }
      .progress-rail {
        position: fixed;
        right: 0;
        top: 142px;
        z-index: 31;
        width: 36px;
        height: 116px;
        border-radius: 8px 0 0 8px;
        border: 1px solid #c6d1e1;
        border-right: 0;
        background: #ffffff;
        color: #24344d;
        box-shadow: 0 12px 28px rgba(39, 52, 78, 0.14);
        writing-mode: vertical-rl;
        letter-spacing: 0;
        padding: 8px 6px;
      }
      body.progress-open .progress-rail {
        display: none;
      }
      .progress-pill {
        border-radius: 999px;
        background: #eef2f8;
        color: #475569;
        padding: 4px 8px;
        font-size: 12px;
        white-space: nowrap;
      }
      .phase-list {
        display: grid;
        gap: 8px;
      }
      .phase-row {
        display: grid;
        grid-template-columns: 18px minmax(0, 1fr);
        gap: 9px;
        align-items: start;
        padding: 7px 0;
        border-bottom: 1px solid #eef2f8;
      }
      .phase-row:last-child {
        border-bottom: 0;
      }
      .phase-dot {
        width: 10px;
        height: 10px;
        border-radius: 50%;
        margin-top: 5px;
        background: #cbd5e1;
      }
      .phase-row.running .phase-dot {
        background: #d97706;
        box-shadow: 0 0 0 4px rgba(217, 119, 6, 0.12);
      }
      .phase-row.done .phase-dot,
      .phase-row.skipped .phase-dot {
        background: #16a34a;
      }
      .phase-row.error .phase-dot,
      .phase-row.failed .phase-dot {
        background: #dc2626;
      }
      .phase-label {
        font-size: 13px;
        font-weight: 650;
      }
      .phase-msg {
        margin-top: 2px;
        color: #64748b;
        font-size: 12px;
        line-height: 1.4;
        overflow-wrap: anywhere;
      }
      .event-log {
        min-height: 220px;
        overflow: auto;
        border-top: 1px solid #eef2f8;
        padding-top: 8px;
      }
      .event-item {
        padding: 8px 0;
        border-bottom: 1px solid #eef2f8;
      }
      .event-meta {
        display: flex;
        justify-content: space-between;
        gap: 8px;
        color: #64748b;
        font-size: 11px;
      }
      .event-message {
        margin-top: 3px;
        font-size: 12px;
        line-height: 1.45;
        overflow-wrap: anywhere;
      }
      .project-grid {
        grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      }
      .project-list {
        display: grid;
        gap: 8px;
        max-height: 360px;
        overflow: auto;
        padding-right: 2px;
      }
      .project-row {
        display: grid;
        grid-template-columns: minmax(180px, 1.2fr) minmax(180px, 1fr) 132px;
        gap: 12px;
        align-items: center;
        border: 1px solid #d9e1ee;
        border-radius: 8px;
        background: #fbfcff;
        padding: 10px;
      }
      .project-title {
        font-weight: 650;
        overflow-wrap: anywhere;
      }
      .project-meta {
        margin-top: 4px;
        color: #64748b;
        font-size: 11px;
        overflow-wrap: anywhere;
      }
      .project-progress {
        display: grid;
        gap: 6px;
      }
      .project-progress-head {
        display: flex;
        justify-content: space-between;
        gap: 8px;
        color: #475569;
        font-size: 12px;
      }
      .project-progress-track {
        height: 7px;
        border-radius: 999px;
        overflow: hidden;
        background: #e6edf7;
      }
      .project-progress-bar {
        height: 100%;
        border-radius: 999px;
        background: #1f6feb;
      }
      .project-actions {
        display: flex;
        gap: 8px;
        justify-content: flex-end;
      }
      .metric {
        border: 1px solid #d9e1ee;
        border-radius: 8px;
        background: #fbfcff;
        padding: 11px;
      }
      .metric .k {
        color: #64748b;
        font-size: 12px;
      }
      .metric .v {
        margin-top: 5px;
        font-size: 14px;
        overflow-wrap: anywhere;
      }
      .story-plan {
        display: grid;
        gap: 10px;
      }
      .cycle-row {
        border-left: 3px solid #1f6feb;
        padding: 8px 10px;
        background: #f7faff;
      }
      .scene-outline {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
        gap: 8px;
      }
      .scene-outline-item {
        border: 1px solid #d9e1ee;
        border-radius: 7px;
        padding: 9px;
        background: #fbfcff;
      }
      .scene-outline-item strong {
        display: block;
        font-size: 12px;
      }
      .scene-outline-item span {
        display: block;
        margin-top: 3px;
        color: #64748b;
        font-size: 11px;
        line-height: 1.35;
      }
      .scene-grid {
        grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
      }
      .scene {
        border: 1px solid #d9e1ee;
        border-radius: 8px;
        background: #ffffff;
        overflow: hidden;
      }
      .scene-body {
        padding: 12px;
      }
      .scene img, .scene video, .scene iframe {
        width: 100%;
        aspect-ratio: 16 / 9;
        object-fit: cover;
        display: block;
        border: 0;
        background: #e8edf5;
      }
      .scene iframe {
        min-height: 180px;
      }
      .scene-placeholder {
        width: 100%;
        aspect-ratio: 16 / 9;
        display: grid;
        place-items: center;
        background: repeating-linear-gradient(135deg, #f7faff 0, #f7faff 10px, #eef4fb 10px, #eef4fb 20px);
        color: #64748b;
        font-size: 13px;
      }
      .scene-status-row {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin-top: 10px;
      }
      .scene-chip {
        border-radius: 999px;
        padding: 4px 8px;
        background: #eef2f8;
        color: #475569;
        font-size: 11px;
      }
      .scene-chip.success,
      .scene-chip.done,
      .scene-chip.preview_ready {
        background: #dff7ea;
        color: #116337;
      }
      .scene-chip.running {
        background: #fff2cc;
        color: #875800;
      }
      .scene-chip.failed,
      .scene-chip.error {
        background: #ffe2e0;
        color: #a32118;
      }
      .keyframe-strip {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(52px, 1fr));
        gap: 6px;
        margin-top: 10px;
      }
      .keyframe-strip img {
        aspect-ratio: 16 / 9;
        border-radius: 6px;
        border: 1px solid #d9e1ee;
        object-fit: cover;
      }
      .muted {
        color: #64748b;
        font-size: 12px;
      }
      pre {
        max-height: 260px;
        overflow: auto;
        margin: 0;
        padding: 12px;
        border-radius: 8px;
        background: #0f172a;
        color: #e2e8f0;
        font-size: 12px;
      }
      a {
        color: #1f6feb;
        text-decoration: none;
      }
      a:hover {
        text-decoration: underline;
      }
      .empty {
        border: 1px dashed #c6d1e1;
        border-radius: 8px;
        padding: 14px;
        color: #64748b;
        background: #fbfcff;
      }
      @media (max-width: 980px) {
        header, .layout {
          grid-template-columns: 1fr;
        }
        .badges, .stage-grid, .project-grid {
          grid-template-columns: repeat(2, minmax(0, 1fr));
          justify-content: stretch;
        }
        .project-row {
          grid-template-columns: 1fr;
        }
        .project-actions {
          justify-content: stretch;
        }
        .project-actions button {
          flex: 1;
        }
      }
      @media (max-width: 1180px) {
        body.progress-open main {
          padding-right: 20px;
        }
        .progress-panel {
          width: min(360px, calc(100vw - 32px));
        }
      }
      @media (max-width: 640px) {
        main {
          padding: 14px;
        }
        body.progress-open main {
          padding-right: 14px;
        }
        .form-grid, .toolbar, .badges, .stage-grid, .project-grid {
          grid-template-columns: 1fr;
        }
      }
    </style>
  </head>
  <body class="progress-collapsed">
    <main>
      <header>
        <div>
          <h1>Story Video</h1>
          <p>主题输入后自动规划 3/5 周期故事线，并产出带图、带语音、带字幕的绘本故事视频。</p>
        </div>
        <div class="badges">
          <div class="badge">故事：Codex</div>
          <div class="badge">出图：__DEFAULT_IMAGE_PROVIDER__</div>
          <div class="badge">语音：__DEFAULT_VOICE__</div>
        </div>
      </header>

      <div class="layout">
        <section>
          <h2>主题</h2>
          <label for="theme">故事主题</label>
          <textarea id="theme">一个胆小的小月亮学会照亮森林</textarea>

          <div class="form-grid">
            <div>
              <label for="audience">受众</label>
              <input id="audience" value="3-6岁儿童" />
            </div>
            <div>
              <label for="tone">语气</label>
              <input id="tone" value="温暖、清晰、适合睡前绘本" />
            </div>
            <div>
              <label for="duration">目标时长（秒）</label>
              <input id="duration" type="number" min="20" value="60" />
              <div class="field-hint">控制整条故事视频的大致长度。</div>
            </div>
            <div>
              <label for="cycleMode">故事周期</label>
              <select id="cycleMode">
                <option value="auto" selected>自动 3/5</option>
                <option value="3">固定 3 周期</option>
                <option value="5">固定 5 周期</option>
              </select>
            </div>
            <div>
              <label for="aspectRatio">画幅</label>
              <select id="aspectRatio">
                <option value="16:9" selected>16:9 横版</option>
                <option value="9:16">9:16 竖版</option>
                <option value="1:1">1:1 方形</option>
                <option value="4:3">4:3 绘本</option>
              </select>
            </div>
            <div>
              <label for="imageSwitch">图片切换间隔（秒）</label>
              <input id="imageSwitch" type="number" min="1" value="20" />
              <div class="field-hint">每隔 N 秒换一张绘本图；单页较长时会生成多张关键帧。</div>
            </div>
          </div>

          <button id="generateFromThemeBtn" type="button" style="margin-top: 14px;">生成绘本视频</button>
          <div class="toolbar">
            <button id="healthBtn" class="secondary" type="button">健康检查</button>
            <button id="listBtn" class="secondary" type="button">项目列表</button>
          </div>

          <label for="projectId">已有项目 ID</label>
          <input id="projectId" placeholder="pv_..." />
          <div class="toolbar">
            <button id="loadProjectBtn" class="secondary" type="button">读取项目</button>
            <button id="continuePipelineBtn" class="secondary" type="button">继续流水线</button>
          </div>

          <div id="statusText" class="status-line">等待主题输入</div>
        </section>

        <div class="pipeline">
          <section>
            <h2>总进度</h2>
            <div id="stageGrid" class="stage-grid"></div>
          </section>

          <section>
            <h2>故事结构</h2>
            <div id="storyPlan" class="story-plan"></div>
          </section>

          <section>
            <h2>绘本页生产看板</h2>
            <div id="sceneGrid" class="scene-grid"></div>
          </section>

          <section>
            <h2>输出</h2>
            <div id="finalOutput" class="empty">生成完成后显示最终视频入口。</div>
          </section>

          <section>
            <h2>项目概览</h2>
            <div id="projectGrid" class="project-grid"></div>
          </section>

          <section>
            <h2>历史项目</h2>
            <div id="projectList" class="project-list"></div>
          </section>

          <section>
            <h2>接口结果</h2>
            <pre id="output">{ "ok": true }</pre>
          </section>
        </div>

      </div>
    </main>

    <button id="progressRailBtn" class="progress-rail" type="button" aria-label="展开生成过程" title="展开生成过程">生成过程</button>
    <aside id="progressDrawer" class="progress-panel surface" aria-label="生成过程">
      <div class="progress-head">
        <div class="progress-title-row">
          <button id="progressCloseBtn" class="icon-button" type="button" aria-label="收起生成过程" title="收起生成过程">›</button>
          <h2>生成过程</h2>
        </div>
        <span id="progressState" class="progress-pill">等待</span>
      </div>
      <div id="phaseTimeline" class="phase-list"></div>
      <div>
        <h3>Codex / 流水线输出</h3>
        <div id="eventLog" class="event-log"></div>
      </div>
    </aside>

    <script>
      const defaultImageProvider = "__DEFAULT_IMAGE_PROVIDER__";
      const defaultAudioProvider = "__DEFAULT_AUDIO_PROVIDER__";
      const defaultVoice = "__DEFAULT_VOICE__";
      const output = document.getElementById("output");
      const statusText = document.getElementById("statusText");
      const projectIdInput = document.getElementById("projectId");
      const stageGrid = document.getElementById("stageGrid");
      const projectGrid = document.getElementById("projectGrid");
      const projectList = document.getElementById("projectList");
      const storyPlan = document.getElementById("storyPlan");
      const sceneGrid = document.getElementById("sceneGrid");
      const finalOutput = document.getElementById("finalOutput");
      const progressState = document.getElementById("progressState");
      const phaseTimeline = document.getElementById("phaseTimeline");
      const eventLog = document.getElementById("eventLog");
      const progressCloseBtn = document.getElementById("progressCloseBtn");
      const progressRailBtn = document.getElementById("progressRailBtn");

      const stageDefs = [
        ["project", "项目"],
        ["story", "故事规划"],
        ["storyboard", "分镜"],
        ["keyframes", "配图规划"],
        ["image", "Codex 出图"],
        ["audio", "语音"],
        ["subtitle", "字幕"],
        ["scene_video", "片段"],
        ["final", "成片"]
      ];
      const phaseLabels = Object.fromEntries(stageDefs);
      let eventHistory = [];
      let eventLastSeq = 0;
      let eventPollTimer = null;
      let detailPollTimer = null;
      let detailFetchInFlight = false;
      let activeProjectId = "";
      let currentDetail = null;

      function escapeHtml(value) {
        return String(value == null ? "" : value)
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")
          .replace(/>/g, "&gt;")
          .replace(/"/g, "&quot;")
          .replace(/'/g, "&#39;");
      }

      function setStatus(message) {
        statusText.textContent = message;
      }

      function setOutput(value) {
        output.textContent = JSON.stringify(value, null, 2);
      }

      function setProgressDrawer(open) {
        document.body.classList.toggle("progress-open", open);
        document.body.classList.toggle("progress-collapsed", !open);
        if (progressCloseBtn) progressCloseBtn.setAttribute("aria-expanded", String(open));
        if (progressRailBtn) progressRailBtn.setAttribute("aria-expanded", String(open));
        try {
          localStorage.setItem("storyVideoProgressDrawerOpen", open ? "1" : "0");
        } catch {}
      }

      function normalizeEventStatus(status) {
        if (status === "failed") return "error";
        if (status === "success") return "done";
        return status || "pending";
      }

      function formatEventTime(value) {
        if (!value) return "";
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) return "";
        return date.toLocaleTimeString("zh-CN", { hour12: false });
      }

      function resetProgress() {
        eventHistory = [];
        eventLastSeq = 0;
        renderProgressPanel();
      }

      function stopEventPolling() {
        if (eventPollTimer) {
          clearInterval(eventPollTimer);
          eventPollTimer = null;
        }
        if (detailPollTimer) {
          clearInterval(detailPollTimer);
          detailPollTimer = null;
        }
      }

      function mergeEvents(events) {
        if (!Array.isArray(events) || !events.length) return;
        const seen = new Set(eventHistory.map(function(event) { return event.seq; }));
        for (const event of events) {
          if (!event || seen.has(event.seq)) continue;
          eventHistory.push(event);
          seen.add(event.seq);
          if (event.seq > eventLastSeq) eventLastSeq = event.seq;
        }
        if (eventHistory.length > 240) {
          eventHistory = eventHistory.slice(eventHistory.length - 240);
        }
      }

      function phaseStatesFromEvents() {
        const states = {};
        for (const def of stageDefs) {
          states[def[0]] = { status: "pending", message: "" };
        }
        for (const event of eventHistory) {
          const phase = event.phase || "pipeline";
          if (!states[phase]) continue;
          states[phase] = {
            status: normalizeEventStatus(event.status),
            message: event.message || ""
          };
        }
        return states;
      }

      function renderProgressPanel() {
        const states = phaseStatesFromEvents();
		const latest = eventHistory[eventHistory.length - 1];
		const hasError = eventHistory.some(function(event) { return normalizeEventStatus(event.status) === "error"; });
		const completed = eventHistory.some(function(event) {
		  return (event.phase === "pipeline" || event.phase === "final") && normalizeEventStatus(event.status) === "done";
		});
		const latestStatus = latest ? normalizeEventStatus(latest.status) : "pending";
		progressState.textContent = !latest ? "等待" : hasError ? "失败" : completed ? "完成" : latestStatus === "running" ? "进行中" : "就绪";
        phaseTimeline.innerHTML = stageDefs.map(function(def) {
          const phase = def[0];
          const state = states[phase] || { status: "pending", message: "" };
          const status = normalizeEventStatus(state.status);
          return "<div class=\"phase-row " + escapeHtml(status) + "\">" +
            "<span class=\"phase-dot\"></span>" +
            "<div><div class=\"phase-label\">" + escapeHtml(def[1]) + "</div>" +
            "<div class=\"phase-msg\">" + escapeHtml(state.message || status) + "</div></div>" +
          "</div>";
        }).join("");
        if (!eventHistory.length) {
          eventLog.innerHTML = "<div class=\"empty\">点击生成后，这里会显示 Codex 和流水线的实时过程。</div>";
          return;
        }
        eventLog.innerHTML = eventHistory.slice(-90).map(function(event) {
          const phaseLabel = phaseLabels[event.phase] || event.phase || "流水线";
          const status = normalizeEventStatus(event.status);
          return "<div class=\"event-item " + escapeHtml(status) + "\">" +
            "<div class=\"event-meta\"><span>" + escapeHtml(formatEventTime(event.created_at)) + " · " + escapeHtml(phaseLabel) + "</span><span>" + escapeHtml(status) + "</span></div>" +
            "<div class=\"event-message\">" + escapeHtml(event.message || "") + "</div>" +
          "</div>";
        }).join("");
        eventLog.scrollTop = eventLog.scrollHeight;
      }

      async function fetchProjectEvents(projectId, reset) {
        if (!projectId) return;
        if (reset) {
          eventHistory = [];
          eventLastSeq = 0;
        }
        const path = "/api/projects/" + encodeURIComponent(projectId) + "/events?after=" + encodeURIComponent(eventLastSeq) + "&limit=160";
        const res = await fetch(path);
        const data = await readJson(res);
        if (!res.ok) throw new Error(data.error || ("HTTP " + res.status));
        mergeEvents(data.events || []);
        renderProgressPanel();
        renderStages(currentDetail || {});
      }

      async function fetchProjectDetailQuiet(projectId) {
        if (!projectId || detailFetchInFlight) return null;
        detailFetchInFlight = true;
        try {
          const res = await fetch("/api/projects/" + encodeURIComponent(projectId));
          const data = await readJson(res);
          if (!res.ok) throw new Error(data.error || ("HTTP " + res.status));
          renderDetail(data);
          return data;
        } finally {
          detailFetchInFlight = false;
        }
      }

      function startEventPolling(projectId) {
        activeProjectId = projectId || "";
        stopEventPolling();
        resetProgress();
        if (!activeProjectId) return;
        fetchProjectEvents(activeProjectId, true).catch(function(error) {
          setStatus(error.message);
        });
        fetchProjectDetailQuiet(activeProjectId).catch(function(error) {
          setStatus(error.message);
        });
        eventPollTimer = setInterval(function() {
          fetchProjectEvents(activeProjectId, false).catch(function(error) {
            setStatus(error.message);
          });
        }, 1200);
        detailPollTimer = setInterval(function() {
          fetchProjectDetailQuiet(activeProjectId).catch(function(error) {
            setStatus(error.message);
          });
        }, 2200);
      }

      function themeTitle(theme) {
        const text = String(theme || "").trim();
        if (!text) return "绘本故事";
        return text.length > 24 ? text.slice(0, 24) : text;
      }

      function buildThemeStoryInputClient() {
        const theme = document.getElementById("theme").value.trim();
        const audience = document.getElementById("audience").value.trim() || "3-6岁儿童";
        const tone = document.getElementById("tone").value.trim() || "温暖、清晰、适合睡前绘本";
        const cycleMode = document.getElementById("cycleMode").value || "auto";
        return [
          "主题：" + theme,
          "受众：" + audience,
          "语气：" + tone,
          "故事周期模式：" + cycleMode,
          "",
          "请基于这个主题从零规划一个完整绘本故事。不要把主题当成已有故事全文；需要先判断适合 3 个还是 5 个周期，再生成对应的故事线、分镜、旁白、字幕、图片提示词和视频设定。"
        ].join("\n");
      }

      function buildCreateProjectPayload() {
        const theme = document.getElementById("theme").value.trim();
        return {
          title: themeTitle(theme),
          story: buildThemeStoryInputClient(),
          provider_ref: defaultImageProvider,
          target_duration_sec: parseInt(document.getElementById("duration").value, 10),
          image_switch_interval_sec: parseInt(document.getElementById("imageSwitch").value, 10),
          aspect_ratio: document.getElementById("aspectRatio").value
        };
      }

      function buildRunAllPayload() {
        return {
          provider_ref: defaultImageProvider,
          audio_provider_ref: defaultAudioProvider,
          voice_name: defaultVoice,
          speaking_rate: "__DEFAULT_SPEAKING_RATE__",
          pitch: "0Hz",
          timeout_ms: 1200000
        };
      }

      async function readJson(res) {
        const text = await res.text();
        try {
          return JSON.parse(text);
        } catch {
          return { raw: text, status: res.status };
        }
      }

      async function callApi(path, init) {
        const res = await fetch(path, init);
        const data = await readJson(res);
        setOutput(data);
        if (!res.ok) {
          const err = new Error(data.error || ("HTTP " + res.status));
          err.data = data;
          throw err;
        }
        return data;
      }

      function countStatus(items, key) {
        const list = items || [];
        if (!list.length) return "pending";
        if (list.some(function(item) { return item[key] === "failed"; })) return "failed";
        if (list.some(function(item) { return item[key] === "running"; })) return "running";
        if (list.every(function(item) { return item[key] === "success" || item[key] === "preview_ready"; })) return "success";
        return "pending";
      }

      function stageState(detail) {
        const project = detail && detail.project ? detail.project : {};
        const scenes = detail && detail.scenes ? detail.scenes : [];
        const storyboard = detail && detail.storyboard ? detail.storyboard : null;
        const hasScenes = scenes.length > 0;
        const imageStatus = countStatus(scenes, "image_status");
        const audioStatus = countStatus(scenes, "audio_status");
        const videoStatus = countStatus(scenes, "compose_status");
        const subtitleStatus = scenes.length && scenes.every(function(scene) {
          return !!scene.subtitle_preview_url && !!scene.subtitle_ass_preview_url;
        }) ? "success" : (audioStatus === "failed" ? "failed" : audioStatus === "running" ? "running" : "pending");
        const states = {
          project: project.project_id ? "success" : "pending",
          story: storyboard && storyboard.story_plan ? "success" : project.status === "storyboard_generating" ? "running" : "pending",
          storyboard: project.storyboard_valid ? "success" : project.status === "storyboard_generating" ? "running" : "pending",
          keyframes: scenes.some(function(scene) { return Array.isArray(scene.keyframes) && scene.keyframes.length > 0; }) ? "success" : (hasScenes ? "pending" : "pending"),
          image: imageStatus,
          audio: audioStatus,
          subtitle: subtitleStatus,
          scene_video: videoStatus,
          final: project.final_video_status === "success" || project.final_video_status === "preview_ready" ? "success" : project.final_video_status === "running" ? "running" : project.final_video_status === "failed" ? "failed" : hasScenes ? "pending" : "pending"
        };
        const eventStates = phaseStatesFromEvents();
        for (const key of Object.keys(eventStates)) {
          const status = normalizeEventStatus(eventStates[key].status);
          if (status && status !== "pending") states[key] = status;
        }
        return states;
      }

      function renderStages(detail) {
        const states = stageState(detail || {});
        stageGrid.innerHTML = stageDefs.map(function(stage) {
          const state = normalizeEventStatus(states[stage[0]] || "pending");
          const className = state === "done" ? "success" : state === "error" ? "failed" : state;
          return "<div class=\"stage " + escapeHtml(className) + "\"><strong>" + escapeHtml(stage[1]) + "</strong><span>" + escapeHtml(state) + "</span></div>";
        }).join("");
      }

      function metric(label, value) {
        return "<div class=\"metric\"><div class=\"k\">" + escapeHtml(label) + "</div><div class=\"v\">" + escapeHtml(value || "-") + "</div></div>";
      }

      function renderProject(project) {
        if (!project || !project.project_id) {
          projectGrid.innerHTML = "<div class=\"empty full\">暂无项目。</div>";
          return;
        }
        projectIdInput.value = project.project_id;
        projectGrid.innerHTML = [
          metric("项目 ID", project.project_id),
          metric("状态", project.status),
          metric("Provider", project.provider_ref || defaultImageProvider),
          metric("场景数", project.scene_count || 0),
          metric("画幅", project.aspect_ratio || "-"),
          metric("目标时长", project.target_duration_sec ? project.target_duration_sec + " 秒" : "-"),
          metric("成片状态", project.final_video_status || "pending"),
          metric("更新时间", project.updated_at || "-")
        ].join("");
      }

      function projectProgressInfo(project) {
        const status = project && project.status ? project.status : "created";
        const finalStatus = project && project.final_video_status ? project.final_video_status : "";
        if (finalStatus === "success" || finalStatus === "preview_ready") {
          return { label: "成片完成", percent: 100 };
        }
        if (finalStatus === "running" || status === "final_video_compositing") {
          return { label: "成片合成中", percent: 92 };
        }
        if (status.indexOf("video") >= 0) {
          return { label: "片段处理中", percent: 78 };
        }
        if (status.indexOf("audio") >= 0) {
          return { label: "语音/字幕中", percent: 64 };
        }
        if (status.indexOf("image") >= 0) {
          return { label: "出图中", percent: 50 };
        }
        if (project && project.storyboard_valid) {
          return { label: "已拆页", percent: 38 };
        }
        if (status.indexOf("storyboard") >= 0) {
          return { label: status.indexOf("failed") >= 0 ? "分镜失败" : "故事/分镜中", percent: status.indexOf("failed") >= 0 ? 18 : 24 };
        }
        return { label: "待规划", percent: 8 };
      }

      function formatProjectTime(value) {
        if (!value) return "-";
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) return value;
        return date.toLocaleString("zh-CN", { hour12: false });
      }

      function renderProjectList(projects) {
        if (!projectList) return;
        if (!projects || !projects.length) {
          projectList.innerHTML = "<div class=\"empty\">还没有历史项目。</div>";
          return;
        }
        projectList.innerHTML = projects.map(function(project) {
          const progress = projectProgressInfo(project);
          const title = project.title || project.project_id;
          const sceneInfo = project.scene_count ? project.scene_count + " 页" : "未拆页";
          const durationInfo = project.target_duration_sec ? project.target_duration_sec + " 秒" : "未设时长";
          const updated = project.updated_at || project.created_at || "";
          return "<div class=\"project-row\" data-project-id=\"" + escapeHtml(project.project_id) + "\">" +
            "<div>" +
              "<div class=\"project-title\">" + escapeHtml(title) + "</div>" +
              "<div class=\"project-meta\">" + escapeHtml(project.project_id) + " · " + escapeHtml(sceneInfo) + " · " + escapeHtml(durationInfo) + "</div>" +
              "<div class=\"project-meta\">更新：" + escapeHtml(formatProjectTime(updated)) + "</div>" +
            "</div>" +
            "<div class=\"project-progress\">" +
              "<div class=\"project-progress-head\"><span>" + escapeHtml(progress.label) + "</span><span>" + escapeHtml(progress.percent) + "%</span></div>" +
              "<div class=\"project-progress-track\"><div class=\"project-progress-bar\" style=\"width:" + escapeHtml(progress.percent) + "%\"></div></div>" +
              "<div class=\"project-meta\">" + escapeHtml(project.status || "created") + (project.final_video_status ? " · " + escapeHtml(project.final_video_status) : "") + "</div>" +
            "</div>" +
            "<div class=\"project-actions\">" +
              "<button class=\"secondary small\" type=\"button\" data-project-load=\"" + escapeHtml(project.project_id) + "\">查看</button>" +
              "<button class=\"small\" type=\"button\" data-project-continue=\"" + escapeHtml(project.project_id) + "\">继续</button>" +
            "</div>" +
          "</div>";
        }).join("");
      }

      async function loadProjectList(showOutput) {
        const res = await fetch("/api/projects");
        const data = await readJson(res);
        if (showOutput) setOutput(data);
        if (!res.ok) throw new Error(data.error || ("HTTP " + res.status));
        renderProjectList(data.projects || []);
        return data.projects || [];
      }

      function renderStoryPlan(storyboard, scenes) {
        if (!storyboard || !storyboard.story_plan) {
          storyPlan.innerHTML = "<div class=\"empty\">Codex 完成故事规划后，会先在这里显示 3/5 周期判断和绘本页拆分。</div>";
          return;
        }
        const plan = storyboard.story_plan;
        const cycles = Array.isArray(plan.cycles) ? plan.cycles : [];
        const storyboardScenes = Array.isArray(storyboard.scenes) ? storyboard.scenes : [];
        const visibleScenes = Array.isArray(scenes) && scenes.length ? scenes : storyboardScenes;
        const sceneCount = visibleScenes.length || storyboardScenes.length || 0;
        const cycleHtml = cycles.map(function(cycle, index) {
          const title = cycle.name || cycle.title || cycle.stage || ("周期 " + (index + 1));
          const goal = cycle.goal || cycle.summary || cycle.description || "";
          return "<div class=\"cycle-row\"><strong>" + escapeHtml(title) + "</strong><div class=\"muted\">" + escapeHtml(goal) + "</div></div>";
        }).join("");
        const outlineHtml = visibleScenes.length ? "<div class=\"scene-outline\">" + visibleScenes.map(function(scene, index) {
          const title = scene.title || scene.scene_id || ("第 " + (index + 1) + " 页");
          const functionName = scene.story_function || scene.storyFunction || "";
          const narration = scene.narration || scene.subtitle || "";
          return "<div class=\"scene-outline-item\"><strong>" + escapeHtml(index + 1) + ". " + escapeHtml(title) + "</strong><span>" + escapeHtml(functionName || narration.slice(0, 26)) + "</span></div>";
        }).join("") + "</div>" : "";
        storyPlan.innerHTML =
          "<div class=\"metric\"><div class=\"k\">结构判断</div><div class=\"v\">" + escapeHtml(plan.cycle_count) + " 周期 · " + escapeHtml(sceneCount) + " 页绘本" + (plan.cycle_reason ? " · " + escapeHtml(plan.cycle_reason) : "") + "</div></div>" +
          cycleHtml +
          outlineHtml;
      }

      function firstImage(scene) {
        if (!scene) return "";
        if (Array.isArray(scene.keyframes)) {
          for (let i = 0; i < scene.keyframes.length; i++) {
            if (scene.keyframes[i].image_preview_url) return scene.keyframes[i].image_preview_url;
          }
        }
        return scene.image_preview_url || "";
      }

      function subtitleStatus(scene) {
        return scene && scene.subtitle_preview_url && scene.subtitle_ass_preview_url ? "success" : "pending";
      }

      function chipStatus(status) {
        return normalizeEventStatus(status || "pending");
      }

      function sceneChip(label, status) {
        const normalized = chipStatus(status);
        const text = normalized === "success" || normalized === "done"
          ? "完成"
          : normalized === "running"
            ? "生成中"
            : normalized === "failed" || normalized === "error"
              ? "失败"
              : normalized === "preview_ready"
                ? "预览"
                : "等待";
        return "<span class=\"scene-chip " + escapeHtml(normalized) + "\">" + escapeHtml(label) + "：" + escapeHtml(text) + "</span>";
      }

      function keyframeStrip(scene) {
        if (!scene || !Array.isArray(scene.keyframes)) return "";
        const frames = scene.keyframes.filter(function(frame) { return frame && frame.image_preview_url; });
        if (frames.length <= 1) return "";
        return "<div class=\"keyframe-strip\">" + frames.map(function(frame, index) {
          return "<img src=\"" + escapeHtml(frame.image_preview_url) + "\" alt=\"关键帧 " + escapeHtml(index + 1) + "\" loading=\"lazy\" />";
        }).join("") + "</div>";
      }

      function renderScenes(scenes) {
        if (!scenes || !scenes.length) {
          sceneGrid.innerHTML = "<div class=\"empty\">分镜一生成，这里会立即出现每一页；图片一生成，会直接显示缩略图。</div>";
          return;
        }
        sceneGrid.innerHTML = scenes.map(function(scene) {
          const image = firstImage(scene);
          const media = scene.scene_video_preview_url
            ? (String(scene.scene_video_mime_type || "").indexOf("video/") === 0
              ? "<video controls preload=\"metadata\" src=\"" + escapeHtml(scene.scene_video_preview_url) + "\"></video>"
              : "<iframe src=\"" + escapeHtml(scene.scene_video_preview_url) + "\" loading=\"lazy\"></iframe>")
            : (image ? "<img src=\"" + escapeHtml(image) + "\" alt=\"" + escapeHtml(scene.title || scene.scene_id) + "\" loading=\"lazy\" />" : "<div class=\"scene-placeholder\">等待 Codex 出图</div>");
          const statusRow = "<div class=\"scene-status-row\">" +
            sceneChip("图", scene.image_status) +
            sceneChip("语音", scene.audio_status) +
            sceneChip("字幕", subtitleStatus(scene)) +
            sceneChip("片段", scene.compose_status) +
          "</div>";
          return "<article class=\"scene\">" +
            media +
            "<div class=\"scene-body\">" +
              "<h3>" + escapeHtml(scene.sequence) + ". " + escapeHtml(scene.title || scene.scene_id) + "</h3>" +
              "<p>" + escapeHtml(scene.narration || "") + "</p>" +
              statusRow +
              keyframeStrip(scene) +
              (scene.subtitle_preview_url ? "<div class=\"muted\"><a href=\"" + escapeHtml(scene.subtitle_preview_url) + "\" target=\"_blank\" rel=\"noreferrer\">SRT</a> · <a href=\"" + escapeHtml(scene.subtitle_ass_preview_url || scene.subtitle_preview_url) + "\" target=\"_blank\" rel=\"noreferrer\">ASS</a></div>" : "") +
            "</div>" +
          "</article>";
        }).join("");
      }

      function renderFinal(project) {
        if (!project || !project.project_id) {
          finalOutput.innerHTML = "生成完成后显示最终视频入口。";
          finalOutput.className = "empty";
          return;
        }
        if (!project.final_video_preview_url) {
          finalOutput.innerHTML = "<div class=\"muted\">最终视频尚未生成。</div>";
          finalOutput.className = "empty";
          return;
        }
        finalOutput.className = "surface";
        const preview = String(project.final_video_mime_type || "").indexOf("video/") === 0
          ? "<video style=\"width:100%;max-height:420px;background:#000;\" controls preload=\"metadata\" src=\"" + escapeHtml(project.final_video_preview_url) + "\"></video>"
          : "<a href=\"" + escapeHtml(project.final_video_preview_url) + "\" target=\"_blank\" rel=\"noreferrer\">打开最终预览</a>";
        finalOutput.innerHTML = preview + "<p><a href=\"" + escapeHtml(project.final_video_preview_url) + "\" target=\"_blank\" rel=\"noreferrer\">新窗口打开</a></p>";
      }

      function renderDetail(detail) {
        currentDetail = detail || null;
        renderStages(detail);
        renderProject(detail && detail.project);
        renderStoryPlan(detail && detail.storyboard, detail && detail.scenes);
        renderScenes(detail && detail.scenes);
        renderFinal(detail && detail.project);
        renderProgressPanel();
      }

      async function loadProject(projectId) {
        const detail = await callApi("/api/projects/" + encodeURIComponent(projectId));
        await fetchProjectEvents(projectId, true).catch(function() {});
        renderDetail(detail);
        return detail;
      }

      async function continueProjectPipeline(projectId, button) {
        if (!projectId) {
          setStatus("请先选择项目");
          return;
        }
        projectIdInput.value = projectId;
        if (button) button.disabled = true;
        setStatus("继续运行流水线：" + projectId);
        startEventPolling(projectId);
        try {
          const detail = await callApi("/api/projects/" + encodeURIComponent(projectId) + "/run-all", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(buildRunAllPayload())
          });
          await fetchProjectEvents(projectId, false).catch(function() {});
          renderDetail(detail);
          await loadProjectList(false).catch(function() {});
          setStatus("流水线已完成：" + projectId);
        } catch (error) {
          await fetchProjectEvents(projectId, false).catch(function() {});
          await loadProject(projectId).catch(function() {});
          await loadProjectList(false).catch(function() {});
          setStatus(error.message);
        } finally {
          stopEventPolling();
          if (button) button.disabled = false;
        }
      }

      document.getElementById("generateFromThemeBtn").addEventListener("click", async function() {
        const button = this;
        button.disabled = true;
        stopEventPolling();
        resetProgress();
        setStatus("正在创建项目...");
        renderStages({});
        let projectId = "";
        try {
          const created = await callApi("/api/projects", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(buildCreateProjectPayload())
          });
          projectId = created.project && created.project.project_id;
          if (!projectId) throw new Error("项目创建成功但没有返回 project_id");
          projectIdInput.value = projectId;
          renderDetail({ project: created.project, scenes: [], tasks: [], files: {} });
          await loadProjectList(false).catch(function() {});
          startEventPolling(projectId);
          setStatus("项目已创建，Codex 正在规划故事并生成完整视频...");
          const detail = await callApi("/api/projects/" + encodeURIComponent(projectId) + "/run-all", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(buildRunAllPayload())
          });
          await fetchProjectEvents(projectId, false).catch(function() {});
          renderDetail(detail);
          await loadProjectList(false).catch(function() {});
          setStatus("绘本故事视频已完成");
        } catch (error) {
          if (projectId) {
            await fetchProjectEvents(projectId, false).catch(function() {});
            await loadProject(projectId).catch(function() {});
            await loadProjectList(false).catch(function() {});
          } else if (error.data && error.data.detail && error.data.detail.project) {
            renderDetail(error.data.detail);
            projectIdInput.value = error.data.project_id || error.data.detail.project.project_id || "";
          }
          setStatus(error.message);
        } finally {
          stopEventPolling();
          button.disabled = false;
        }
      });

      document.getElementById("healthBtn").addEventListener("click", async function() {
        setStatus("检查服务中...");
        try {
          await callApi("/healthz");
          setStatus("健康检查完成");
        } catch (error) {
          setStatus(error.message);
        }
      });

      document.getElementById("listBtn").addEventListener("click", async function() {
        setStatus("读取项目列表...");
        try {
          await loadProjectList(true);
          setStatus("历史项目已刷新");
        } catch (error) {
          setStatus(error.message);
        }
      });

      document.getElementById("loadProjectBtn").addEventListener("click", async function() {
        const projectId = projectIdInput.value.trim();
        if (!projectId) {
          setStatus("请先输入项目 ID");
          return;
        }
        setStatus("读取项目...");
        try {
          await loadProject(projectId);
          setStatus("项目已加载");
        } catch (error) {
          setStatus(error.message);
        }
      });

      document.getElementById("continuePipelineBtn").addEventListener("click", async function() {
        const projectId = projectIdInput.value.trim();
        if (!projectId) {
          setStatus("请先输入项目 ID");
          return;
        }
        await continueProjectPipeline(projectId, this);
      });

      projectList?.addEventListener("click", async function(event) {
        const loadButton = event.target.closest("[data-project-load]");
        if (loadButton) {
          const projectId = loadButton.getAttribute("data-project-load");
          if (!projectId) return;
          loadButton.disabled = true;
          setStatus("读取项目：" + projectId);
          try {
            await loadProject(projectId);
            setStatus("项目已加载：" + projectId);
          } catch (error) {
            setStatus(error.message);
          } finally {
            loadButton.disabled = false;
          }
          return;
        }
        const continueButton = event.target.closest("[data-project-continue]");
        if (continueButton) {
          const projectId = continueButton.getAttribute("data-project-continue");
          await continueProjectPipeline(projectId, continueButton);
        }
      });

      progressCloseBtn?.addEventListener("click", function() {
        setProgressDrawer(false);
      });
      progressRailBtn?.addEventListener("click", function() {
        setProgressDrawer(true);
      });
      let savedProgressOpen = "0";
      try {
        savedProgressOpen = localStorage.getItem("storyVideoProgressDrawerOpen") || "0";
      } catch {}
      setProgressDrawer(savedProgressOpen !== "0");

      renderStages({});
      renderProject(null);
      renderProjectList([]);
      renderStoryPlan(null, []);
      renderScenes([]);
      renderFinal(null);
      renderProgressPanel();
      loadProjectList(false).catch(function(error) {
        setStatus(error.message);
      });
    </script>
  </body>
</html>`

func buildLegacyHomeHTML(projectsDir string, zeroTokenBuilt bool) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Story Video Server</title>
    <style>
      :root {
        color-scheme: light dark;
        font-family: Inter, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      }
      body {
        margin: 0;
        background: #0b1020;
        color: #e8eefc;
      }
      main {
        max-width: 1080px;
        margin: 0 auto;
        padding: 24px;
      }
      h1, h2 {
        margin: 0 0 12px;
      }
      p {
        color: #b8c2dd;
      }
      .grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
        gap: 16px;
        margin-top: 20px;
      }
      .panel {
        background: #121936;
        border: 1px solid #26304f;
        border-radius: 12px;
        padding: 16px;
      }
      label {
        display: block;
        margin: 10px 0 6px;
        font-size: 13px;
        color: #b8c2dd;
      }
      input, textarea, select, button {
        width: 100%%;
        box-sizing: border-box;
        border-radius: 8px;
        border: 1px solid #324068;
        background: #0e1530;
        color: #e8eefc;
        padding: 10px 12px;
        font: inherit;
      }
      textarea {
        min-height: 120px;
        resize: vertical;
      }
      button {
        cursor: pointer;
        background: #3b82f6;
        border-color: #3b82f6;
        font-weight: 600;
        margin-top: 12px;
      }
      button.secondary {
        background: #182342;
        border-color: #324068;
      }
      .status {
        display: inline-block;
        margin-top: 8px;
        font-size: 13px;
        color: #98a6cf;
      }
      pre {
        margin: 0;
        white-space: pre;
        background: #09101f;
        border-radius: 10px;
        padding: 14px;
        border: 1px solid #26304f;
        height: 320px;
        max-height: 320px;
        overflow: auto;
      }
      code {
        background: #182342;
        padding: 2px 6px;
        border-radius: 6px;
      }
      ul {
        padding-left: 18px;
        color: #cfd8f3;
      }
      h3 {
        margin: 0 0 10px;
      }
      .inline-actions {
        display: flex;
        gap: 10px;
        margin-top: 12px;
      }
      .inline-actions button {
        margin-top: 0;
      }
      .stack {
        display: grid;
        gap: 16px;
      }
      .detail-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
        gap: 16px;
      }
      .muted {
        color: #98a6cf;
        font-size: 13px;
      }
      .meta-list {
        display: grid;
        gap: 10px;
      }
      .project-actions {
        margin-top: 12px;
        display: grid;
        gap: 12px;
      }
      .meta-item {
        padding: 12px;
        border-radius: 10px;
        background: #0e1530;
        border: 1px solid #26304f;
      }
      .scene-list, .task-list {
        display: grid;
        gap: 12px;
      }
      .scene-card, .task-row {
        padding: 14px;
        border-radius: 10px;
        background: #0e1530;
        border: 1px solid #26304f;
      }
      .scene-head, .task-head {
        display: flex;
        justify-content: space-between;
        gap: 12px;
        align-items: flex-start;
      }
      .scene-actions {
        display: grid;
        grid-template-columns: repeat(3, minmax(0, 1fr));
        gap: 8px;
        margin-top: 12px;
      }
      .scene-actions button {
        margin-top: 0;
        padding: 9px 10px;
      }
      .task-chips {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-top: 10px;
      }
      .task-chip {
        border-radius: 999px;
        padding: 4px 10px;
        font-size: 12px;
        background: #182342;
        border: 1px solid #324068;
        color: #cfd8f3;
      }
      .scene-media-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
        align-items: start;
        gap: 12px;
        margin-top: 12px;
      }
      .image-panel {
        padding: 12px;
        border-radius: 10px;
        background: #101933;
        border: 1px solid #26304f;
        width: 100%%;
        max-width: 280px;
      }
      .image-panel h4 {
        margin: 0 0 10px;
        font-size: 14px;
      }
      .image-preview {
        width: auto;
        max-width: 100%%;
        max-height: 220px;
        object-fit: contain;
        border-radius: 10px;
        display: block;
        background: #060b18;
        border: 1px solid #26304f;
        margin: 0 auto;
      }
      .video-preview,
      .video-preview-frame {
        width: 100%%;
        max-width: 100%%;
        max-height: 220px;
        border-radius: 10px;
        display: block;
        background: #060b18;
        border: 1px solid #26304f;
        margin: 0 auto;
      }
      .video-preview-frame {
        aspect-ratio: 9 / 16;
      }
      .image-candidates {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-top: 10px;
      }
      .candidate-btn {
        padding: 7px 10px;
        border-radius: 999px;
        border: 1px solid #324068;
        background: #182342;
        color: #cfd8f3;
        cursor: pointer;
      }
      .candidate-btn.active {
        background: #23406c;
        border-color: #6ea8fe;
        color: #f8fbff;
      }
      .candidate-btn[disabled] {
        opacity: 1;
        cursor: default;
      }
      .force-ready {
        transition: background 0.15s ease, border-color 0.15s ease, color 0.15s ease;
      }
      .force-ready.force-active {
        background: #6c2a1e;
        border-color: #ff9b73;
        color: #fff5f1;
      }
      .small {
        font-size: 12px;
      }
      .empty-state {
        padding: 14px;
        border-radius: 10px;
        border: 1px dashed #324068;
        color: #98a6cf;
        background: #0e1530;
      }
      a.link {
        color: #93c5fd;
        text-decoration: none;
      }
      a.link:hover {
        text-decoration: underline;
      }
    </style>
  </head>
  <body>
    <main>
      <h1>Story Video Server</h1>
      <p>这是 Go 后端的最小操作页，用于验证本地项目目录、Go API 和 zero-token 桥接是否打通。</p>
      <p><strong>projects 目录：</strong> <code>%s</code></p>
      <p><strong>zero-token 构建状态：</strong> <code>%t</code></p>

      <div class="grid">
        <section class="panel">
          <h2>快速操作</h2>
          <button id="healthBtn" class="secondary">检查健康状态</button>
          <button id="listBtn" class="secondary">列出项目</button>

          <label for="title">项目标题</label>
          <input id="title" value="小云朵的星星收集之旅" />

          <label for="story">故事内容</label>
          <textarea id="story">在很远很远的天空上，住着一朵软乎乎的小云朵，名字叫棉棉。一天晚上，它决定帮助迷路的小星星回家。</textarea>

          <label for="providerRef">Storyboard Provider</label>
          <input id="providerRef" value="codex/image" />

          <label for="targetDuration">目标视频时长（秒）</label>
          <input id="targetDuration" type="number" min="1" value="60" />

          <label for="imageSwitchInterval">图片切换间隔（秒）</label>
          <input id="imageSwitchInterval" type="number" min="1" value="3" />

          <label for="aspectRatio">画面比例</label>
          <select id="aspectRatio">
            <option value="1:1">1:1 正方形</option>
            <option value="2:3">2:3 社交媒体/自拍</option>
            <option value="3:4">3:4 经典比例/拍照</option>
            <option value="4:3">4:3 文章配图/插画</option>
            <option value="9:16" selected>9:16 手机壁纸/人像</option>
            <option value="16:9">16:9 桌面壁纸/风景</option>
          </select>

          <button id="createBtn">创建项目</button>
          <span class="status" id="statusText">等待操作</span>
        </section>

        <section class="panel">
          <h2>Storyboard 生成</h2>
          <label for="projectId">项目 ID</label>
          <input id="projectId" placeholder="先创建项目，或输入已有 project_id" />

          <label for="storyboardProviderRef">Provider Ref</label>
          <input id="storyboardProviderRef" value="codex/image" />

          <button id="generateBtn">生成 Storyboard</button>
          <div class="inline-actions">
            <button id="detailBtn" class="secondary" type="button">读取项目详情</button>
          </div>

          <h2 style="margin-top: 20px;">接口</h2>
          <ul>
            <li><code>GET /healthz</code></li>
            <li><code>GET /api/projects</code></li>
            <li><code>POST /api/projects</code></li>
            <li><code>GET /api/projects/{projectId}</code></li>
            <li><code>GET /api/projects/{projectId}/scenes</code></li>
            <li><code>GET /api/projects/{projectId}/scenes/{sceneId}</code></li>
            <li><code>POST /api/projects/{projectId}/storyboard</code></li>
            <li><code>POST /api/projects/{projectId}/scenes/{sceneId}/keyframes</code></li>
            <li><code>POST /api/projects/{projectId}/scenes/{sceneId}/image</code></li>
            <li><code>POST /api/projects/{projectId}/scenes/{sceneId}/image/select</code></li>
			<li><code>POST /api/projects/{projectId}/scenes/{sceneId}/audio</code></li>
			<li><code>POST /api/projects/{projectId}/scenes/{sceneId}/video</code></li>
			<li><code>POST /api/projects/{projectId}/final-video</code></li>
            <li><code>GET /api/projects/{projectId}/assets</code></li>
            <li><code>GET /local/projects/{projectId}/...</code></li>
          </ul>
        </section>
      </div>

      <section class="panel" style="margin-top: 16px;">
        <h2>结果</h2>
        <pre id="output">{ "ok": true }</pre>
      </section>

      <section class="panel stack" style="margin-top: 16px;">
        <div>
          <h2>项目详情</h2>
          <p class="muted">读取 <code>GET /api/projects/{projectId}</code>，展示场景及相关任务，并可直接触发单场景生成接口。</p>
        </div>

        <div class="detail-grid">
          <div>
            <h3>项目概览</h3>
            <div id="projectMeta" class="meta-list">
              <div class="empty-state">输入 project_id 后点击“读取项目详情”。</div>
            </div>
            <div id="finalVideoPanel" class="project-actions">
              <div class="empty-state">场景任务完成后，可在这里手动触发最终视频生成。</div>
            </div>
          </div>
        </div>

        <div>
          <h3>场景任务</h3>
          <div id="sceneList" class="scene-list">
            <div class="empty-state">Storyboard 生成并拆分场景任务后，这里会显示每个 scene 的相关任务和操作按钮。</div>
          </div>
        </div>
      </section>
    </main>

    <script>
      const output = document.getElementById("output");
      const statusText = document.getElementById("statusText");
      const projectIdInput = document.getElementById("projectId");
      const projectMeta = document.getElementById("projectMeta");
      const finalVideoPanel = document.getElementById("finalVideoPanel");
      const sceneList = document.getElementById("sceneList");

      function setOutput(value) {
        output.textContent = JSON.stringify(value, null, 2);
      }

      function setStatus(message) {
        statusText.textContent = message;
      }

      async function readJson(res) {
        const text = await res.text();
        try {
          return JSON.parse(text);
        } catch {
          return { raw: text, status: res.status };
        }
      }

      async function callApi(path, init) {
        const res = await fetch(path, init);
        const data = await readJson(res);
        setOutput(data);
        if (!res.ok) {
          throw new Error(data.error || ("HTTP " + res.status));
        }
        return data;
      }

      function escapeHtml(value) {
        return String(value == null ? "" : value)
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")
          .replace(/>/g, "&gt;")
          .replace(/"/g, "&quot;")
          .replace(/'/g, "&#39;");
      }

      function formatTaskKind(kind) {
        if (!kind) {
          return "-";
        }
        return kind.replaceAll("_", " ");
      }

      function resolveImageCandidates(imageOwner) {
        if (!imageOwner) {
          return [];
        }
        if (Array.isArray(imageOwner.image_candidates) && imageOwner.image_candidates.length > 0) {
          return imageOwner.image_candidates;
        }
        if (imageOwner.image_preview_url || imageOwner.image_url) {
          return [{
            image_preview_url: imageOwner.image_preview_url,
            image_url: imageOwner.image_url,
            image_local_path: imageOwner.image_local_path,
            image_mime_type: imageOwner.image_mime_type
          }];
        }
        return [];
      }

      function resolveSelectedCandidateIndex(imageOwner, candidates) {
        if (!candidates.length) {
          return -1;
        }
        var selectedIndex = Number.isInteger(imageOwner && imageOwner.selected_image_index) ? imageOwner.selected_image_index : 0;
        if (selectedIndex >= 0 && selectedIndex < candidates.length) {
          return selectedIndex;
        }
        if (imageOwner && imageOwner.image_preview_url) {
          for (var i = 0; i < candidates.length; i++) {
            if (candidates[i].image_preview_url === imageOwner.image_preview_url) {
              return i;
            }
          }
        }
        return 0;
      }

      function renderImagePanel(taskSceneId, title, imageOwner) {
        var candidates = resolveImageCandidates(imageOwner);
        if (!candidates.length) {
          return "<div class=\"image-panel\"><h4>" + escapeHtml(title) + "</h4><div class=\"muted small\">暂无图片</div></div>";
        }
        var selectedIndex = resolveSelectedCandidateIndex(imageOwner, candidates);
        var selected = candidates[selectedIndex] || candidates[0];
        var previewUrl = selected.image_preview_url || selected.image_url || "";
        var candidateButtons = candidates.map(function(candidate, index) {
          var active = index === selectedIndex;
          var label = active ? "已选图 " + (index + 1) : "切换图 " + (index + 1);
          return "<button type=\"button\" class=\"candidate-btn" + (active ? " active" : "") + "\" data-scene-id=\"" + escapeHtml(taskSceneId) + "\" data-scene-action=\"select-image\" data-candidate-index=\"" + index + "\"" + (active ? " disabled" : "") + ">" + label + "</button>";
        }).join("");
        return "" +
          "<div class=\"image-panel\">" +
            "<h4>" + escapeHtml(title) + "</h4>" +
            "<img class=\"image-preview\" src=\"" + escapeHtml(previewUrl) + "\" alt=\"" + escapeHtml(title) + "\" />" +
            "<div class=\"image-candidates\">" + candidateButtons + "</div>" +
          "</div>";
      }

      function renderVideoPanel(scene) {
        if (!scene || !scene.scene_video_preview_url) {
          return "";
        }
        var previewUrl = scene.scene_video_preview_url;
        var mediaHtml = "";
        if ((scene.scene_video_mime_type || "").indexOf("video/") === 0 || /\.mp4(?:\?|$)/i.test(previewUrl)) {
          mediaHtml = "<video class=\"video-preview\" controls preload=\"metadata\" src=\"" + escapeHtml(previewUrl) + "\"></video>";
        } else {
          mediaHtml = "<iframe class=\"video-preview-frame\" src=\"" + escapeHtml(previewUrl) + "\" loading=\"lazy\"></iframe>";
        }
        return "" +
          "<div class=\"image-panel\">" +
            "<h4>视频预览</h4>" +
            mediaHtml +
          "</div>";
      }

      function isSceneTaskFinished(task) {
        return !!task && task.status === "success";
      }

      function parseTimestamp(value) {
        if (!value) {
          return 0;
        }
        var timestamp = Date.parse(value);
        return Number.isNaN(timestamp) ? 0 : timestamp;
      }

      function resolveLatestSceneImageTimestamp(scene) {
        var latest = parseTimestamp(scene && scene.image_generated_at);
        if (scene && Array.isArray(scene.keyframes)) {
          for (var i = 0; i < scene.keyframes.length; i++) {
            var keyframeTimestamp = parseTimestamp(scene.keyframes[i] && scene.keyframes[i].image_generated_at);
            if (keyframeTimestamp > latest) {
              latest = keyframeTimestamp;
            }
          }
        }
        return latest;
      }

      function isSceneVideoStale(scene) {
        if (!scene) {
          return false;
        }
        var composedAt = parseTimestamp(scene.composed_at);
        if (!composedAt) {
          return false;
        }
        if (resolveLatestSceneImageTimestamp(scene) > composedAt) {
          return true;
        }
        if (parseTimestamp(scene.audio_generated_at) > composedAt) {
          return true;
        }
        return false;
      }

      function shouldShowVideoUpdate(scene, videoTask, canRunVideo) {
        if (!scene || !videoTask || videoTask.status !== "success" || !canRunVideo) {
          return false;
        }
        if (scene.compose_status === "success" || scene.compose_status === "preview_ready") {
          return isSceneVideoStale(scene);
        }
        return !scene.compose_status;
      }

      function canGenerateFinalVideo(scenes, tasks) {
        if (!scenes || scenes.length === 0) {
          return false;
        }
        return scenes.every(function(scene) {
          const sceneTasks = (tasks || []).filter(function(task) {
            if (!task.scene_id) return false;
            return task.scene_id === scene.scene_id || task.scene_id.startsWith(scene.scene_id + "_kf");
          });
          const imageTasks = sceneTasks.filter(function(task) { return task.kind === "scene_image_generation"; });
          const allImagesDone = imageTasks.length > 0 && imageTasks.every(function(task) { return task.status === "success"; });
          const audioTask = sceneTasks.find(function(task) { return task.kind === "scene_audio_generation"; });
          const videoTask = sceneTasks.find(function(task) { return task.kind === "scene_video_compositing"; });
          return allImagesDone &&
            isSceneTaskFinished(audioTask) &&
            isSceneTaskFinished(videoTask) &&
            (scene.compose_status === "success" || scene.compose_status === "preview_ready");
        });
      }

      function renderProjectMeta(project) {
        if (!project) {
          projectMeta.innerHTML = "<div class=\"empty-state\">暂无项目信息。</div>";
          return;
        }
        const items = [
          ["项目 ID", project.project_id],
          ["标题", project.title],
          ["画面比例", project.aspect_ratio || "9:16"],
          ["状态", project.status],
          ["场景数", project.scene_count || 0],
          ["Storyboard 有效", project.storyboard_valid ? "yes" : "no"],
          ["Final Video", project.final_video_status || "-"]
        ];
        projectMeta.innerHTML = items.map(function(item) {
          return "<div class=\"meta-item\"><div class=\"muted\">" + escapeHtml(item[0]) + "</div><div>" + escapeHtml(item[1]) + "</div></div>";
        }).join("");
      }

      function renderFinalVideoPanel(project, scenes, tasks) {
        if (!project) {
          finalVideoPanel.innerHTML = "<div class=\"empty-state\">暂无最终视频信息。</div>";
          return;
        }
        const ready = canGenerateFinalVideo(scenes, tasks);
        const running = project.final_video_status === "running";
        const preview = project.final_video_preview_url
          ? "<a class=\"link\" href=\"" + escapeHtml(project.final_video_preview_url) + "\" target=\"_blank\" rel=\"noreferrer\">打开预览</a>"
          : "暂无预览";
        const finalDisabled = (ready || project.final_video_status === "failed") && !running ? "" : " disabled";
        const finalBtnLabel = "生成最终视频 (" + escapeHtml(project.final_video_status || "pending") + ")";
        const hint = ready
          ? "全部场景任务已完成，可以手动触发最终视频生成。"
          : "部分场景任务未完成，点击「一键运行所有任务」自动执行。";
        const hasPendingTasks = !ready || project.final_video_status === "failed";
        finalVideoPanel.innerHTML = "" +
          "<div class=\"meta-item\">" +
            "<div class=\"muted\">最终视频状态</div>" +
            "<div>" + escapeHtml(project.final_video_status || "pending") + "</div>" +
            "<div class=\"muted small\" style=\"margin-top: 8px;\">" + escapeHtml(hint) + "</div>" +
            "<div class=\"muted small\" style=\"margin-top: 8px;\">预览: " + preview + "</div>" +
          "</div>" +
          (hasPendingTasks ? "<button id=\"runAllTasksBtn\" type=\"button\">一键运行所有任务</button>" : "") +
          "<button id=\"finalVideoBtn\" type=\"button\"" + finalDisabled + ">" + finalBtnLabel + "</button>";
      }

      function renderSceneList(scenes, tasks) {
        if (!scenes || scenes.length === 0) {
          sceneList.innerHTML = "<div class=\"empty-state\">暂无场景数据，先生成 storyboard。</div>";
          return;
        }

        sceneList.innerHTML = scenes.map(function(scene) {
          const sceneTasks = (tasks || []).filter(function(task) {
            if (!task.scene_id) return false;
            return task.scene_id === scene.scene_id || task.scene_id.startsWith(scene.scene_id + "_kf");
          });
          const taskHtml = sceneTasks.length > 0
            ? sceneTasks.map(function(task) {
                return "<div class=\"task-chip\">" + escapeHtml(formatTaskKind(task.kind)) + ": " + escapeHtml(task.status || "-") + "</div>";
              }).join("")
            : "<div class=\"muted small\">暂无关联任务</div>";
          const canRunVideo = scene.image_status === "success" && scene.audio_status === "success";

          var keyframeTask = sceneTasks.find(function(task) { return task.kind === "scene_keyframe_prompt_generation" && task.scene_id === scene.scene_id; });
          var keyframeStatus = Array.isArray(scene.keyframes) && scene.keyframes.length > 0 ? "success" : "pending";
          if (keyframeTask && keyframeTask.status === "running") {
            keyframeStatus = "running";
          } else if (keyframeTask && keyframeTask.status === "failed") {
            keyframeStatus = "failed";
          }
          var keyframeBtnLabel = "关键帧提示词 (" + escapeHtml(keyframeStatus) + ")";
          var keyframeDisabled = keyframeStatus === "running" ? " disabled" : "";
          var keyframeForceAttrs = keyframeStatus === "success"
            ? " class=\"force-ready\" data-force-eligible=\"true\" data-default-label=\"" + escapeHtml(keyframeBtnLabel) + "\" data-force-label=\"重新生成 关键帧提示词\""
            : "";

          const imageTasks = sceneTasks.filter(function(task) { return task.kind === "scene_image_generation"; });
          const imageButtonsHtml = imageTasks.map(function(task) {
            var label = "图片任务";
            if (task.scene_id.indexOf("_kf") >= 0) {
              var kfNum = task.scene_id.split("_kf")[1];
              label = "关键帧 " + (parseInt(kfNum) + 1);
            }
            var btnLabel = label + " (" + escapeHtml(task.status || "pending") + ")";
            var disabled = task.status === "running" ? " disabled" : "";
            var forceAttrs = task.status === "success"
              ? " class=\"force-ready\" data-force-eligible=\"true\" data-default-label=\"" + escapeHtml(btnLabel) + "\" data-force-label=\"强制重生 " + escapeHtml(label) + "\""
              : "";
            return "<button type=\"button\" data-scene-id=\"" + escapeHtml(task.scene_id) + "\" data-scene-action=\"image\"" + forceAttrs + disabled + ">" + btnLabel + "</button>";
          }).join("");

          var imagePanelsHtml = "";
          if (Array.isArray(scene.keyframes) && scene.keyframes.length > 0) {
            imagePanelsHtml = scene.keyframes.map(function(keyframe, index) {
              return renderImagePanel(scene.scene_id + "_kf" + index, "关键帧 " + (index + 1), keyframe);
            }).join("");
          } else {
            imagePanelsHtml = renderImagePanel(scene.scene_id, "场景主图", scene);
          }
          imagePanelsHtml += renderVideoPanel(scene);

          var audioTask = sceneTasks.find(function(task) { return task.kind === "scene_audio_generation" && task.scene_id === scene.scene_id; });
          var audioStatus = audioTask ? audioTask.status : "pending";
          var audioBtnLabel = "音频任务 (" + escapeHtml(audioStatus) + ")";
          var audioDisabled = audioStatus === "running" ? " disabled" : "";

          var videoTask = sceneTasks.find(function(task) { return task.kind === "scene_video_compositing" && task.scene_id === scene.scene_id; });
          var videoStatus = videoTask ? videoTask.status : "pending";
          var videoNeedsUpdate = shouldShowVideoUpdate(scene, videoTask, canRunVideo);
          var videoBtnLabel = videoNeedsUpdate
            ? "视频任务 (需更新)"
            : "视频任务 (" + escapeHtml(videoStatus) + ")";
          var videoDisabled = videoStatus === "running" ? " disabled" : (canRunVideo ? "" : " disabled");
          var videoForceAttrs = videoTask && videoTask.status === "success"
            ? " class=\"force-ready\" data-force-eligible=\"true\" data-default-label=\"" + escapeHtml(videoBtnLabel) + "\" data-force-label=\"强制重生 视频任务\""
            : "";

          return "" +
            "<div class=\"scene-card\">" +
              "<div class=\"scene-head\">" +
                "<div>" +
                  "<div><strong>" + escapeHtml(scene.scene_id) + "</strong> · " + escapeHtml(scene.title || "Untitled Scene") + "</div>" +
                  "<div class=\"muted small\">sequence: " + escapeHtml(scene.sequence || "-") + " · status: " + escapeHtml(scene.status || "-") + "</div>" +
                "</div>" +
                "<div class=\"task-chip\">" + escapeHtml(scene.compose_status || "pending") + "</div>" +
              "</div>" +
              "<div class=\"muted small\" style=\"margin-top: 8px;\">image: " + escapeHtml(scene.image_status || "-") + " · audio: " + escapeHtml(scene.audio_status || "-") + " · video: " + escapeHtml(scene.compose_status || "-") + "</div>" +
              "<div class=\"task-chips\">" + taskHtml + "</div>" +
              "<div class=\"scene-media-grid\">" + imagePanelsHtml + "</div>" +
              "<div class=\"scene-actions\">" +
                "<button type=\"button\" data-scene-id=\"" + escapeHtml(scene.scene_id) + "\" data-scene-action=\"keyframes\"" + keyframeForceAttrs + keyframeDisabled + ">" + keyframeBtnLabel + "</button>" +
                imageButtonsHtml +
                "<button type=\"button\" data-scene-id=\"" + escapeHtml(scene.scene_id) + "\" data-scene-action=\"audio\"" + audioDisabled + ">" + audioBtnLabel + "</button>" +
                "<button type=\"button\" data-scene-id=\"" + escapeHtml(scene.scene_id) + "\" data-scene-action=\"video\"" + videoForceAttrs + videoDisabled + ">" + videoBtnLabel + "</button>" +
              "</div>" +
            "</div>";
        }).join("");
      }

      function renderProjectDetail(detail) {
        renderProjectMeta(detail.project);
        renderFinalVideoPanel(detail.project, detail.scenes || [], detail.tasks || []);
        renderSceneList(detail.scenes || [], detail.tasks || []);
      }

      async function loadProjectDetail(projectId) {
        if (!projectId) {
          throw new Error("请先输入 project_id");
        }
        const detail = await callApi("/api/projects/" + encodeURIComponent(projectId));
        renderProjectDetail(detail);
        return detail;
      }

      document.getElementById("healthBtn").addEventListener("click", async () => {
        setStatus("检查中...");
        try {
          await callApi("/healthz");
          setStatus("健康检查完成");
        } catch (error) {
          setStatus(error.message);
        }
      });

      document.getElementById("listBtn").addEventListener("click", async () => {
        setStatus("读取项目中...");
        try {
          await callApi("/api/projects");
          setStatus("项目列表已刷新");
        } catch (error) {
          setStatus(error.message);
        }
      });

      document.getElementById("createBtn").addEventListener("click", async () => {
        setStatus("创建项目中...");
        try {
          const data = await callApi("/api/projects", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              title: document.getElementById("title").value,
              story: document.getElementById("story").value,
              provider_ref: document.getElementById("providerRef").value,
              target_duration_sec: parseInt(document.getElementById("targetDuration").value),
              image_switch_interval_sec: parseInt(document.getElementById("imageSwitchInterval").value),
              aspect_ratio: document.getElementById("aspectRatio").value
            })
          });
          const projectId = data.project && data.project.project_id;
          if (projectId) {
            projectIdInput.value = projectId;
            await loadProjectDetail(projectId);
          }
          setStatus("项目已创建");
        } catch (error) {
          setStatus(error.message);
        }
      });

      document.getElementById("generateBtn").addEventListener("click", async () => {
        const projectId = projectIdInput.value.trim();
        if (!projectId) {
          setStatus("请先输入 project_id");
          return;
        }
        setStatus("生成 storyboard 中...");
        try {
          await callApi("/api/projects/" + encodeURIComponent(projectId) + "/storyboard", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              provider_ref: document.getElementById("storyboardProviderRef").value,
              timeout_ms: 300000
            })
          });
          await loadProjectDetail(projectId);
          setStatus("storyboard 生成完成");
        } catch (error) {
          setStatus(error.message);
        }
      });

      document.getElementById("detailBtn").addEventListener("click", async () => {
        const projectId = projectIdInput.value.trim();
        if (!projectId) {
          setStatus("请先输入 project_id");
          return;
        }
        setStatus("读取项目详情中...");
        try {
          await loadProjectDetail(projectId);
          setStatus("项目详情已刷新");
        } catch (error) {
          setStatus(error.message);
        }
      });

      function buildSceneActionRequestBody(action, button) {
        if (action === "audio") {
	          return {
	            provider_ref: "`+defaultKokoroProviderRef+`",
	            voice_name: "`+defaultKokoroVoiceName+`",
	            speaking_rate: "%s",
	            pitch: "0Hz"
	          };
        }
        if (action === "image" && button && button.dataset.forceMode === "true") {
          return { force: true };
        }
        if (action === "keyframes" && button && button.dataset.forceMode === "true") {
          return { force: true };
        }
        if (action === "video" && button && button.dataset.forceMode === "true") {
          return { force: true };
        }
        if (action === "select-image" && button) {
          return { candidate_index: parseInt(button.dataset.candidateIndex || "0", 10) };
        }
        return null;
      }

      function buildSceneActionPath(projectId, sceneId, action) {
        if (action === "select-image") {
          return "/api/projects/" + encodeURIComponent(projectId) + "/scenes/" + encodeURIComponent(sceneId) + "/image/select";
        }
        return "/api/projects/" + encodeURIComponent(projectId) + "/scenes/" + encodeURIComponent(sceneId) + "/" + encodeURIComponent(action);
      }

      function setForceButtonState(button, forceMode) {
        if (!button || button.dataset.forceEligible !== "true") {
          return;
        }
        button.dataset.forceMode = forceMode ? "true" : "false";
        button.textContent = forceMode ? button.dataset.forceLabel : button.dataset.defaultLabel;
        button.classList.toggle("force-active", forceMode);
      }

      sceneList.addEventListener("mouseover", function(event) {
        var button = event.target.closest("button[data-force-eligible=\"true\"]");
        if (!button) {
          return;
        }
        setForceButtonState(button, true);
      });

      sceneList.addEventListener("mouseout", function(event) {
        var button = event.target.closest("button[data-force-eligible=\"true\"]");
        if (!button) {
          return;
        }
        var related = event.relatedTarget;
        if (related && button.contains(related)) {
          return;
        }
        setForceButtonState(button, false);
      });

      sceneList.addEventListener("click", async function(event) {
        const button = event.target.closest("button[data-scene-id][data-scene-action]");
        if (!button) {
          return;
        }
        const projectId = projectIdInput.value.trim();
        if (!projectId) {
          setStatus("请先输入 project_id");
          return;
        }
        const sceneId = button.getAttribute("data-scene-id");
        const action = button.getAttribute("data-scene-action");
        button.disabled = true;
        setStatus("执行 " + sceneId + " 的 " + action + " 任务中...");
        try {
          const options = { method: "POST" };
          const requestBody = buildSceneActionRequestBody(action, button);
          if (requestBody) {
            options.headers = { "Content-Type": "application/json" };
            options.body = JSON.stringify(requestBody);
          }
          await callApi(buildSceneActionPath(projectId, sceneId, action), options);
          await loadProjectDetail(projectId);
          if (action === "select-image") {
            setStatus(sceneId + " 已切换到候选图 " + ((parseInt(button.dataset.candidateIndex || "0", 10)) + 1));
          } else if ((action === "image" || action === "video" || action === "keyframes") && button.dataset.forceMode === "true") {
            var label = action === "video" ? "视频" : (action === "keyframes" ? "关键帧提示词" : "图片");
            setStatus(sceneId + " 已强制重生" + label);
          } else {
            setStatus(sceneId + " 的 " + action + " 任务已触发");
          }
        } catch (error) {
          setStatus(error.message);
        } finally {
          button.disabled = false;
        }
      });

      finalVideoPanel.addEventListener("click", async function(event) {
        const runAllBtn = event.target.closest("#runAllTasksBtn");
        if (runAllBtn) {
          const projectId = projectIdInput.value.trim();
          if (!projectId) {
            setStatus("请先输入 project_id");
            return;
          }
          runAllBtn.disabled = true;
          setStatus("服务端一键流水线运行中...");
          try {
            const detail = await callApi("/api/projects/" + encodeURIComponent(projectId) + "/run-all", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({
                provider_ref: document.getElementById("storyboardProviderRef").value,
	                timeout_ms: 300000,
	                audio_provider_ref: "`+defaultKokoroProviderRef+`",
	                voice_name: "`+defaultKokoroVoiceName+`",
	                speaking_rate: "%s",
	                pitch: "0Hz"
	              })
            });
            renderProjectDetail(detail);
            setStatus("一键流水线完成");
          } catch (error) {
            setStatus(error.message);
          } finally {
            runAllBtn.disabled = false;
          }
          return;
        }

        const button = event.target.closest("#finalVideoBtn");
        if (!button) {
          return;
        }
        const projectId = projectIdInput.value.trim();
        if (!projectId) {
          setStatus("请先输入 project_id");
          return;
        }
        button.disabled = true;
        setStatus("生成最终视频中...");
        try {
          await callApi("/api/projects/" + encodeURIComponent(projectId) + "/final-video", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({})
          });
          await loadProjectDetail(projectId);
          setStatus("最终视频生成已触发");
        } catch (error) {
          setStatus(error.message);
        } finally {
          button.disabled = false;
        }
      });
    </script>
  </body>
	</html>`, projectsDir, zeroTokenBuilt, defaultChildSpeakingRate, defaultChildSpeakingRate)
}
