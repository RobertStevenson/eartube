package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/RobertStevenson/eartube/internal/store"
)

// Formats are the export formats Export accepts.
var Formats = []string{"json", "markdown"}

// exported is one video in the JSON export. Field names are part of the
// file format people may keep, so they stay stable.
type exported struct {
	ID          string   `json:"id"`
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Channel     string   `json:"channel"`
	ChannelURL  string   `json:"channel_url,omitempty"`
	UploadDate  string   `json:"upload_date,omitempty"`
	Duration    float64  `json:"duration_seconds,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Thumbnail   string   `json:"thumbnail_file,omitempty"`
	Playlists   []string `json:"playlists,omitempty"`
	Available   bool     `json:"available"`
	GoneSince   string   `json:"unavailable_since,omitempty"`
	FirstSeen   string   `json:"first_seen,omitempty"`
	DetailsSave string   `json:"details_saved,omitempty"`
}

func date(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(DateFormat)
}

// Export writes videos in the given format ("json" or "markdown"). With
// onlyGone, only videos that are no longer available are written.
func Export(w io.Writer, videos []store.Video, format string, onlyGone bool) error {
	var vs []store.Video
	for _, v := range videos {
		if !onlyGone || !v.GoneAt.IsZero() {
			vs = append(vs, v)
		}
	}
	switch format {
	case "json":
		return exportJSON(w, vs)
	case "markdown", "md":
		return exportMarkdown(w, vs)
	}
	return fmt.Errorf("unknown format %q (use %s)", format, strings.Join(Formats, " or "))
}

func exportJSON(w io.Writer, vs []store.Video) error {
	out := make([]exported, 0, len(vs))
	for _, v := range vs {
		out = append(out, exported{
			ID: v.ID, URL: URL(v.ID), Title: v.Title, Channel: v.Channel, ChannelURL: v.ChannelURL,
			UploadDate: v.UploadDate, Duration: v.Duration, Description: v.Description,
			Tags: v.Tags, Thumbnail: v.Thumbnail, Playlists: v.Playlists,
			Available: v.GoneAt.IsZero(), GoneSince: date(v.GoneAt),
			FirstSeen: date(v.FirstSeen), DetailsSave: date(v.DetailsAt),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func exportMarkdown(w io.Writer, vs []store.Video) error {
	var b strings.Builder
	gone := 0
	for _, v := range vs {
		if !v.GoneAt.IsZero() {
			gone++
		}
	}
	fmt.Fprintf(&b, "# eartube archive\n\n%d videos, %d no longer available.\n", len(vs), gone)
	for _, v := range vs {
		title := oneLine(v.Title)
		if title == "" {
			title = "(title never saved)"
		}
		fmt.Fprintf(&b, "\n## %s\n\n", title)
		for _, l := range Lines(v) {
			fmt.Fprintf(&b, "- **%s:** %s\n", l[0], oneLine(l[1]))
		}
		if v.Description != "" {
			b.WriteString("\n")
			for _, line := range strings.Split(strings.TrimRight(v.Description, "\n"), "\n") {
				b.WriteString(">")
				if line != "" {
					b.WriteString(" " + line)
				}
				b.WriteString("\n")
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Lines are a video's saved fields as label/value pairs, in display order,
// leaving out the description and anything unknown. The UI's details view
// and the Markdown export share them.
func Lines(v store.Video) [][2]string {
	var out [][2]string
	add := func(k, val string) {
		if val != "" {
			out = append(out, [2]string{k, val})
		}
	}
	status := "available"
	if !v.GoneAt.IsZero() {
		status = "unavailable since " + date(v.GoneAt) + " (private, deleted or removed)"
	}
	add("Status", status)
	add("Channel", v.Channel)
	add("Channel link", v.ChannelURL)
	add("Uploaded", v.UploadDate)
	add("Length", FmtDuration(v.Duration))
	add("Link", URL(v.ID))
	add("Playlists", strings.Join(v.Playlists, ", "))
	add("Tags", strings.Join(v.Tags, ", "))
	add("Thumbnail", v.Thumbnail)
	add("First seen", date(v.FirstSeen))
	saved := "title, channel and length only (full details not saved yet)"
	switch {
	case v.HasDetails():
		saved = "full details on " + date(v.DetailsAt)
	case v.Title == "":
		saved = "only its link and playlists: it was already gone when first seen"
	}
	add("Saved", saved)
	return out
}
