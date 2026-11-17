package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyu/rpskill/internal/character/contentio"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/spf13/cobra"
)

const maxTextSize = 4 << 20
const maxResponseSize = 32 << 20

type remoteOptions struct {
	project, timeline               string
	endpoint, tokenFile, connection string
	json                            bool
}

func (o *remoteOptions) flags(cmd *cobra.Command, withTimeline bool) {
	f := cmd.PersistentFlags()
	f.StringVar(&o.endpoint, "server", "", "This run’s loopback server URL (or RPS_SERVER_URL)")
	f.StringVar(&o.tokenFile, "token-file", "", "Read this run’s host token from a private file (or RPS_SERVER_TOKEN)")
	f.StringVar(&o.project, "project", "", "Workspace directory (or --connection project)")
	f.BoolVar(&o.json, "json", false, "Print structured API response")
	f.StringVar(&o.connection, "connection", "", "Private connection file supplied by the launching server")
	if withTimeline {
		f.StringVar(&o.timeline, "chat", "", "Target timeline ID")
	}
}
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return usage("%s does not accept positional arguments", cmd.CommandPath())
	}
	return nil
}
func countArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return usage("%s requires %d positional argument(s)", cmd.CommandPath(), n)
		}
		return nil
	}
}
func validID(s string) error {
	if !contentio.ValidName(s) {
		return usage("invalid resource ID %q", s)
	}
	return nil
}
func (o remoteOptions) linePath() (string, error) {
	id := o.timeline
	if id == "" {
		return "", usage("--chat is required")
	}

	if err := validID(id); err != nil {
		return "", err
	}
	return "/chats/" + id, nil
}

type remoteError struct {
	Status  int
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

func (e *remoteError) Error() string {
	return fmt.Sprintf("%s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

type apiClient struct {
	endpoint, token, project string
}

func localEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return errors.New("endpoint must be a plain loopback HTTP URL")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return errors.New("endpoint must use a loopback IP and explicit port")
	}
	return nil
}
func readLocalFile(name string, limit int) ([]byte, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	volume := filepath.VolumeName(absolute)
	base := volume + string(filepath.Separator)
	f, err := storage.OpenFS(base)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	relative, err := filepath.Rel(base, absolute)
	if err != nil {
		return nil, err
	}
	data, err := f.ReadFile(filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, usage("file exceeds %d byte limit", limit)
	}
	return data, nil
}
func readText(cmd *cobra.Command, file, text string) (string, error) {
	if file != "" && cmd.Flags().Changed("text") {
		return "", usage("use --text or --file, not both")
	}
	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxTextSize+1))
	} else if file != "" {
		data, err = readLocalFile(file, maxTextSize)
	} else {
		data = []byte(text)
	}
	if err != nil {
		return "", err
	}
	if len(data) > maxTextSize {
		return "", usage("text exceeds 4MiB limit")
	}
	if !utf8.Valid(data) {
		return "", usage("text must be valid UTF-8")
	}
	return string(data), nil
}
func (c *apiClient) do(cmd *cobra.Command, method, path string, payload any) ([]byte, error) {
	var data []byte
	var err error
	if payload != nil {
		data, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		if len(data) > maxResponseSize {
			return nil, usage("request exceeds size limit")
		}
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// Never automatically replay mutations after an ambiguous network failure.

	req, err := http.NewRequestWithContext(cmd.Context(), method, c.endpoint+"/api/v1"+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-RPS-Project", c.project)
	req.Header.Set("Accept", "application/json")
	if payload != nil || method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("local Server connection failed; inspect history before resubmitting")
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	resp.Body.Close()
	if readErr != nil {
		return nil, errors.New("incomplete Server response")
	}
	if len(body) > maxResponseSize {
		return nil, errors.New("Server response exceeds size limit")
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, errors.New("invalid JSON response from Server")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if envelope.Error == nil {
			return nil, &remoteError{Status: resp.StatusCode, Code: "http_error", Message: http.StatusText(resp.StatusCode)}
		}
		return nil, &remoteError{Status: resp.StatusCode, Code: envelope.Error.Code, Message: strings.ReplaceAll(envelope.Error.Message, c.token, "[redacted]"), Details: envelope.Error.Details}
	}
	if envelope.Error != nil || len(envelope.Data) == 0 {
		return nil, errors.New("Server success response is missing data")
	}
	return body, nil
}
func printResponse(cmd *cobra.Command, body []byte, asJSON bool) error {
	if asJSON {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), string(bytes.TrimSpace(body)))
		return err
	}
	var e struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, e.Data, "", "  "); err != nil {
		return err
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), pretty.String())
	return err
}
func (o remoteOptions) call(cmd *cobra.Command, method, path string, body any) error {
	c, err := o.client()
	if err != nil {
		return err
	}
	out, err := c.do(cmd, method, path, body)
	if err != nil {
		return err
	}
	return printResponse(cmd, out, o.json)
}
func parseJSON(raw []byte) (any, error) {
	var v any
	if err := storage.DecodeJSON(raw, &v); err != nil {
		return nil, usage("invalid JSON: %v", err)
	}
	return v, nil
}
func patchFromArgs(args []string, file string) (map[string]any, error) {
	if file != "" {
		if len(args) != 0 {
			return nil, usage("use KEY JSON_VALUE or --file PATCH.json")
		}
		raw, err := readLocalFile(file, maxTextSize)
		if err != nil {
			return nil, err
		}
		v, err := parseJSON(raw)
		if err != nil {
			return nil, err
		}
		m, ok := v.(map[string]any)
		if !ok || len(m) == 0 {
			return nil, usage("patch must be a nonempty JSON object")
		}
		return m, nil
	}
	if len(args) != 2 {
		return nil, usage("requires KEY JSON_VALUE or --file PATCH.json")
	}
	value, err := parseJSON([]byte(args[1]))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(args[0], ".")
	var nested any = value
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "" {
			return nil, usage("empty configuration key")
		}
		nested = map[string]any{parts[i]: nested}
	}
	return nested.(map[string]any), nil
}
