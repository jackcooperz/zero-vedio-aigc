package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteJSONFile_NoHTMLEscape(t *testing.T) {
	testPath := "./test.json"
	t.Logf("testPath: %s", testPath)

	type testStruct struct {
		URL string `json:"url"`
	}

	input := testStruct{
		URL: "https://example.com/image.jpeg?lk3s=8e244e95&rcl=20260428&x-signature=q%2FQeAcg4umSvUjXDua%2FeCZqwKM0%3D",
	}

	err := writeJSONFile(testPath, input)
	if err != nil {
		t.Fatalf("writeJSONFile failed: %v", err)
	}

	data, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}

	content := string(data)

	if strings.Contains(content, "\\u0026") {
		t.Errorf("JSON contains HTML-escaped & as \\u0026:\n%s", content)
	}

	if !strings.Contains(content, "&") {
		t.Errorf("JSON should contain literal & character, got:\n%s", content)
	}

	var decoded testStruct
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.URL != input.URL {
		t.Errorf("decoded URL mismatch: got %q, want %q", decoded.URL, input.URL)
	}
}

func TestWriteJSONFile_VsMarshalIndent(t *testing.T) {
	type testStruct struct {
		URL string `json:"url"`
	}

	input := testStruct{
		URL: "https://example.com?a=1&b=2",
	}

	marshaled, _ := json.MarshalIndent(input, "", "  ")

	if strings.Contains(string(marshaled), "\\u0026") {
		t.Log("json.MarshalIndent escapes & to \\u0026 (expected behavior)")
	}

	testPath := "./test1.json"
	_ = writeJSONFile(testPath, input)

	data, _ := os.ReadFile(testPath)

	if strings.Contains(string(data), "\\u0026") {
		t.Errorf("writeJSONFile should NOT escape &, but got:\n%s", string(data))
	}
}

func TestWriteSceneSubtitleFiles_GeneratesSegmentedSRTAndASS(t *testing.T) {
	tmp := t.TempDir()
	srtPath := filepath.Join(tmp, "scene.srt")
	assPath := filepath.Join(tmp, "scene.ass")

	err := writeSceneSubtitleFiles(srtPath, assPath, "小兔子走进森林，发现星星藏在树叶后面。它轻轻挥手，邀请大家一起听故事。", 5200)
	if err != nil {
		t.Fatalf("writeSceneSubtitleFiles failed: %v", err)
	}

	srt, err := os.ReadFile(srtPath)
	if err != nil {
		t.Fatalf("read srt failed: %v", err)
	}
	ass, err := os.ReadFile(assPath)
	if err != nil {
		t.Fatalf("read ass failed: %v", err)
	}
	if strings.Count(string(srt), "-->") < 2 {
		t.Fatalf("expected segmented srt, got:\n%s", string(srt))
	}
	if strings.Contains(string(srt), "\n狼\n") {
		t.Fatalf("subtitle should not isolate a single character, got:\n%s", string(srt))
	}
	assText := string(ass)
	if !strings.Contains(assText, "Style: PictureBook") || !strings.Contains(assText, "Dialogue: 0") {
		t.Fatalf("expected picture-book ass dialogues, got:\n%s", assText)
	}
	if !strings.Contains(assText, `\k`) {
		t.Fatalf("expected karaoke character timing in ass, got:\n%s", assText)
	}
	if strings.Contains(assText, "BorderStyle=3") {
		t.Fatalf("ass should not use opaque subtitle box style, got:\n%s", assText)
	}
}

func TestSplitSubtitleTextKeepsWholeSentences(t *testing.T) {
	chunks := splitSubtitleText("月光洒在胡萝卜田，小兔朵朵提着篮子回家。路口忽然站着大灰狼，肚子咕咕叫，想拦住她的路。", 0)
	want := []string{
		"月光洒在胡萝卜田，小兔朵朵提着篮子回家。",
		"路口忽然站着大灰狼，肚子咕咕叫，想拦住她的路。",
	}
	if len(chunks) != len(want) {
		t.Fatalf("chunks = %#v, want %#v", chunks, want)
	}
	for i := range want {
		if chunks[i] != want[i] {
			t.Fatalf("chunks[%d] = %q, want %q; all chunks: %#v", i, chunks[i], want[i], chunks)
		}
	}
}

