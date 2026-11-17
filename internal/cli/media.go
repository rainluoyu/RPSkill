package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/luoyu/rpskill/internal/timeline"
	"github.com/spf13/cobra"
)

func mediaCommand() *cobra.Command {
	o := &remoteOptions{}
	var character string
	group := &cobra.Command{Use: "media", Short: "Upload, inspect and download durable media through the running Server"}
	o.flags(group, true)
	group.PersistentFlags().StringVar(&character, "character", "", "Character media scope instead of a timeline")
	group.AddCommand(&cobra.Command{Use: "list", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := o.client()
		if err != nil {
			return err
		}
		path, err := mediaListPath(*o, character, c)
		if err != nil {
			return err
		}
		raw, err := c.do(cmd, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		return printResponse(cmd, raw, o.json)
	}})
	var file, expression string
	var avatar bool
	upload := &cobra.Command{Use: "upload --file FILE [--chat ID | --character ID]", Short: "Upload an attachment, avatar or expression (max 32MiB)", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if file == "" {
			return usage("--file is required")
		}
		if character != "" && o.timeline != "" {
			return usage("use --chat or --character, not both")
		}
		if avatar && expression != "" {
			return usage("use --avatar or --expression, not both")
		}
		if (avatar || expression != "") && character == "" {
			return usage("--avatar and --expression require --character")
		}
		fields := map[string]string{}
		if character != "" {
			if err := validID(character); err != nil {
				return err
			}
			fields["character"] = character
		} else {
			line, err := o.linePath()
			if err != nil {
				return err
			}
			fields["timeline"] = strings.TrimPrefix(line, "/chats/")
		}
		if avatar {
			fields["avatar"] = "true"
		}
		if expression != "" {
			if err := validID(expression); err != nil {
				return err
			}
			fields["expression"] = expression
		}
		c, err := o.client()
		if err != nil {
			return err
		}
		raw, err := c.upload(cmd, "/media", file, fields, maxMediaBytes)
		if err != nil {
			return err
		}
		return printResponse(cmd, raw, o.json)
	}}
	upload.Flags().StringVar(&file, "file", "", "Regular local file (max 32MiB; Server validates actual MIME)")
	upload.Flags().BoolVar(&avatar, "avatar", false, "Set this image as the character's avatar atomically")
	upload.Flags().StringVar(&expression, "expression", "", "Character expression ID; images only")
	group.AddCommand(upload)
	group.AddCommand(&cobra.Command{Use: "get ID", Short: "Read media metadata; use download for bytes", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		c, err := o.client()
		if err != nil {
			return err
		}
		path, err := mediaListPath(*o, character, c)
		if err != nil {
			return err
		}
		records, err := mediaRecords(cmd, c, path)
		if err != nil {
			return err
		}
		for _, raw := range records {
			var ref timeline.MediaReference
			if err := json.Unmarshal(raw, &ref); err != nil {
				return err
			}
			if ref.ID == args[0] {
				body, _ := json.Marshal(map[string]any{"data": json.RawMessage(raw)})
				return printResponse(cmd, body, o.json)
			}
		}
		return &remoteError{Status: 404, Code: "not_found", Message: "media is not visible in the selected scope"}
	}})
	var output string
	download := &cobra.Command{Use: "download ID --output FILE", Short: "Stream media to a new file outside the active Workspace", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		c, err := o.client()
		if err != nil {
			return err
		}
		result, err := c.download(cmd, http.MethodGet, "/media/"+args[0]+"?download=1", output, "*/*", maxMediaBytes)
		if err != nil {
			return err
		}
		return printTransfer(cmd, result, o.json)
	}}
	download.Flags().StringVar(&output, "output", "", "New destination file; never overwritten and never inside the Workspace")
	group.AddCommand(download)
	group.AddCommand(&cobra.Command{Use: "delete ID", Short: "Delete unreferenced media; Server rejects effective event/card references", Args: countArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(args[0]); err != nil {
			return err
		}
		return o.call(cmd, http.MethodDelete, "/media/"+args[0], nil)
	}})
	return group
}
func mediaListPath(o remoteOptions, character string, c *apiClient) (string, error) {
	if character != "" && o.timeline != "" {
		return "", usage("use --chat or --character, not both")
	}
	q := url.Values{}
	if character != "" {
		if err := validID(character); err != nil {
			return "", err
		}
		q.Set("character", character)
	} else if o.timeline != "" {
		line, err := o.linePath()
		if err != nil {
			return "", err
		}
		q.Set("timeline", strings.TrimPrefix(line, "/chats/"))
	}
	if len(q) == 0 {
		return "/media", nil
	}
	return "/media?" + q.Encode(), nil
}
func mediaRecords(cmd *cobra.Command, c *apiClient, path string) ([]json.RawMessage, error) {
	raw, err := c.do(cmd, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}
func (o remoteOptions) resolveMedia(cmd *cobra.Command, file string, ids []string) ([]timeline.MediaReference, error) {
	refs, err := readMedia(file)
	if err != nil || len(ids) == 0 {
		return refs, err
	}
	for _, id := range ids {
		if err := validID(id); err != nil {
			return nil, err
		}
	}
	c, err := o.client()
	if err != nil {
		return nil, err
	}
	line, err := o.linePath()
	if err != nil {
		return nil, err
	}
	records, err := mediaRecords(cmd, c, "/media?timeline="+url.QueryEscape(strings.TrimPrefix(line, "/chats/")))
	if err != nil {
		return nil, err
	}
	visible := map[string]timeline.MediaReference{}
	for _, raw := range records {
		var ref timeline.MediaReference
		if err := json.Unmarshal(raw, &ref); err != nil {
			return nil, err
		}
		visible[ref.ID] = ref
	}
	seen := map[string]timeline.MediaReference{}
	for _, ref := range refs {
		seen[ref.ID] = ref
	}
	for _, id := range ids {
		if err := validID(id); err != nil {
			return nil, err
		}
		ref, ok := visible[id]
		if !ok {
			return nil, &remoteError{Status: 404, Code: "not_found", Message: fmt.Sprintf("media %s is not visible in this timeline", id)}
		}
		if old, ok := seen[id]; ok {
			if old.Path != ref.Path || old.MIME != ref.MIME {
				return nil, errors.New("media file reference conflicts with registered media ID")
			}
			continue
		}
		refs = append(refs, ref)
		seen[id] = ref
	}
	return refs, nil
}
func printTransfer(cmd *cobra.Command, result map[string]any, asJSON bool) error {
	raw, err := json.Marshal(map[string]any{"data": result})
	if err != nil {
		return err
	}
	return printResponse(cmd, raw, asJSON)
}
