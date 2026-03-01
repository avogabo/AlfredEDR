package streamer

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/avogabo/AlfredEDR/internal/cache"
	"github.com/avogabo/AlfredEDR/internal/yenc"
)

type SegmentLocator struct {
	ImportID  string
	FileIdx   int
	Number    int
	Bytes     int64
	MessageID string
}

type FileLayout struct {
	ImportID string
	FileIdx  int
	Total    int64
	Segs     []SegmentLocator // sorted by Number
	Offsets  []int64          // starting byte offset for each seg (same index as Segs)
}

func buildLayout(segs []segRow, importID string, fileIdx int) (*FileLayout, error) {
	sort.Slice(segs, func(i, j int) bool { return segs[i].Number < segs[j].Number })
	layout := &FileLayout{ImportID: importID, FileIdx: fileIdx}
	layout.Segs = make([]SegmentLocator, 0, len(segs))
	layout.Offsets = make([]int64, 0, len(segs))
	var off int64 = 0
	for _, s := range segs {
		layout.Offsets = append(layout.Offsets, off)
		layout.Segs = append(layout.Segs, SegmentLocator{ImportID: importID, FileIdx: fileIdx, Number: s.Number, Bytes: s.Bytes, MessageID: s.MessageID})
		off += s.Bytes
	}
	layout.Total = off
	return layout, nil
}

func (s *Streamer) segCachePath(importID string, fileIdx int, segNum int, messageID string) string {
	// include message-id hash to avoid collisions if same seg num changes across reimports
	h := sha1.Sum([]byte(messageID))
	name := fmt.Sprintf("%06d_%s.bin", segNum, hex.EncodeToString(h[:6]))
	return filepath.Join(s.cacheDir, "rawseg", importID, fmt.Sprintf("%d", fileIdx), name)
}

func (s *Streamer) ensureSegment(ctx context.Context, seg SegmentLocator) (string, error) {
	p := s.segCachePath(seg.ImportID, seg.FileIdx, seg.Number, seg.MessageID)
	if st, err := os.Stat(p); err == nil && st.Size() > 0 {
		return p, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}

	// Single-flight per segment cache path to avoid concurrent writers racing on .part/.rename.
	lAny, _ := s.segLocks.LoadOrStore(p, &sync.Mutex{})
	l := lAny.(*sync.Mutex)
	l.Lock()
	defer l.Unlock()

	// Re-check after lock (another goroutine may have completed it).
	if st, err := os.Stat(p); err == nil && st.Size() > 0 {
		return p, nil
	}

	// Download + decode (reuse NNTP connections)
	if s.pool == nil {
		return "", fmt.Errorf("nntp pool not initialized")
	}
	cl, err := s.pool.Acquire(ctx)
	if err != nil {
		return "", err
	}
	defer s.pool.Release(cl)
	log.Printf("rawseg: import=%s fileIdx=%d seg=%d fetching", seg.ImportID, seg.FileIdx, seg.Number)
	lines, err := cl.BodyByMessageID(seg.MessageID)
	if err != nil {
		return "", err
	}
	data, _, _, _, err := yenc.DecodePart(lines)
	if err != nil {
		return "", err
	}
	log.Printf("rawseg: import=%s fileIdx=%d seg=%d decoded=%d bytes", seg.ImportID, seg.FileIdx, seg.Number, len(data))

	tmp := p + ".part"
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		return "", err
	}
	// Best-effort cache limit enforcement.
	cache.EnforceSizeLimit(filepath.Join(s.cacheDir, "rawseg"), s.maxCache)
	return p, nil
}