func TestStoryVideoDefaultsCodexOnly(t *testing.T) {
	if got := defaultProviderRef(""); got != defaultCodexImageProviderRef {
		t.Fatalf("defaultProviderRef() = %q, want %q", got, defaultCodexImageProviderRef)
	}
	if got := defaultProviderRef("other/web"); got != defaultCodexImageProviderRef {
		t.Fatalf("defaultProviderRef(other/web) = %q, want codex-only %q", got, defaultCodexImageProviderRef)
	}
	if got := resolveImageProviderRef("other/web", "legacy/web"); got != defaultCodexImageProviderRef {
		t.Fatalf("resolveImageProviderRef() = %q, want %q", got, defaultCodexImageProviderRef)
	}
}

func TestProjectEventsAppendAndRead(t *testing.T) {
	tmp := t.TempDir()
	a := &app{projectsDir: tmp}
	project, err := a.createProject(createProjectRequest{
		Title:       "测试绘本",
		Story:       "主题：一颗小种子",
		ProviderRef: defaultCodexImageProviderRef,
		AspectRatio: "16:9",
	})
	if err != nil {
		t.Fatalf("createProject failed: %v", err)
	}

	a.recordProjectEvent(project.ProjectID, "story", "running", "Codex 正在规划故事线", map[string]any{
		"cycle_mode": "auto",
	})
	a.recordProjectEvent(project.ProjectID, "story", "done", "故事线规划完成", map[string]any{
		"cycle_count": 3,
	})

	events, err := a.readProjectEvents(project.ProjectID, 0, 20)
	if err != nil {
		t.Fatalf("readProjectEvents failed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3 including project-created event", len(events))
	}
	if events[0].Seq != 1 || events[2].Seq != 3 {
		t.Fatalf("unexpected event seqs: %+v", events)
	}
	if events[2].Phase != "story" || events[2].Status != "done" {
		t.Fatalf("unexpected last event: %+v", events[2])
	}

	afterEvents, err := a.readProjectEvents(project.ProjectID, 1, 20)
	if err != nil {
		t.Fatalf("readProjectEvents after failed: %v", err)
	}
	if len(afterEvents) != 2 || afterEvents[0].Seq != 2 {
		t.Fatalf("unexpected after events: %+v", afterEvents)
	}
}

func TestListProjectsSortsByUpdatedAtDesc(t *testing.T) {
	tmp := t.TempDir()
	a := &app{projectsDir: tmp}
	oldProject, err := a.createProject(createProjectRequest{
		Title:       "旧项目",
		Story:       "主题：旧项目",
		ProviderRef: defaultCodexImageProviderRef,
		AspectRatio: "16:9",
	})
	if err != nil {
		t.Fatalf("create old project failed: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	newProject, err := a.createProject(createProjectRequest{
		Title:       "新项目",
		Story:       "主题：新项目",
		ProviderRef: defaultCodexImageProviderRef,
		AspectRatio: "16:9",
	})
	if err != nil {
		t.Fatalf("create new project failed: %v", err)
	}
	oldProject.UpdatedAt = "2026-06-30T01:00:00Z"
	newProject.UpdatedAt = "2026-06-30T02:00:00Z"
	if err := a.writeProject(oldProject); err != nil {
		t.Fatalf("write old project failed: %v", err)
	}
	if err := a.writeProject(newProject); err != nil {
		t.Fatalf("write new project failed: %v", err)
	}

	projects, err := a.listProjects()
	if err != nil {
		t.Fatalf("listProjects failed: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("projects len = %d, want 2", len(projects))
	}
	if projects[0].ProjectID != newProject.ProjectID {
		t.Fatalf("first project = %s, want newest %s", projects[0].ProjectID, newProject.ProjectID)
	}
}

func TestBuildBaseStoryboardPromptIncludesStoryPlanAndCodexOnly(t *testing.T) {
	project := projectFile{
		ProjectID:           "pv_test",
		Title:               "小月亮",
		Story:               buildThemeStoryInput(createStoryVideoFromThemeRequest{Theme: "一个胆小的小月亮学会照亮森林", CycleMode: "auto"}),
		TargetDurationSec:   60,
		AspectRatio:         "16:9",
		ProviderRef:         defaultCodexImageProviderRef,
		StoryboardValid:     false,
		FinalVideoStatus:    "",
		FinalVideoLocalPath: "",
	}
	prompt := buildBaseStoryboardPrompt(project)
	for _, needle := range []string{
		"story_plan",
		"cycle_count 只能是 3 或 5",
		defaultCodexImageProviderRef,
		defaultKokoroProviderRef,
		defaultKokoroVoiceName,
		defaultChildSpeakingRate,
		"故事周期模式：auto",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q:\n%s", needle, prompt)
		}
	}
}

