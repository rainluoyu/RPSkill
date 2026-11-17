package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/luoyu/rpskill/internal/server"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/spf13/cobra"
)

const maxMediaBytes int64 = 32 << 20
const maxArchiveBytes int64 = 64 << 20

// Pin the source handle and reject links/special files before opening. Uploads
// then spool a bounded, immutable multipart body outside the Workspace for a
// single submission without automatic replay.
func openTransferSource(name string, limit int64) (*os.File, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	if err := rejectInstallLinks(filepath.Dir(absolute)); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	base := filepath.Base(absolute)
	if err := storage.ValidPath(base); err != nil {
		return nil, usage("invalid input filename: %v", err)
	}
	before, err := root.Lstat(base)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, usage("input must be a regular non-symlink file")
	}
	if before.Size() <= 0 || before.Size() > limit {
		return nil, usage("input must contain 1..%d bytes", limit)
	}
	file, err := root.Open(base)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		file.Close()
		return nil, errors.New("input changed while opening")
	}
	return file, nil
}

func transferClient() (*http.Client, func()) {
	t := &http.Transport{Proxy: nil}
	return &http.Client{Transport: t, Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, t.CloseIdleConnections
}
func (c *apiClient) transferRequest(cmd *cobra.Command, method, path, contentType string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(cmd.Context(), method, c.endpoint+"/api/v1"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-RPS-Project", c.project)

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}
func (c *apiClient) responseFailure(resp *http.Response) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return errors.New("incomplete or oversized Server error response")
	}
	var envelope struct {
		Error *remoteError `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Error == nil {
		return &remoteError{Status: resp.StatusCode, Code: "http_error", Message: http.StatusText(resp.StatusCode)}
	}
	envelope.Error.Status = resp.StatusCode
	envelope.Error.Message = strings.ReplaceAll(envelope.Error.Message, c.token, "[redacted]")
	return envelope.Error
}
func (c *apiClient) upload(cmd *cobra.Command, path, filename string, fields map[string]string, limit int64) ([]byte, error) {
	source, err := openTransferSource(filename, limit)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	tempDirectory, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return nil, err
	}
	tempDirectory, err = filepath.Abs(tempDirectory)
	if err != nil {
		return nil, err
	}
	if c.project != "" {
		inside, err := storage.WithinDirectory(c.project, tempDirectory)
		if err != nil {
			return nil, err
		}
		if inside {
			return nil, usage("temporary upload directory must be outside the active Workspace")
		}
	}
	spool, err := os.CreateTemp(tempDirectory, "rps-upload-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(spool.Name())
	defer spool.Close()
	form := multipart.NewWriter(spool)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := form.WriteField(k, fields[k]); err != nil {
			return nil, err
		}
	}
	part, err := form.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(part, io.LimitReader(source, limit+1))
	if err != nil {
		return nil, err
	}
	if n == 0 || n > limit {
		return nil, usage("input must contain 1..%d bytes", limit)
	}
	if err := form.Close(); err != nil {
		return nil, err
	}
	info, err := spool.Stat()
	if err != nil {
		return nil, err
	}
	client, closeClient := transferClient()
	defer closeClient()

	// Send the immutable spool once; an ambiguous response is not retried.
	body := io.NopCloser(io.NewSectionReader(spool, 0, info.Size()))
	req, err := c.transferRequest(cmd, http.MethodPost, path, form.FormDataContentType(), body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = info.Size()
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("upload response was lost; inspect media before resubmitting")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := c.responseFailure(resp)
		resp.Body.Close()
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	resp.Body.Close()
	if err != nil {
		return nil, errors.New("incomplete Server upload receipt")
	}
	if len(raw) > maxResponseSize {
		return nil, errors.New("Server upload receipt exceeds size limit")
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Data) == 0 || len(envelope.Error) > 0 {
		return nil, errors.New("invalid Server upload receipt")
	}
	return raw, nil
}

// Downloads never use a server-supplied filename. A private staging file is
// linked into place only after a complete, bounded, synced transfer. Link is
// atomic and fails if the destination already exists; no file is overwritten.
func (c *apiClient) download(cmd *cobra.Command, method, path, filename, expectedMIME string, limit int64) (map[string]any, error) {
	if filename == "" || filename == "-" {
		return nil, usage("--output must name a new file outside the Workspace (not stdout)")
	}
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	inside, err := storage.WithinDirectory(c.project, absolute)
	if err != nil {
		return nil, err
	}
	if inside {
		return nil, usage("download/export must not write into the active Workspace; use Server mutations")
	}
	if err := rejectInstallLinks(filepath.Dir(absolute)); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	base := filepath.Base(absolute)
	if err := storage.ValidPath(base); err != nil {
		return nil, usage("invalid output filename: %v", err)
	}
	if _, err := root.Lstat(base); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return nil, usage("output already exists; choose a new filename")
		}
		return nil, err
	}
	var body io.Reader
	contentType := ""
	if method != http.MethodGet {
		body = bytes.NewReader([]byte("{}"))
		contentType = "application/json"
	}
	req, err := c.transferRequest(cmd, method, path, contentType, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", expectedMIME)
	client, closeClient := transferClient()
	defer closeClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("cannot download from local Server")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.responseFailure(resp)
	}
	mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mt == "application/json" || mt == "text/html" || expectedMIME != "*/*" && mt != expectedMIME {
		return nil, errors.New("unexpected binary Content-Type from Server")
	}
	if resp.ContentLength > limit {
		return nil, errors.New("Server file exceeds size limit")
	}
	nonce, err := server.NewToken()
	if err != nil {
		return nil, err
	}
	temp := ".rps-download-" + nonce
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer root.Remove(temp)
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, limit+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return nil, errors.New("incomplete download; no destination file was published")
	}
	if n > limit || resp.ContentLength >= 0 && n != resp.ContentLength {
		return nil, errors.New("invalid download length; no destination file was published")
	}
	if err := root.Link(temp, base); err != nil {
		return nil, fmt.Errorf("publish output without overwriting: %w", err)
	}
	return map[string]any{"file": absolute, "bytes": n, "mime": mt, "sha256": hex.EncodeToString(hash.Sum(nil))}, nil
}
