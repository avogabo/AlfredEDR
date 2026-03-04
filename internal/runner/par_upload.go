package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/avogabo/AlfredEDR/internal/config"
	"github.com/avogabo/AlfredEDR/internal/jobs"
)

func (r *Runner) runUploadParNZB(ctx context.Context, j *jobs.Job) {
	_ = r.jobs.AppendLog(ctx, j.ID, "starting upload PAR NZB job")
	var p struct {
		Dir      string `json:"dir"`
		BaseName string `json:"base_name"`
	}
	_ = json.Unmarshal(j.Payload, &p)

	if p.Dir == "" || p.BaseName == "" {
		_ = r.jobs.SetFailed(ctx, j.ID, "dir and base_name required")
		return
	}

	cfg := config.Default()
	if r.GetConfig != nil {
		cfg = r.GetConfig()
	}
	ng := cfg.NgPost

	if !ng.Enabled || ng.Host == "" || ng.User == "" || ng.Pass == "" || ng.Groups == "" {
		_ = r.jobs.SetFailed(ctx, j.ID, "ngpost/nyuu config missing or disabled")
		return
	}

	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		_ = r.jobs.SetFailed(ctx, j.ID, "read dir error: "+err.Error())
		return
	}

	var parFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), p.BaseName) && strings.HasSuffix(e.Name(), ".par2") {
			parFiles = append(parFiles, filepath.Join(p.Dir, e.Name()))
		}
	}

	if len(parFiles) == 0 {
		_ = r.jobs.AppendLog(ctx, j.ID, "no par2 files found for base_name")
		_ = r.jobs.SetDone(ctx, j.ID)
		return
	}

	cacheDir := cfg.Paths.CacheDir
	if strings.TrimSpace(cacheDir) == "" {
		cacheDir = "/cache"
	}
	stagingDir := filepath.Join(cacheDir, "nzb-staging")
	_ = os.MkdirAll(stagingDir, 0o755)

	// Resulting NZB will be placed in the same PAR directory
	finalNZB := filepath.Join(p.Dir, p.BaseName+".par.nzb")
	stagingNZB := filepath.Join(stagingDir, fmt.Sprintf("%s.par-%s.nzb", p.BaseName, j.ID))

	args := []string{"-h", ng.Host, "-P", fmt.Sprintf("%d", ng.Port)}
	if ng.SSL {
		args = append(args, "-S")
	}
	if ng.Connections > 0 {
		args = append(args, "-n", fmt.Sprintf("%d", ng.Connections))
	}
	if ng.Groups != "" {
		args = append(args, "-g", ng.Groups)
	}

	// For PAR we can just use the base name as subject
	args = append(args,
		"--subject", p.BaseName+" PAR2 yEnc ({part}/{parts})",
		"--nzb-subject", `"{filename}" yEnc ({part}/{parts})`,
		"--message-id", "${rand(24)}-${rand(12)}@nyuu",
		"--from", "poster <poster@example.com>",
	)
	args = append(args, "-o", stagingNZB, "-O")
	args = append(args, "-u", ng.User, "-p", ng.Pass)

	// Add the actual PAR files
	args = append(args, parFiles...)

	_ = r.jobs.AppendLog(ctx, j.ID, fmt.Sprintf("uploading %d par2 files...", len(parFiles)))

	err = runCommand(ctx, func(line string) {
		clean := sanitizeLine(line, ng.Pass)
		if m := rePercent.FindStringSubmatch(clean); len(m) == 2 {
			if n, e := strconv.Atoi(m[1]); e == nil && n >= 0 && n <= 100 {
				// Only log some percent milestones to avoid spam
				if n == 25 || n == 50 || n == 75 || n == 99 {
					_ = r.jobs.AppendLog(ctx, j.ID, fmt.Sprintf("progress: %d%%", n))
				}
			}
		}
	}, r.NyuuPath, args...)

	if err != nil {
		_ = r.jobs.SetFailed(ctx, j.ID, "nyuu upload failed: "+err.Error())
		return
	}

	// Move staging to final
	_, err = moveNZBStagingToFinal(stagingNZB, finalNZB)
	if err != nil {
		_ = r.jobs.SetFailed(ctx, j.ID, "failed to move nzb: "+err.Error())
		return
	}

	_ = r.jobs.AppendLog(ctx, j.ID, "created "+finalNZB)

	// Cleanup the uploaded par2 files
	deleted := 0
	for _, pf := range parFiles {
		if err := os.Remove(pf); err == nil {
			deleted++
		}
	}
	_ = r.jobs.AppendLog(ctx, j.ID, fmt.Sprintf("deleted %d local par2 files", deleted))

	_ = r.jobs.SetDone(ctx, j.ID)
}
