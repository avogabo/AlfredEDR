package api

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avogabo/AlfredEDR/internal/fusefs"
	"github.com/avogabo/AlfredEDR/internal/streamer"
)

type webdavNode struct {
	Name   string
	Href   string
	IsDir  bool
	Size   int64
	MTime  time.Time
	Entry  *fusefs.VirtualEntry
}

func (s *Server) registerLibraryWebDAVRoutes() {
	s.mux.HandleFunc("/webdav", s.handleLibraryWebDAV)
	s.mux.HandleFunc("/webdav/", s.handleLibraryWebDAV)
}

func (s *Server) handleLibraryWebDAV(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		http.Error(w, "jobs db not configured", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("DAV", "1")
		w.Header().Set("Allow", "OPTIONS, GET, HEAD, PROPFIND")
		w.WriteHeader(http.StatusOK)
		return
	case "PROPFIND":
		s.handleLibraryPROPFIND(w, r)
		return
	case http.MethodGet, http.MethodHead:
		s.handleLibraryGETHEAD(w, r)
		return
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) buildWebDAVIndex(ctx context.Context) (map[string]webdavNode, map[string][]webdavNode, error) {
	cfg := s.Config()
	items, err := fusefs.AutoVirtualEntries(ctx, cfg, s.jobs, 8000)
	if err != nil {
		return nil, nil, err
	}
	nodes := map[string]webdavNode{}
	dirs := map[string][]webdavNode{}

	addDir := func(rel string) {
		rel = strings.Trim(strings.TrimPrefix(path.Clean("/"+rel), "/"), " ")
		if rel == "." {
			rel = ""
		}
		href := "/webdav/"
		name := ""
		if rel != "" {
			href = "/webdav/" + rel + "/"
			name = path.Base(rel)
		}
		if _, ok := nodes[href]; !ok {
			nodes[href] = webdavNode{Name: name, Href: href, IsDir: true, MTime: time.Now()}
		}
	}
	addDir("")

	for _, it := range items {
		rel := strings.Trim(strings.TrimPrefix(filepath.ToSlash(filepath.Clean(it.VirtualPath)), "/"), " ")
		if rel == "" || rel == "." {
			continue
		}
		parts := strings.Split(rel, "/")
		cur := ""
		for i, p := range parts {
			if cur == "" {
				cur = p
			} else {
				cur = cur + "/" + p
			}
			if i < len(parts)-1 {
				addDir(cur)
			}
		}
		fileHref := "/webdav/" + rel
		nodes[fileHref] = webdavNode{Name: parts[len(parts)-1], Href: fileHref, IsDir: false, Size: it.Size, MTime: time.Now(), Entry: &it}
	}

	for href, n := range nodes {
		if href == "/webdav/" {
			continue
		}
		parent := path.Dir(strings.TrimSuffix(strings.TrimPrefix(href, "/webdav"), "/"))
		if parent == "." || parent == "/" {
			parent = ""
		}
		parentHref := "/webdav/"
		if parent != "" {
			parentHref = "/webdav/" + strings.TrimPrefix(parent, "/") + "/"
		}
		dirs[parentHref] = append(dirs[parentHref], n)
	}
	for k := range dirs {
		sort.Slice(dirs[k], func(i, j int) bool {
			if dirs[k][i].IsDir != dirs[k][j].IsDir {
				return dirs[k][i].IsDir
			}
			return strings.ToLower(dirs[k][i].Name) < strings.ToLower(dirs[k][j].Name)
		})
	}
	return nodes, dirs, nil
}

type multistatus struct {
	XMLName   xml.Name   `xml:"D:multistatus"`
	XmlnsD    string     `xml:"xmlns:D,attr"`
	Responses []response `xml:"D:response"`
}

type response struct {
	Href     string   `xml:"D:href"`
	Propstat propstat `xml:"D:propstat"`
}

type propstat struct {
	Prop   prop   `xml:"D:prop"`
	Status string `xml:"D:status"`
}

type prop struct {
	DisplayName     string     `xml:"D:displayname,omitempty"`
	GetContentLen   *int64     `xml:"D:getcontentlength,omitempty"`
	GetLastModified string     `xml:"D:getlastmodified,omitempty"`
	ResourceType    resourcety `xml:"D:resourcetype"`
}

type resourcety struct {
	Collection *struct{} `xml:"D:collection,omitempty"`
}

func (s *Server) handleLibraryPROPFIND(w http.ResponseWriter, r *http.Request) {
	nodes, dirs, err := s.buildWebDAVIndex(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reqPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if reqPath == "webdav" {
		reqPath = "webdav/"
	}
	if !strings.HasPrefix(reqPath, "webdav") {
		http.NotFound(w, r)
		return
	}
	href := "/" + strings.TrimPrefix(reqPath, "/")
	if href == "/webdav" {
		href = "/webdav/"
	}
	n, ok := nodes[href]
	if !ok {
		http.NotFound(w, r)
		return
	}
	depth := strings.TrimSpace(r.Header.Get("Depth"))
	if depth == "" {
		depth = "1"
	}
	resp := []response{mkResponse(n)}
	if n.IsDir && depth != "0" {
		for _, c := range dirs[n.Href] {
			resp = append(resp, mkResponse(c))
		}
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_ = xml.NewEncoder(w).Encode(multistatus{XmlnsD: "DAV:", Responses: resp})
}

func mkResponse(n webdavNode) response {
	p := prop{DisplayName: n.Name, GetLastModified: n.MTime.UTC().Format(http.TimeFormat)}
	if n.IsDir {
		p.ResourceType.Collection = &struct{}{}
	} else {
		sz := n.Size
		p.GetContentLen = &sz
	}
	return response{Href: n.Href, Propstat: propstat{Prop: p, Status: "HTTP/1.1 200 OK"}}
}

func (s *Server) handleLibraryGETHEAD(w http.ResponseWriter, r *http.Request) {
	nodes, _, err := s.buildWebDAVIndex(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	href := path.Clean(r.URL.Path)
	if href == "/webdav" {
		href = "/webdav/"
	}
	n, ok := nodes[href]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if n.IsDir {
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte("library directory\n"))
		}
		return
	}
	if n.Entry == nil {
		http.NotFound(w, r)
		return
	}
	sz := n.Size
	w.Header().Set("Content-Type", "video/x-matroska")
	w.Header().Set("Accept-Ranges", "bytes")
	mr, perr := parseRanges(r.Header.Get("Range"), sz)
	if perr != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", sz))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	st := streamer.New(s.Config().Download, s.jobs, s.Config().Paths.CacheDir, s.Config().Paths.CacheMaxBytes)
	if mr == nil {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", sz))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_ = st.StreamRange(r.Context(), n.Entry.ImportID, n.Entry.FileIdx, n.Entry.Filename, 0, sz-1, w, prefetchForSubject(n.Entry.Subject, 8))
		return
	}
	if len(mr.Ranges) != 1 {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	br := mr.Ranges[0]
	length := br.End - br.Start + 1
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", br.Start, br.End, sz))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", length))
	w.WriteHeader(http.StatusPartialContent)
	if r.Method == http.MethodHead {
		return
	}
	_ = st.StreamRange(r.Context(), n.Entry.ImportID, n.Entry.FileIdx, n.Entry.Filename, br.Start, br.End, w, prefetchForSubject(n.Entry.Subject, 8))
}
