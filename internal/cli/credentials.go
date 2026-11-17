package cli

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/luoyu/rpskill/internal/server"
	"github.com/luoyu/rpskill/internal/storage"
)

func (o remoteOptions) client() (*apiClient, error) {
	if o.connection != "" {
		data, err := readLocalFile(o.connection, 64<<10)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(o.connection)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("connection file must be private (0600)")
		}
		var c server.Connection
		if err = storage.DecodeJSON(data, &c); err != nil {
			return nil, err
		}
		if err = localEndpoint(c.URL); err != nil {
			return nil, err
		}
		if len(c.Token) < 32 {
			return nil, errors.New("invalid connection token")
		}
		if o.endpoint != "" || o.tokenFile != "" {
			return nil, usage("do not combine --connection with --server or --token-file")
		}
		if o.project != "" {
			p, err := server.CanonicalProject(o.project)
			if err != nil {
				return nil, err
			}
			if p != c.Project {
				return nil, errors.New("connection project mismatch")
			}
		}
		p, err := server.CanonicalProject(c.Project)
		if err != nil {
			return nil, err
		}
		if p != c.Project {
			return nil, errors.New("connection project must be canonical")
		}
		return &apiClient{endpoint: c.URL, token: c.Token, project: c.Project}, nil
	}
	if o.project == "" {
		return nil, usage("--project is required")
	}
	endpoint := o.endpoint
	if endpoint == "" {
		endpoint = os.Getenv("RPS_SERVER_URL")
	}
	token := os.Getenv("RPS_SERVER_TOKEN")
	if o.tokenFile != "" {
		data, err := readLocalFile(o.tokenFile, 4096)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(o.tokenFile)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("token file must be private (0600)")
		}
		token = strings.TrimSpace(string(data))
	}
	if err := localEndpoint(endpoint); err != nil {
		return nil, fmt.Errorf("specify --server or RPS_SERVER_URL for this run: %w", err)
	}
	if len(token) < 32 {
		return nil, errors.New("specify --token-file or RPS_SERVER_TOKEN for this run")
	}
	project, err := server.CanonicalProject(o.project)
	if err != nil {
		return nil, err
	}
	return &apiClient{endpoint: endpoint, token: token, project: project}, nil
}
