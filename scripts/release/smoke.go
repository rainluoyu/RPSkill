package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// protocolFixture is a test-only inbox bridge, never a model or Agent session.
func protocolFixture() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil || !json.Valid(raw) {
		os.Exit(2)
	}
	if err = os.WriteFile(os.Getenv("RPS_RELEASE_FIXTURE_TRACE"), raw, 0600); err != nil {
		os.Exit(2)
	}
}
func copyFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}
func smokeRelease(directory string) error {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return errors.New("release smoke test runs only a matching Linux package; other platforms require native validation")
	}
	if err := verifyRelease(directory); err != nil {
		return err
	}
	raw, err := readRegular(filepath.Join(directory, "release.json"))
	if err != nil {
		return err
	}
	var manifest releaseManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	t := target{"linux", runtime.GOARCH}
	archive := t.archive(manifest.Version)
	files, err := readArchive(filepath.Join(directory, archive), strings.TrimSuffix(archive, archiveExt(t)))
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "rpskill-release-smoke-")
	if err != nil {
		return err
	}
	// Keep private evidence for independent inspection; do not package runtime
	// descriptors or their local management credentials in the release.
	fmt.Printf("Smoke evidence: %s\n", work)
	for _, dir := range []string{"package", "bin-only", "home", "empty-path", "runtime", "skills"} {
		if err := os.Mkdir(filepath.Join(work, dir), 0700); err != nil {
			return err
		}
	}
	packageRoot := filepath.Join(work, "package")
	for _, file := range files {
		name := filepath.Join(packageRoot, filepath.FromSlash(file.name))
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(name, file.data, file.mode); err != nil {
			return err
		}
	}
	binary := filepath.Join(work, "bin-only", "rps")
	if err := copyFile(filepath.Join(packageRoot, "rps"), binary, 0755); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	peer := filepath.Join(work, "fixture")
	if err := copyFile(self, peer, 0755); err != nil {
		return err
	}
	env := []string{"PATH=" + filepath.Join(work, "empty-path"), "HOME=" + filepath.Join(work, "home"), "XDG_CONFIG_HOME=" + filepath.Join(work, "home", "config"), "RPS_RUNTIME_DIR=" + filepath.Join(work, "runtime"), "TMPDIR=" + work, "RPS_RELEASE_PROTOCOL_FIXTURE=1", "RPS_RELEASE_FIXTURE_TRACE=" + filepath.Join(work, "fixture-methods.log")}
	var checks []string
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = filepath.Join(work, "bin-only")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("rps %s: %w: %s", args[0], err, out)
		}
		return out, nil
	}
	out, err := run("version")
	if err != nil {
		return err
	}
	if string(out) != "rps "+manifest.Version+" (protocol 3)\n" {
		return fmt.Errorf("unexpected package version: %q", out)
	}
	checks = append(checks, "version")
	if _, err := run("--help"); err != nil {
		return err
	}
	checks = append(checks, "help")
	project := filepath.Join(work, "MyRP")
	if _, err := run("init", project, "--json"); err != nil {
		return err
	}
	checks = append(checks, "init")
	if _, err := run("validate", project, "--offline", "--json"); err != nil {
		return err
	}
	checks = append(checks, "offline_validate")
	if _, err := run("validate", filepath.Join(packageRoot, "examples", "harbor-academy"), "--offline", "--json"); err != nil {
		return err
	}
	checks = append(checks, "example_validate")
	if _, err := run("install-skill", "--target", filepath.Join(work, "skills"), "--json"); err != nil {
		return err
	}
	installed := filepath.Join(work, "skills", "rp")
	for _, file := range files {
		if strings.HasPrefix(file.name, "skill/rp/") {
			name := strings.TrimPrefix(file.name, "skill/rp/")
			data, err := os.ReadFile(filepath.Join(installed, filepath.FromSlash(name)))
			if err != nil {
				return err
			}
			if digest(data) != digest(file.data) {
				return fmt.Errorf("installed Skill differs from package %s", name)
			}
		}
	}
	checks = append(checks, "installed_skill_texts_and_native_binary")
	// Remove the extracted archive before serving. Only the isolated executable
	// and its privately self-installed Skill remain available to the server.
	if err := os.RemoveAll(packageRoot); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(work, "server.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "serve", project, "--callback", "codex", "--dispatch-command", peer, "--listen", "127.0.0.1:0")
	cmd.Dir = filepath.Join(work, "bin-only")
	cmd.Env = env
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	defer func() {
		if !stopped {
			cancel()
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
		}
	}()
	var descriptor struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(log.Name())
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(line, "Open: ") {
				continue
			}
			u, err := url.Parse(strings.TrimPrefix(line, "Open: "))
			if err != nil {
				return err
			}
			values, err := url.ParseQuery(u.Fragment)
			if err != nil {
				return err
			}
			descriptor.URL = u.Scheme + "://" + u.Host
			descriptor.Token = values.Get("token")
		}
		if descriptor.URL != "" {
			env = append(env, "RPS_SERVER_URL="+descriptor.URL, "RPS_SERVER_TOKEN="+descriptor.Token)
			break
		}
		select {
		case err := <-done:
			stopped = true
			return fmt.Errorf("fixture serve stopped before discovery: %v; see %s/server.log", err, work)
		default:
		}
		if time.Now().After(deadline) {
			return errors.New("fixture Server did not publish discovery")
		}
		time.Sleep(20 * time.Millisecond)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	get := func(route string, auth bool) ([]byte, error) {
		req, err := http.NewRequest(http.MethodGet, descriptor.URL+route, nil)
		if err != nil {
			return nil, err
		}
		if auth {
			req.Header.Set("Authorization", "Bearer "+descriptor.Token)
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s returned %d", route, res.StatusCode)
		}
		return io.ReadAll(io.LimitReader(res.Body, 8<<20))
	}
	html, err := get("/", false)
	if err != nil {
		return err
	}
	if !strings.Contains(string(html), "id=\"app\"") {
		return errors.New("embedded Vue root missing")
	}
	assetRe := regexp.MustCompile(`(?:src|href)="\.?(/assets/[^"?]+)"`)
	matches := assetRe.FindAllSubmatch(html, -1)
	if len(matches) < 2 {
		return errors.New("embedded frontend JS/CSS assets missing")
	}
	for _, m := range matches {
		data, err := get(string(m[1]), false)
		if err != nil {
			return err
		}
		if len(data) == 0 {
			return errors.New("empty embedded frontend asset")
		}
	}
	checks = append(checks, "embedded_vue_html_js_css_without_source_or_node")
	status, err := get("/api/v1/status", true)
	if err != nil {
		return err
	}
	var state struct {
		Data struct {
			Version, Phase string
			Agent          struct{ State string }
		}
	}
	if err := json.Unmarshal(status, &state); err != nil {
		return err
	}
	if state.Data.Version != manifest.Version || state.Data.Phase != "v1.0-candidate" {
		return errors.New("Server version/phase mismatch")
	}
	if _, err := run("status", "--project", project, "--json"); err != nil {
		return err
	}
	if _, err := run("validate", "--project", project, "--online", "--json"); err != nil {
		return err
	}
	checks = append(checks, "authenticated_local_status_validate_without_model")
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			return err
		}
	case <-time.After(8 * time.Second):
		return errors.New("Server failed graceful shutdown")
	}
	if _, err := os.Stat(filepath.Join(work, "fixture-methods.log")); !os.IsNotExist(err) {
		return errors.New("startup unexpectedly invoked launcher inbox")
	}
	checks = append(checks, "zero_sessions_zero_prompts_no_provider_authentication", "graceful_shutdown")
	binaryHash, err := fileDigest(binary)
	if err != nil {
		return err
	}
	result := map[string]any{"version": manifest.Version, "target": t.key(), "binary_sha256": binaryHash, "checks": checks, "runtime_path": filepath.Join(work, "runtime"), "evidence_only": "protocol fixture and HTTP asset smoke; not a browser, model or native macOS/Windows acceptance"}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "smoke.json"), append(encoded, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("Linux isolated smoke PASS: %d checks; evidence %s\n", len(checks), work)
	return nil
}
