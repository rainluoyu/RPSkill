package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/luoyu/rpskill/internal/project"
	"github.com/spf13/cobra"
)

func printData(cmd *cobra.Command, data any) error {
	return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
		Data any `json:"data"`
	}{data})
}
func initCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use: "init PATH", Short: "Initialize a portable RP Workspace with a main timeline",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usage("init requires one project path")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {

			store, err := project.Init(args[0], project.InitOptions{})
			if err != nil {
				return err
			}
			defer store.Close()
			config, err := store.Config()
			if err != nil {
				return err
			}
			if asJSON {
				return printData(cmd, map[string]any{"project": store.Root(), "name": config.Name, "version": config.Version})
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Initialized %s\nProject: %s\nTimeline: main\n", config.Name, store.Root())
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print a structured initialization result")
	return cmd
}
func validateCommand() *cobra.Command {
	var directory string
	var asJSON, repair, online, offline bool
	cmd := &cobra.Command{
		Use: "validate [PATH] [--project PATH]", Short: "Validate Workspace configuration, content, timelines and JSONL",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usage("validate accepts one path or --project PATH")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if directory != "" {
					return usage("use either PATH or --project, not both")
				}
				directory = args[0]
			}
			if directory == "" {
				return usage("validate requires a project path or --project PATH")
			}
			if online && offline {
				return usage("--online and --offline are mutually exclusive")
			}
			if repair && online {
				return usage("--repair-tail is offline-only")
			}

			useHTTP := online || !offline && os.Getenv("RPS_SERVER_URL") != ""
			if useHTTP {
				if repair {
					return usage("stop the Server before --repair-tail; online repair is forbidden")
				}
				opts := remoteOptions{project: directory, json: asJSON}
				c, err := opts.client()
				if err != nil {
					return err
				}
				body, err := c.do(cmd, http.MethodGet, "/validate", nil)
				if err != nil {
					return err
				}
				var result struct {
					Data project.ValidationReport `json:"data"`
				}
				if err := json.Unmarshal(body, &result); err != nil {
					return err
				}
				if err := printResponse(cmd, body, asJSON); err != nil {
					return err
				}
				if !result.Data.Valid {
					return errors.New("workspace validation failed")
				}
				return nil
			}
			var report project.ValidationReport
			var err error
			if repair {
				var store *project.Store
				store, err = project.Open(directory, project.OpenOptions{RepairTail: true})
				if err == nil {
					report, err = store.Validate()
					for _, r := range store.Recoveries() {
						fmt.Fprintf(cmd.ErrOrStderr(), "Quarantined %d uncommitted tail bytes from %s to %s\n", r.Bytes, r.File, r.Quarantine)
					}
					err = errors.Join(err, store.Close())
				}
				if err != nil {
					report = project.ValidationReport{Project: directory, Issues: []project.Issue{{Path: ".", Code: "recovery_failed", Message: err.Error()}}}
				}
			} else {
				report, err = project.Validate(directory)
			}
			if asJSON {
				if writeErr := printData(cmd, report); writeErr != nil {
					return writeErr
				}
			} else {
				for _, issue := range report.Issues {
					fmt.Fprintf(cmd.OutOrStdout(), "%s [%s] %s\n", issue.Path, issue.Code, issue.Message)
				}
				if report.Valid {
					fmt.Fprintf(cmd.OutOrStdout(), "Valid Workspace: %s (%d timelines, %d stored events)\n", report.Project, report.Timelines, report.Events)
				}
			}
			if err != nil {
				return err
			}
			if !report.Valid {
				return errors.New("workspace validation failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&directory, "project", "", "Workspace directory")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print structured validation issues and counts")
	cmd.Flags().BoolVar(&repair, "repair-tail", false, "Offline only: quarantine uncommitted JSONL tails, never interior corruption")
	cmd.Flags().BoolVar(&online, "online", false, "Require the running Server validation API")
	cmd.Flags().BoolVar(&offline, "offline", false, "Explicit offline validation (still respects writer lock)")
	return cmd
}