// StreamRange writes exactly [start,end] inclusive from the logical file.
// El parámetro prefetch indica cuántos segmentos adicionales descargar anticipadamente.
func (s *Streamer) StreamRange(ctx context.Context, importID string, fileIdx int, filename string, start, end int64, w io.Writer, prefetch int) error {
	// Load segments from DB
	qctx, qcancel := context.WithTimeout(ctx, 5*time.Second)
	defer qcancel()
	rows, err := s.jobs.DB().SQL.QueryContext(qctx, `SELECT number,bytes,message_id FROM nzb_segments WHERE import_id=? AND file_idx=? ORDER BY number ASC`, importID, fileIdx)
	if err != nil {
		return err
	}
	defer rows.Close()
	segs := make([]segRow, 0)
	for rows.Next() {
		var r segRow
		if err := rows.Scan(&r.Number, &r.Bytes, &r.MessageID); err != nil {
			continue
		}
		r.MessageID = strings.TrimSpace(r.MessageID)
		segs = append(segs, r)
	}
	if len(segs) == 0 {
		return fmt.Errorf("no segments")
	}
	layout, _ := buildLayout(segs, importID, fileIdx)
	if start < 0 {
		start = 0
	}
	if end < start {
		return fmt.Errorf("invalid range")
	}

	// IMPORTANT: NZB segment bytes are often ENCODED sizes and may not match decoded payload sizes.
	// We use encoded offsets only as a fast index hint (start near requested range),
	// then stream using real decoded segment sizes from cache/files.
	writtenAny := false

	nyuuMode := prefetch < 0
	if nyuuMode {
		prefetch = -prefetch
	}

	startIdx := sort.Search(len(layout.Segs), func(i int) bool {
		return layout.Offsets[i]+layout.Segs[i].Bytes > start
	})
	if startIdx < 0 {
		startIdx = 0
	}
	// Backtrack window to absorb encoded-vs-decoded drift.
	// Nyuu-posted releases can drift more, so widen the window without full scan.
	backtrack := 2
	if nyuuMode {
		// Keep startup snappy for WebDAV first-byte by limiting initial rewind.
		// If this under-shoots on some posts, we still continue forward and can fallback later.
		backtrack = 24
	}
	if startIdx > backtrack {
		startIdx -= backtrack
	} else {
		startIdx = 0
	}
	off := int64(0)
	if startIdx < len(layout.Offsets) {
		off = layout.Offsets[startIdx]
	}

	streamFrom := func(fromIdx int, fromOff int64) (bool, error) {
		localWritten := false
		offLocal := fromOff
		for i := fromIdx; i < len(layout.Segs); i++ {
			seg := layout.Segs[i]

			p, err := s.ensureSegment(ctx, seg)
			if err != nil {
				return localWritten, err
			}
			st, err := os.Stat(p)
			if err != nil {
				return localWritten, err
			}
			segSize := st.Size()
			if segSize <= 0 {
				continue
			}
			segStart := offLocal
			segEnd := offLocal + segSize - 1
			offLocal += segSize

			if start > segEnd {
				continue
			}
			if end < segStart {
				break
			}

			f, err := os.Open(p)
			if err != nil {
				return localWritten, err
			}
			sliceStart := start
			if sliceStart < segStart {
				sliceStart = segStart
			}
			sliceEnd := end
			if sliceEnd > segEnd {
				sliceEnd = segEnd
			}
			if _, err := f.Seek(sliceStart-segStart, 0); err != nil {
				_ = f.Close()
				return localWritten, err
			}
			if _, err := io.CopyN(w, f, (sliceEnd-sliceStart)+1); err != nil {
				_ = f.Close()
				return localWritten, err
			}
			_ = f.Close()
			localWritten = true

			// Prefetch only after first bytes are already flowing to client.
			// This protects startup latency from background warm-up work.
			if prefetch > 0 && i+1 < len(layout.Segs) {
				for j := 1; j <= prefetch && i+j < len(layout.Segs); j++ {
					nextSeg := layout.Segs[i+j]
					select {
					case s.prefetchSem <- struct{}{}:
						go func(ns SegmentLocator) {
							defer func() { <-s.prefetchSem }()
							ctx2, cancel := context.WithTimeout(ctx, 20*time.Second)
							defer cancel()
							_, _ = s.ensureSegment(ctx2, ns)
						}(nextSeg)
					default:
						// keep latency low for foreground range request
					}
				}
			}
			if sliceEnd == end {
				break
			}
		}
		return localWritten, nil
	}

	writtenAny, err = streamFrom(startIdx, off)
	if err != nil {
		return err
	}
	if !writtenAny && nyuuMode && start > 0 {
		// Resume requests (non-zero ranges) can suffer large encoded-vs-decoded drift on some posts.
		// Fallback to an exact scan from segment 0 to reliably locate the requested offset.
		writtenAny, err = streamFrom(0, 0)
		if err != nil {
			return err
		}
	}
	if !writtenAny {
		// Requested range starts beyond currently addressable decoded data.
		// For FUSE readers this should behave like EOF (empty read), not I/O error.
		return nil
	}
	return nil
}
