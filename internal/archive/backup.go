package archive

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RobertStevenson/eartube/internal/store"
)

// Backup writes a copy of videos that are about to be deleted into a new
// folder under dir: videos.json, videos.md and their thumbnails. It copies the
// thumbnails rather than moving them, so if anything fails nothing has been
// lost; the caller removes the originals (RemoveThumbnails) once the videos
// are deleted. It returns the folder.
func Backup(dir, playlistTitle string, videos []store.Video, now time.Time) (string, error) {
	folder := filepath.Join(dir, now.Format("2006-01-02-150405")+"-"+slug(playlistTitle))
	if err := os.MkdirAll(filepath.Join(folder, "thumbnails"), 0o755); err != nil {
		return "", err
	}
	vs := make([]store.Video, len(videos))
	for i, v := range videos {
		if v.Thumbnail != "" {
			dst := filepath.Join(folder, "thumbnails", filepath.Base(v.Thumbnail))
			switch err := copyFile(v.Thumbnail, dst); {
			case err == nil:
				v.Thumbnail = dst
			case os.IsNotExist(err):
				v.Thumbnail = "" // already missing: nothing to keep
			default:
				return "", err
			}
		}
		vs[i] = v
	}
	for _, f := range []struct{ name, format string }{{"videos.json", "json"}, {"videos.md", "markdown"}} {
		var b bytes.Buffer
		if err := Export(&b, vs, f.format, false); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(folder, f.name), b.Bytes(), 0o644); err != nil {
			return "", err
		}
	}
	return folder, nil
}

// RemoveThumbnails deletes the saved thumbnail images of videos that were
// deleted. Missing files are not an error.
func RemoveThumbnails(videos []store.Video) error {
	var first error
	for _, v := range videos {
		if v.Thumbnail == "" {
			continue
		}
		if err := os.Remove(v.Thumbnail); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	return first
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug turns a playlist title into a safe folder-name part.
func slug(title string) string {
	s := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		s = "playlist"
	}
	return s
}
