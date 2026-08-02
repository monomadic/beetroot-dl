package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/grafov/m3u8"
	"github.com/vbauerster/mpb/v8"
)

type StreamKey struct {
	Value []byte
	IV    []byte
}

func (app *application) getStreamSegments(stream string) ([]string, *StreamKey, error) {
	resp, err := app.get(stream)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}
	playlist, _, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return nil, nil, err
	}
	u, err := url.Parse(stream)
	if err != nil {
		return nil, nil, err
	}
	base := u.Scheme + "://" + u.Host + path.Dir(u.Path) + "/"
	media := playlist.(*m3u8.MediaPlaylist)
	var segments []string
	var streamKey StreamKey
	for i, segment := range media.Segments {
		if segment == nil {
			break
		}
		if i == 0 {
			if segment.Key == nil {
				return nil, nil, errors.New("stream playlist has no encryption key")
			}
			req, err := app.get(base + segment.Key.URI)
			if err != nil {
				return nil, nil, err
			}
			defer req.Body.Close()
			if req.StatusCode != http.StatusOK {
				return nil, nil, fmt.Errorf("get stream key failed with status code: %d", req.StatusCode)
			}
			keyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("read stream key: %v", err)
			}
			ivBytes, err := hex.DecodeString(strings.TrimPrefix(segment.Key.IV, "0x"))
			if err != nil {
				return nil, nil, fmt.Errorf("decode stream iv: %v", err)
			}
			streamKey.Value = keyBytes
			streamKey.IV = ivBytes
		}
		segments = append(segments, base+segment.URI)
	}

	return segments, &streamKey, nil
}

func decryptSegment(segment []byte, key StreamKey) ([]byte, error) {
	block, err := aes.NewCipher(key.Value)
	if err != nil {
		return nil, err
	}
	// CryptBlocks panics on a short or misaligned input, and the PKCS#7
	// padding byte is attacker-controlled data from the CDN — validate both
	// rather than letting a malformed segment take the process down.
	if len(segment) == 0 || len(segment)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("segment length %d is not a multiple of the %d-byte block size",
			len(segment), block.BlockSize())
	}
	if len(key.IV) != block.BlockSize() {
		return nil, fmt.Errorf("iv length %d does not match the %d-byte block size",
			len(key.IV), block.BlockSize())
	}
	cbc := cipher.NewCBCDecrypter(block, key.IV)
	decrypted := make([]byte, len(segment))
	cbc.CryptBlocks(decrypted, segment)
	padding := int(decrypted[len(decrypted)-1])
	if padding == 0 || padding > len(decrypted) {
		return nil, fmt.Errorf("invalid padding length %d for a %d-byte segment", padding, len(decrypted))
	}
	return decrypted[:len(decrypted)-padding], nil
}

func (app *application) downloadSegments(path string, segmentUrls []string, key StreamKey, pbPrefix string) (string, error) {
	tempFileName := uuid.New().String()
	path = filepath.Join(path, tempFileName)
	// 0644, not 0755: this is a data file, it never needs to be executable.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	// Every failure below returns the path alongside the error so the caller's
	// deferred os.Remove actually deletes the partial file — returning "" left
	// it behind in the downloads directory.
	defer file.Close()

	var bar *mpb.Bar
	var start time.Time
	if pbPrefix != "" {
		start = time.Now()
		total := len(segmentUrls)
		bar = app.pbp.AddBar(int64(total), ProgressBarOptions(pbPrefix)...)
	}

	for _, segmentUrl := range segmentUrls {
		if err := func() error {
			resp, err := app.get(segmentUrl)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return errors.New(resp.Status)
			}
			segBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			decSegBytes, err := decryptSegment(segBytes, key)
			if err != nil {
				return err
			}
			if _, err := file.Write(decSegBytes); err != nil {
				return err
			}
			return nil
		}(); err != nil {
			return path, err
		}
		if bar != nil {
			bar.EwmaIncrInt64(1, time.Since(start))
		}
	}
	if err := file.Close(); err != nil {
		return path, err
	}
	return path, nil
}

func remuxToM4A(input, output string) error {
	cmd := exec.Command("ffmpeg",
		"-i", input,
		"-map_metadata", "-1",
		"-c:a", "copy",
		output,
	)
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}