func TestResolveSceneVoiceSettingsDefaultsToChildSpeakingRate(t *testing.T) {
	voiceName, speakingRate, pitch := resolveSceneVoiceSettings(nil, sceneFile{}, generateSceneAudioRequest{}, defaultKokoroProviderRef)
	if voiceName != defaultKokoroVoiceName {
		t.Fatalf("voiceName = %q, want %q", voiceName, defaultKokoroVoiceName)
	}
	if speakingRate != defaultChildSpeakingRate {
		t.Fatalf("speakingRate = %q, want %q", speakingRate, defaultChildSpeakingRate)
	}
	if pitch != "0%" {
		t.Fatalf("pitch = %q, want 0%%", pitch)
	}
}

func TestValidateStoryboardRequiresStoryPlanCycleCount(t *testing.T) {
	raw := []byte(`{
  "meta": { "image_provider_ref": "codex/image" },
  "project": { "title": "小月亮" },
  "story_plan": {
    "cycle_count": 3,
    "cycle_reason": "短故事适合三段式",
    "cycles": [
      { "name": "起因", "goal": "主角遇到问题" },
      { "name": "转折", "goal": "主角尝试改变" },
      { "name": "解决", "goal": "主角完成成长" }
    ]
  },
  "global_style": {},
  "character_bible": {},
  "audio_profile": { "tts_provider_ref": "kokoro/zf_xiaoyi", "voice_name": "zf_xiaoyi" },
  "video_profile": { "aspect_ratio": "16:9" },
  "render_rules": {},
  "scenes": [
    {
      "scene_id": "s01",
      "sequence": 1,
      "title": "起因",
      "story_function": "起因",
      "narration": "小月亮害怕黑暗。",
      "subtitle": "小月亮害怕黑暗。",
      "duration_hint_sec": 3,
      "characters": ["c01"],
      "objects": [],
      "environment": {},
      "visual": {},
      "prompt": { "subject_prompt": "小月亮", "scene_prompt": "森林夜晚", "full_prompt": "" },
      "audio": {},
      "effects": {}
    },
    {
      "scene_id": "s02",
      "sequence": 2,
      "title": "转折",
      "story_function": "转折",
      "narration": "它听见树叶轻轻求助。",
      "subtitle": "它听见树叶轻轻求助。",
      "duration_hint_sec": 3,
      "characters": ["c01"],
      "objects": [],
      "environment": {},
      "visual": {},
      "prompt": { "subject_prompt": "小月亮", "scene_prompt": "森林夜晚", "full_prompt": "" },
      "audio": {},
      "effects": {}
    },
    {
      "scene_id": "s03",
      "sequence": 3,
      "title": "解决",
      "story_function": "解决",
      "narration": "小月亮终于照亮了森林。",
      "subtitle": "小月亮终于照亮了森林。",
      "duration_hint_sec": 3,
      "characters": ["c01"],
      "objects": [],
      "environment": {},
      "visual": {},
      "prompt": { "subject_prompt": "小月亮", "scene_prompt": "森林夜晚", "full_prompt": "" },
      "audio": {},
      "effects": {}
    }
  ]
}`)
	validation, scenes := validateStoryboard(raw, "pv_test", "2026-06-30T00:00:00Z")
	if !validation.Valid {
		t.Fatalf("expected valid storyboard, got errors: %v", validation.Errors)
	}
	if len(scenes) != 3 {
		t.Fatalf("scenes len = %d, want 3", len(scenes))
	}

	invalid := strings.Replace(string(raw), `"cycle_count": 3`, `"cycle_count": 5`, 1)
	validation, _ = validateStoryboard([]byte(invalid), "pv_test", "2026-06-30T00:00:00Z")
	if validation.Valid {
		t.Fatal("expected mismatched cycle count to be invalid")
	}
}

