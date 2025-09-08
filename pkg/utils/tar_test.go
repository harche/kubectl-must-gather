package utils

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
	"time"
)

func TestWriteFileToTar(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		data     []byte
		expected string
	}{
		{
			name:     "simple file",
			path:     "test.txt",
			data:     []byte("hello world"),
			expected: "hello world",
		},
		{
			name:     "empty file",
			path:     "empty.txt",
			data:     []byte{},
			expected: "",
		},
		{
			name:     "file with path",
			path:     "dir/subdir/file.txt",
			data:     []byte("content"),
			expected: "content",
		},
		{
			name:     "binary data",
			path:     "binary.dat",
			data:     []byte{0x00, 0x01, 0x02, 0xFF},
			expected: string([]byte{0x00, 0x01, 0x02, 0xFF}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)

			err := WriteFileToTar(tw, tt.path, tt.data)
			if err != nil {
				t.Fatalf("WriteFileToTar failed: %v", err)
			}

			err = tw.Close()
			if err != nil {
				t.Fatalf("Failed to close tar writer: %v", err)
			}

			// Read back the tar and verify
			tr := tar.NewReader(&buf)
			header, err := tr.Next()
			if err != nil {
				t.Fatalf("Failed to read tar header: %v", err)
			}

			if header.Name != tt.path {
				t.Errorf("expected path %q, got %q", tt.path, header.Name)
			}

			if header.Size != int64(len(tt.data)) {
				t.Errorf("expected size %d, got %d", len(tt.data), header.Size)
			}

			if header.Mode != 0644 {
				t.Errorf("expected mode 0644, got %o", header.Mode)
			}

			// Verify the content
			content, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("Failed to read tar content: %v", err)
			}

			if string(content) != tt.expected {
				t.Errorf("expected content %q, got %q", tt.expected, string(content))
			}

			// Verify timestamp is recent (within last minute)
			if time.Since(header.ModTime) > time.Minute {
				t.Errorf("timestamp seems too old: %v", header.ModTime)
			}
		})
	}
}

func TestWriteStreamToTar(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		content  string
		expected string
	}{
		{
			name:     "simple stream",
			path:     "stream.txt",
			content:  "hello from stream",
			expected: "hello from stream",
		},
		{
			name:     "empty stream",
			path:     "empty_stream.txt",
			content:  "",
			expected: "",
		},
		{
			name:     "multiline stream",
			path:     "multiline.txt",
			content:  "line1\nline2\nline3",
			expected: "line1\nline2\nline3",
		},
		{
			name:     "large stream",
			path:     "large.txt",
			content:  strings.Repeat("abcdefghij", 1000),
			expected: strings.Repeat("abcdefghij", 1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)

			reader := strings.NewReader(tt.content)
			err := WriteStreamToTar(tw, tt.path, reader)
			if err != nil {
				t.Fatalf("WriteStreamToTar failed: %v", err)
			}

			err = tw.Close()
			if err != nil {
				t.Fatalf("Failed to close tar writer: %v", err)
			}

			// Read back the tar and verify
			tr := tar.NewReader(&buf)
			header, err := tr.Next()
			if err != nil {
				t.Fatalf("Failed to read tar header: %v", err)
			}

			if header.Name != tt.path {
				t.Errorf("expected path %q, got %q", tt.path, header.Name)
			}

			if header.Size != int64(len(tt.content)) {
				t.Errorf("expected size %d, got %d", len(tt.content), header.Size)
			}

			// Verify the content
			content, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("Failed to read tar content: %v", err)
			}

			if string(content) != tt.expected {
				t.Errorf("expected content %q, got %q", tt.expected, string(content))
			}
		})
	}
}

func TestWriteStreamToTarErrorHandling(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Create a reader that will fail
	errorReader := &errorReader{}
	err := WriteStreamToTar(tw, "test.txt", errorReader)
	
	if err == nil {
		t.Error("expected error from WriteStreamToTar with failing reader")
	}

	if !strings.Contains(err.Error(), "unexpected EOF") {
		t.Errorf("expected EOF error, got %q", err.Error())
	}
}

func TestWriteFileToTarMultipleFiles(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	files := map[string][]byte{
		"file1.txt":        []byte("content1"),
		"dir/file2.txt":    []byte("content2"),
		"dir/sub/file3.txt": []byte("content3"),
	}

	// Write multiple files
	for path, data := range files {
		err := WriteFileToTar(tw, path, data)
		if err != nil {
			t.Fatalf("WriteFileToTar failed for %s: %v", path, err)
		}
	}

	err := tw.Close()
	if err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}

	// Read back and verify all files
	tr := tar.NewReader(&buf)
	foundFiles := make(map[string]string)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read tar header: %v", err)
		}

		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("Failed to read tar content for %s: %v", header.Name, err)
		}

		foundFiles[header.Name] = string(content)
	}

	if len(foundFiles) != len(files) {
		t.Errorf("expected %d files, found %d", len(files), len(foundFiles))
	}

	for path, expectedContent := range files {
		if foundContent, exists := foundFiles[path]; !exists {
			t.Errorf("file %s not found in tar", path)
		} else if foundContent != string(expectedContent) {
			t.Errorf("content mismatch for %s: expected %q, got %q", path, string(expectedContent), foundContent)
		}
	}
}

func TestTarGzipIntegration(t *testing.T) {
	// Test that our tar functions work with gzip compression
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	testData := []byte("test content for gzip integration")
	err := WriteFileToTar(tw, "test.txt", testData)
	if err != nil {
		t.Fatalf("WriteFileToTar failed: %v", err)
	}

	err = tw.Close()
	if err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}

	err = gzw.Close()
	if err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}

	// Read back the gzipped tar
	gzr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("Failed to read tar header: %v", err)
	}

	if header.Name != "test.txt" {
		t.Errorf("expected filename 'test.txt', got %q", header.Name)
	}

	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("Failed to read tar content: %v", err)
	}

	if string(content) != string(testData) {
		t.Errorf("expected content %q, got %q", string(testData), string(content))
	}
}

// errorReader is a helper that always returns an error when read
type errorReader struct{}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}