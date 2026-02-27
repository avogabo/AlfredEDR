package subject

import (
	"path/filepath"
	"regexp"
	"strings"
)

var quotedRe = regexp.MustCompile(`"([^"]+)"`)
var yencTailRe = regexp.MustCompile(`(?i)\s+yenc\s*\([^)]*\)\s*$`)

func looksLikeMediaFilename(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(s))
	switch ext {
	case ".mkv", ".mp4", ".avi", ".m4v", ".mov", ".ts", ".m2ts", ".wmv", ".mpg", ".mpeg":
		return true
	default:
		return false
	}
}

// FilenameFromSubject tries to extract a filename from an NZB subject.
// Supports both quoted style (e.g. "file.ext" yEnc ...) and plain style (file.ext).
func FilenameFromSubject(subj string) (string, bool) {
	m := quotedRe.FindStringSubmatch(subj)
	if len(m) == 2 {
		name := strings.TrimSpace(m[1])
		if name != "" {
			return name, true
		}
	}

	plain := strings.TrimSpace(subj)
	plain = yencTailRe.ReplaceAllString(plain, "")
	if looksLikeMediaFilename(plain) {
		return plain, true
	}
	return "", false
}