func TestBuildDeterministicKeyframes(t *testing.T) {
	scene := map[string]any{
		"scene_id":  "s01",
		"narration": "小月亮先躲在云后，后来轻轻探出头，最后照亮森林。",
		"characters": []any{
			"c01",
		},
		"prompt": map[string]any{
			"subject_prompt": "圆脸小月亮，银白柔光",
			"scene_prompt":   "蓝绿色森林夜景",
		},
		"visual": map[string]any{
			"shot_type":     "中景",
			"camera_motion": "缓慢推进",
			"composition":   "小月亮在画面上方，森林在下方",
			"action":        "小月亮慢慢变勇敢",
		},
	}
	keyframes := buildDeterministicKeyframes(scene, 3, "16:9")
	if len(keyframes) != 3 {
		t.Fatalf("keyframes len = %d, want 3", len(keyframes))
	}
	first, ok := keyframes[0].(map[string]any)
	if !ok {
		t.Fatal("keyframe should be an object")
	}
	if first["frame_id"] != "s01_f01" {
		t.Fatalf("frame_id = %v, want s01_f01", first["frame_id"])
	}
	prompt, ok := first["prompt"].(map[string]any)
	if !ok || !strings.Contains(prompt["scene_prompt"].(string), "画面比例 16:9") {
		t.Fatalf("keyframe prompt missing aspect ratio: %#v", prompt)
	}
}

func TestRunFFmpegSceneCompose_BurnsASSSubtitle(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found")
	}

	tmp := t.TempDir()
	imagePath := filepath.Join(tmp, "scene.png")
	audioPath := filepath.Join(tmp, "scene.wav")
	srtPath := filepath.Join(tmp, "scene.srt")
	assPath := filepath.Join(tmp, "scene.ass")
	outputPath := filepath.Join(tmp, "scene.mp4")

	run := func(args ...string) {
		cmd := exec.Command(ffmpegPath, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg %v failed: %v\n%s", args, err, strings.TrimSpace(string(out)))
		}
	}
	run("-y", "-f", "lavfi", "-i", "color=c=#203040:s=720x1280", "-frames:v", "1", imagePath)
	run("-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "pcm_s16le", audioPath)
	if err := writeSceneSubtitleFiles(srtPath, assPath, "小兔子开始讲故事，森林亮了起来。", 1000); err != nil {
		t.Fatalf("write subtitle failed: %v", err)
	}

	scene := sceneFile{
		ImageLocalPath:       imagePath,
		AudioLocalPath:       audioPath,
		SubtitleLocalPath:    srtPath,
		SubtitleASSLocalPath: assPath,
		AudioDurationMs:      1000,
		SceneDurationMs:      1000,
	}
	if err := runFFmpegSceneCompose(ffmpegPath, scene, outputPath, 720, 1280, 24); err != nil {
		t.Fatalf("runFFmpegSceneCompose failed: %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("output mp4 missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output mp4 is empty")
	}
}
