package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"testing"

	"github.com/spf13/cobra"
)

func transferTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return cmd
}

func TestBinaryDownloadSafetyAndAtomicPublication(t *testing.T) {
	for _, mode := range []string{"success", "truncated", "oversize", "chunked-oversize", "json", "error", "destination-race"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "safe.zip")
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Error("export method/key")
				}
				w.Header().Set("Content-Type", "application/zip")
				w.Header().Set("Content-Disposition", `attachment; filename="../../unsafe.zip"`)
				switch mode {
				case "truncated":
					w.Header().Set("Content-Length", "100")
					fmt.Fprint(w, "short")
				case "oversize":
					w.Header().Set("Content-Length", "1000")
					w.WriteHeader(200)
				case "chunked-oversize":
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					fmt.Fprint(w, strings.Repeat("x", 200))
				case "json":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"data":"not ZIP"}`)
				case "error":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(409)
					fmt.Fprint(w, `{"error":{"code":"archive_conflict","message":"test-token conflict"}}`)
				default:
					if mode == "destination-race" {
						os.WriteFile(output, []byte("existing user data"), 0600)
					}
					fmt.Fprint(w, "PK binary fixture")
				}
			}))
			defer ts.Close()
			c := &apiClient{endpoint: ts.URL, token: "test-token", project: t.TempDir()}
			result, err := c.download(transferTestCommand(), http.MethodPost, "/project/export", output, "application/zip", 128)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				data, _ := os.ReadFile(output)
				if string(data) != "PK binary fixture" || result["bytes"] != int64(len(data)) || result["sha256"] == "" {
					t.Fatal("invalid binary receipt", result)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe/incomplete download succeeded")
				}
				if mode == "destination-race" {
					data, _ := os.ReadFile(output)
					if string(data) != "existing user data" {
						t.Fatal("overwrote raced destination")
					}
				} else if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("published failed download")
				}
				if mode == "error" {
					var remote *remoteError
					if !errors.As(err, &remote) || remote.Code != "archive_conflict" || strings.Contains(remote.Message, "test-token") {
						t.Fatal("lost error code or leaked token", err)
					}
				}
			}
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".rps-") {
					t.Fatal("staging file leaked")
				}
			}
		})
	}
}

func TestTransfersRejectUnsafeLocalFilesBeforeHTTP(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("unsafe file reached HTTP") }))
	defer ts.Close()
	project := t.TempDir()
	c := &apiClient{endpoint: ts.URL, project: project}
	dir := t.TempDir()
	ordinary := filepath.Join(dir, "original.bin")
	os.WriteFile(ordinary, []byte("unchanged"), 0600)
	for _, output := range []string{"", "-", ordinary, filepath.Join(project, "history.jsonl")} {
		if _, err := c.download(transferTestCommand(), http.MethodGet, "/media/med_x", output, "*/*", 128); err == nil {
			t.Fatal("unsafe output accepted", output)
		}
	}
	link := filepath.Join(dir, "link.bin")
	if err := os.Symlink(ordinary, link); err == nil {
		if _, err := c.download(transferTestCommand(), http.MethodGet, "/media/med_x", link, "*/*", 128); err == nil {
			t.Fatal("output symlink accepted")
		}
		if _, err := c.upload(transferTestCommand(), "/media", link, nil, 128); err == nil {
			t.Fatal("input symlink accepted")
		}
	}
	large := filepath.Join(dir, "large.bin")
	f, _ := os.Create(large)
	f.Truncate(129)
	f.Close()
	empty := filepath.Join(dir, "empty.bin")
	os.WriteFile(empty, nil, 0600)
	for _, file := range []string{dir, large, empty} {
		if _, err := c.upload(transferTestCommand(), "/media", file, nil, 128); err == nil {
			t.Fatal("invalid source accepted", file)
		}
	}
	if calls != 0 {
		t.Fatal("unsafe transfer made an HTTP request")
	}
}

func TestBinaryTransferExceedsOldJSONResponseLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", fmt.Sprint(maxResponseSize+1))
		chunk := bytes.Repeat([]byte{'z'}, 64<<10)
		for n := 0; n < maxResponseSize; n += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
		w.Write([]byte{'z'})
	}))
	defer ts.Close()
	c := &apiClient{endpoint: ts.URL, project: t.TempDir()}
	output := filepath.Join(t.TempDir(), "large.zip")
	result, err := c.download(transferTestCommand(), http.MethodGet, "/worldbook/export", output, "application/zip", maxArchiveBytes)
	if err != nil {
		t.Fatal(err)
	}
	if result["bytes"] != int64(maxResponseSize+1) {
		t.Fatal(result)
	}
}

func TestUploadDoesNotSpoolIntoWorkspace(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(file, []byte("attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	t.Setenv("TMPDIR", project)
	t.Setenv("TEMP", project)
	t.Setenv("TMP", project)
	c := &apiClient{project: project, endpoint: "http://127.0.0.1:1"}
	if _, err := c.upload(transferTestCommand(), "/media", file, nil, 128); err == nil || !strings.Contains(err.Error(), "outside the active Workspace") {
		t.Fatal("upload did not reject Workspace temp directory", err)
	}
	entries, err := os.ReadDir(project)
	if err != nil || len(entries) != 0 {
		t.Fatal("upload wrote into Workspace", err)
	}
}

func TestUploadAcceptsRelativeExternalTempDirectory(t *testing.T) {
	dir, project := t.TempDir(), t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir("spool", 0700); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, "spool")
	}
	if err := os.WriteFile("note.txt", []byte("attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"id":"attachment"}}`)
	}))
	defer ts.Close()
	c := &apiClient{endpoint: ts.URL, token: "test-token", project: project}
	if _, err := c.upload(transferTestCommand(), "/media", "note.txt", nil, 128); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("spool")
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary upload not cleaned: %v %v", entries, err)
	}
}
