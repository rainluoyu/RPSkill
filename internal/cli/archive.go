package cli

import (
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func projectCommand() *cobra.Command {
	o := &remoteOptions{}
	group := &cobra.Command{Use: "project", Short: "Import/export a portable Workspace through the running Server"}
	o.flags(group, false)
	group.AddCommand(archiveCommands(o, "project")...)
	return group
}
func archiveCommands(o *remoteOptions, kind string) []*cobra.Command {
	var output, category string
	use, nargs := "export --output ARCHIVE.zip", 0
	if kind == "chats" || kind == "characters" {
		use, nargs = "export ID --output ARCHIVE.zip", 1
	}
	export := &cobra.Command{Use: use, Short: "Stream a portable ZIP to a new external file (no overwrite)", Args: countArgs(nargs), RunE: func(cmd *cobra.Command, args []string) error {
		path, method := "/"+kind+"/export", http.MethodGet
		if nargs == 1 {
			if err := validID(args[0]); err != nil {
				return err
			}
			path = "/" + kind + "/" + args[0] + "/export"
		}
		if kind == "project" || kind == "chats" {
			method = http.MethodPost
		}
		if category != "" {
			if err := validID(category); err != nil {
				return err
			}
			path += "?category=" + url.QueryEscape(category)
		}
		c, err := o.client()
		if err != nil {
			return err
		}

		result, err := c.download(cmd, method, path, output, "application/zip", maxArchiveBytes)
		if err != nil {
			return err
		}
		return printTransfer(cmd, result, o.json)
	}}
	export.Flags().StringVar(&output, "output", "", "New ZIP file outside the active Workspace; never overwritten")
	if kind == "worldbook" {
		export.Flags().StringVar(&category, "category", "", "Export only this worldbook category")
	}
	var file string
	importCmd := &cobra.Command{Use: "import --file ARCHIVE.zip", Short: "Validate/import ZIP through Server; no ID renaming or silent overwrite", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if file == "" {
			return usage("--file is required")
		}
		c, err := o.client()
		if err != nil {
			return err
		}

		raw, err := c.upload(cmd, "/"+kind+"/import", file, nil, maxArchiveBytes)
		if err != nil {
			return err
		}
		return printResponse(cmd, raw, o.json)
	}}
	if kind == "project" {
		importCmd.Long = "Import into a freshly initialized empty Workspace only. The Server stages, validates and commits the archive; existing content is never overwritten."
	}
	importCmd.Flags().StringVar(&file, "file", "", "Regular ZIP file, compressed max 64MiB; Server validates paths, CRC and expanded limits")
	return []*cobra.Command{export, importCmd}
}
