package fusefs

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/avogabo/AlfredEDR/internal/config"
	"github.com/avogabo/AlfredEDR/internal/jobs"
)

type VirtualEntry struct {
	VirtualPath string
	ImportID    string
	FileIdx     int
	Filename    string
	Size        int64
	Subject     string
}

// AutoVirtualEntries returns virtual library entries (relative paths) mapped to source import/file rows.
func AutoVirtualEntries(ctx context.Context, cfg config.Config, st *jobs.Store, limit int) ([]VirtualEntry, error) {
	if st == nil {
		return nil, fmt.Errorf("jobs store required")
	}
	if limit <= 0 {
		limit = 5000
	}
	lfs := &LibraryFS{Cfg: cfg, Jobs: st}
	_, _ = lfs.Root()
	ld := &libDir{fs: lfs, rel: ""}

	rows, err := st.DB().SQL.QueryContext(ctx, `SELECT import_id, idx, filename, subject, total_bytes FROM nzb_files ORDER BY import_id, idx LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]VirtualEntry, 0, 256)
	seen := map[string]bool{}
	for rows.Next() {
		var importID, subj string
		var idx int
		var fn sql.NullString
		var bytes int64
		if err := rows.Scan(&importID, &idx, &fn, &subj, &bytes); err != nil {
			continue
		}
		name := strings.TrimSpace(fn.String)
		if name == "" {
			name = filepath.Base(subj)
		}
		if strings.ToLower(filepath.Ext(name)) != ".mkv" {
			continue
		}
		vp := ld.buildPath(ctx, libRow{ImportID: importID, Idx: idx, Filename: name, Bytes: bytes})
		vp = filepath.Clean(vp)
		vp = strings.TrimPrefix(vp, string(filepath.Separator))
		if vp == "" || vp == "." {
			continue
		}
		if seen[vp] {
			continue
		}
		seen[vp] = true
		out = append(out, VirtualEntry{VirtualPath: vp, ImportID: importID, FileIdx: idx, Filename: name, Size: bytes, Subject: subj})
	}
	return out, nil
}
