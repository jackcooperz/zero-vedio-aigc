package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
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
