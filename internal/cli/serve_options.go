package cli

import (
	"os"
	"os/exec"
	"strings"

	"github.com/luoyu/rpskill/internal/callback"
)

type serveOptions struct {
	codexExecutable string
	skillDirectory  string
	ownerThread     string
	dispatchCommand string
	dispatchArgs    []string
}

func (o serveOptions) dispatcher(provider callback.Provider) (callback.Dispatcher, error) {
	if !provider.Valid() {
		return nil, usage("only the Codex launcher is supported")
	}
	if o.dispatchCommand != "" {
		path, err := exec.LookPath(o.dispatchCommand)
		if err != nil {
			return nil, err
		}
		return callback.Command{Executable: path, Args: o.dispatchArgs}, nil
	}
	if len(o.dispatchArgs) > 0 {
		return nil, usage("--dispatch-arg requires --dispatch-command")
	}
	owner := o.ownerThread
	if owner == "" {
		owner = os.Getenv("CODEX_THREAD_ID")
	}
	if strings.TrimSpace(owner) == "" {
		return nil, usage("start from the owning Codex conversation (CODEX_THREAD_ID), or specify --owner-thread explicitly")
	}
	path, err := exec.LookPath(o.codexExecutable)
	if err != nil {
		return nil, err
	}
	return callback.Codex{Executable: path, Thread: owner}, nil
}
