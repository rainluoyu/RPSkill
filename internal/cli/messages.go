package cli

import (
	"net/http"
	"strings"

	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"

	"github.com/spf13/cobra"
)

func emitCommand() *cobra.Command {
	o := remoteOptions{}
	var kind, file, text string
	var input int
	var mediaFile string
	var mediaIDs []string
	var characters []string

	cmd := &cobra.Command{Use: "emit", Short: "Deliver formal output for an explicitly identified input line", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if kind != "story" && kind != "assistant" {
			return usage("--kind must be story or assistant")
		}
		media, err := o.resolveMedia(cmd, mediaFile, mediaIDs)
		if err != nil {
			return err
		}
		content, err := readText(cmd, file, text)
		if err != nil {
			return err
		}
		if strings.TrimSpace(content) == "" && len(media) == 0 {
			return usage("provide --text, --file or --media-file")
		}
		c, err := o.client()
		if err != nil {
			return err
		}
		if input < 1 {
			return usage("--input is required; use the notification or read current history")
		}
		path, err := o.linePath()
		if err != nil {
			return err
		}
		body := map[string]any{"input": input, "kind": kind, "text": content, "media": media}
		if len(characters) > 0 {
			body["characters"] = characters
		}
		out, err := c.do(cmd, http.MethodPost, path+"/outputs", body)
		if err != nil {
			return err
		}
		return printResponse(cmd, out, o.json)
	}}
	o.flags(cmd, true)
	cmd.Flags().IntVar(&input, "input", 0, "1-based input line from the notification")
	cmd.Flags().StringArrayVar(&characters, "character", nil, "Character folder name appearing in this output; repeat for each character")
	cmd.Flags().StringVar(&kind, "kind", "", "Required: story or assistant (Server validates input identity)")
	textFlags(cmd, &file, &text)
	cmd.Flags().StringVar(&mediaFile, "media-file", "", "JSON array of complete existing media references")
	cmd.Flags().StringArrayVar(&mediaIDs, "media", nil, "Existing visible timeline media ID; repeat for multiple resources")
	return cmd
}
func inputCommand() *cobra.Command {
	o := remoteOptions{}
	var speaker, file, text string
	var mediaFile string
	var mediaIDs []string
	cmd := &cobra.Command{Use: "input", Short: "Submit an original user prompt to a running timeline", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := validID(speaker); err != nil {
			return err
		}
		if speaker == "story" || speaker == "assistant" || speaker == "narrator" || speaker == "environment" {
			return usage("output types are not input identities")
		}
		media, err := o.resolveMedia(cmd, mediaFile, mediaIDs)
		if err != nil {
			return err
		}
		content, err := readText(cmd, file, text)
		if err != nil {
			return err
		}
		if strings.TrimSpace(content) == "" && len(media) == 0 {
			return usage("provide --text, --file or --media-file")
		}
		p, err := o.linePath()
		if err != nil {
			return err
		}
		return o.call(cmd, http.MethodPost, p+"/inputs", map[string]any{"speaker": speaker, "text": content, "media": media})
	}}
	o.flags(cmd, true)
	cmd.Flags().StringVar(&speaker, "speaker", "", "chat, director or ordinary character ID")
	textFlags(cmd, &file, &text)
	cmd.Flags().StringVar(&mediaFile, "media-file", "", "JSON array of complete existing media references")
	cmd.Flags().StringArrayVar(&mediaIDs, "media", nil, "Existing visible timeline media ID; repeat for multiple resources")
	return cmd
}
func textFlags(cmd *cobra.Command, file, text *string) {
	cmd.Flags().StringVar(file, "file", "", "Read UTF-8 text from a regular file, or - for stdin (max 4MiB)")
	cmd.Flags().StringVar(text, "text", "", "Original UTF-8 text (not trimmed)")
}
func readMedia(file string) ([]timeline.MediaReference, error) {
	refs := []timeline.MediaReference{}
	if file == "" {
		return refs, nil
	}
	raw, err := readLocalFile(file, maxTextSize)
	if err != nil {
		return nil, err
	}
	if err := storage.DecodeJSON(raw, &refs); err != nil {
		return nil, usage("invalid media reference array: %v", err)
	}
	if refs == nil {
		return nil, usage("media references must be a JSON array")
	}
	return refs, nil
}
