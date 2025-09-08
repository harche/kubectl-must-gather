package utils

import (
	"archive/tar"
	"io"
	"time"
)

func WriteFileToTar(tw *tar.Writer, path string, data []byte) error {
	hdr := &tar.Header{
		Name:    path,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func WriteStreamToTar(tw *tar.Writer, path string, r io.Reader) error {
	// Stream to a temp buffer to get size? Tar needs size up-front; so we buffer in memory for now.
	// For large outputs, consider chunk files.
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return WriteFileToTar(tw, path, buf)
}