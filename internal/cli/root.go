// Package cli provides the shared Server/CLI binary commands.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luoyu/rpskill/internal/callback"
	"github.com/luoyu/rpskill/internal/server"
	"github.com/spf13/cobra"
)

type UsageError struct{ Err error }

func (e *UsageError) Error() string          { return e.Err.Error() }
func (e *UsageError) Unwrap() error          { return e.Err }
func usage(format string, args ...any) error { return &UsageError{fmt.Errorf(format, args...)} }

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var u *UsageError
	if errors.As(err, &u) {
		return 2
	}
	return 1
}

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use: "rps", Short: "RPSkill: local, file-first roleplay workspace",
		Long:         "RPSkill Server and CLI. Read RP files directly; use authenticated local HTTP commands for all online changes.",
		SilenceUsage: true, SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return &UsageError{err} })
	root.AddCommand(initCommand(), validateCommand(), serveCommand(), statusCommand(), emitCommand(), inputCommand(), timelineCommand(), characterCommand(), worldbookCommand(), configCommand(), mediaCommand(), projectCommand(), installSkillCommand(), &cobra.Command{
		Use: "version", Short: "Print version and data protocol",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return usage("version does not accept arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "rps %s (protocol %d)\n", server.Version, server.ProtocolVersion)
			return err
		},
	})
	rejectUnknownSubcommands(root)
	return root
}

// Cobra otherwise prints group help and succeeds for unknown nested commands.
func rejectUnknownSubcommands(cmd *cobra.Command) {
	if !cmd.Runnable() {
		cmd.Args = noArgs
		cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	}
	for _, child := range cmd.Commands() {
		rejectUnknownSubcommands(child)
	}
}

func serveCommand() *cobra.Command {
	var provider, address string
	options := serveOptions{}
	cmd := &cobra.Command{
		Use: "serve PROJECT --callback codex", Short: "Serve a validated Workspace with authenticated local management",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usage("serve requires one project directory and --callback codex")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !callback.Provider(provider).Valid() {
				return usage("--callback is required and must be codex; run rps serve --help")
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return usage("invalid --listen: %v", err)
			}
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return usage("--listen must use a loopback IP, for example 127.0.0.1:8717")
			}
			return serve(cmd, args[0], callback.Provider(provider), address, options)
		},
	}
	cmd.Flags().StringVar(&provider, "callback", "", "Required default callback provider: codex")
	cmd.Flags().StringVar(&address, "listen", "127.0.0.1:8717", "Loopback HTTP listen address")
	cmd.Flags().StringVar(&options.codexExecutable, "codex-executable", "codex", "Trusted local Codex executable (no shell expansion)")
	cmd.Flags().StringVar(&options.ownerThread, "owner-thread", "", "Existing launching Codex conversation; defaults to CODEX_THREAD_ID")
	cmd.Flags().StringVar(&options.dispatchCommand, "dispatch-command", "", "Trusted existing-launcher inbox bridge; reads notification JSON from stdin (no shell)")
	cmd.Flags().StringArrayVar(&options.dispatchArgs, "dispatch-arg", nil, "Argument for the inbox bridge; repeat as needed")
	cmd.Flags().StringVar(&options.skillDirectory, "skill-directory", "", "Trusted installed rp Skill directory; default: deploy bundled Skill privately")

	return cmd
}

func serve(cmd *cobra.Command, project string, provider callback.Provider, address string, options serveOptions) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	canonical, err := server.CanonicalProject(project)
	if err != nil {
		return err
	}
	source, err := os.Executable()
	if err != nil {
		return err
	}
	plan, err := planServeSkill(canonical, options.skillDirectory, source)
	if err != nil {
		return err
	}

	if plan.managed {
		defer os.RemoveAll(filepath.Dir(plan.directory))
	}
	token, err := server.NewToken()
	if err != nil {
		return err
	}
	dispatcher, err := options.dispatcher(provider)
	if err != nil {
		return err
	}
	srv, err := server.New(server.Options{Project: canonical, Token: token, Callback: provider, SkillDirectory: plan.directory, Dispatcher: dispatcher})
	if err != nil {
		return err
	}
	defer srv.Close()
	if err := plan.deploy(canonical); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	endpoint := "http://" + listener.Addr().String()
	if err := srv.SetEndpoint(endpoint); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "RPSkill %s\n%s\nOpen: %s/#token=%s\nSkill: %s\nCLI: set RPS_SERVER_URL and RPS_SERVER_TOKEN for this run. Stop with Ctrl+C.\n", server.Version, "Input → launching conversation (no managed Agent session)", endpoint, token, plan.directory)
	httpServer := &http.Server{
		Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
	}
	// No global WriteTimeout: SSE connections are intentionally long-lived.
	httpServer.BaseContext = func(net.Listener) context.Context { return ctx }
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Shutdown does not stop active SSE by itself. Closing the server after the
		// grace period forcibly releases remaining streams without persisting data.
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = httpServer.Close()
			if !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
		}
		return nil
	}
}

func statusCommand() *cobra.Command {
	o := remoteOptions{}
	cmd := &cobra.Command{Use: "status --project PROJECT", Short: "Read authenticated status from the running local Server", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := o.client()
		if err != nil {
			return err
		}
		body, err := c.do(cmd, http.MethodGet, "/status", nil)
		if err != nil {
			return err
		}
		var result struct {
			Data server.Status `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		if result.Data.Project != c.project {
			return errors.New("server project mismatch")
		}
		if o.json {
			return printResponse(cmd, body, true)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\nServer: %s\nCallback: %s (launcher inbox)\nPhase: %s\n", result.Data.Project, c.endpoint, result.Data.Callback, result.Data.Phase)
		return err
	}}
	o.flags(cmd, false)
	return cmd
}
