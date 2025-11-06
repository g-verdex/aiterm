package server

import (
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "os/exec"
    "net/http"
    "path/filepath"
    "strings"
    "time"

    "ai-terminal/api"
    "ai-terminal/internal/term"
)

type Server struct { pty *term.PTYManager }

func New() *Server { return &Server{pty: term.NewPTYManager()} }

func (s *Server) Handler() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("/v1/shell/run", s.handleShellRun)
    mux.HandleFunc("/v1/pty/open", s.handlePTYOpen)
    mux.HandleFunc("/v1/pty/send", s.handlePTYSend)
    mux.HandleFunc("/v1/pty/read", s.handlePTYRead)
    mux.HandleFunc("/v1/pty/resize", s.handlePTYResize)
    mux.HandleFunc("/v1/pty/close", s.handlePTYClose)
    mux.HandleFunc("/v1/fs/read", s.handleFSRead)
    mux.HandleFunc("/v1/fs/write", s.handleFSWrite)
    mux.HandleFunc("/v1/fs/list", s.handleFSList)
    mux.HandleFunc("/v1/bridge/tmux/create", s.handleBridgeTmuxCreate)
    mux.HandleFunc("/v1/bridge/tmux/destroy", s.handleBridgeTmuxDestroy)
    mux.HandleFunc("/v1/bridge/tmux/list", s.handleBridgeTmuxList)
    return mux
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    enc := json.NewEncoder(w)
    enc.SetEscapeHTML(false)
    _ = enc.Encode(v)
}

func (s *Server) handleShellRun(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.ShellRunRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    var stdin []byte
    if req.StdinB64 != "" {
        b, err := base64.StdEncoding.DecodeString(req.StdinB64)
        if err != nil {
            writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid stdin base64"})
            return
        }
        stdin = b
    }
    timeout := time.Duration(req.TimeoutMS) * time.Millisecond
    res, err := term.ShellRun(r.Context(), term.RunRequest{
        Argv:    req.Argv,
        Cwd:     req.Cwd,
        Env:     req.Env,
        Timeout: timeout,
        Stdin:   stdin,
    })
    out := api.ShellRunResponse{
        RC:         res.RC,
        StdoutB64:  base64.StdEncoding.EncodeToString(res.Stdout),
        StderrB64:  base64.StdEncoding.EncodeToString(res.Stderr),
        DurationMS: res.Duration.Milliseconds(),
        Cwd:        res.Cwd,
    }
    if err != nil { out.Error = err.Error() }
    writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePTYOpen(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.PTYOpenRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    id, err := s.pty.PTYOpen(req.Argv, req.Rows, req.Cols, req.Cwd, req.Env)
    if err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, api.PTYOpenResponse{ID: id})
}

func (s *Server) handlePTYSend(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.PTYSendRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    b, err := base64.StdEncoding.DecodeString(req.DataB64)
    if err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid data base64"})
        return
    }
    n, err := s.pty.PTYSend(req.ID, b)
    if err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, api.PTYSendResponse{BytesWritten: n})
}

func (s *Server) handlePTYRead(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.PTYReadRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    timeout := time.Duration(req.TimeoutMS) * time.Millisecond
    chunks, closed, err := s.pty.PTYRead(req.ID, req.SinceSeq, req.MaxBytes, timeout)
    if err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    out := api.PTYReadResponse{Closed: closed}
    out.Chunks = make([]api.PTYChunk, 0, len(chunks))
    for _, c := range chunks {
        out.Chunks = append(out.Chunks, api.PTYChunk{
            Seq: c.Seq, Stream: c.Stream, Ts: c.Ts.UnixMilli(), Data: base64.StdEncoding.EncodeToString(c.Data),
        })
    }
    writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePTYResize(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.PTYResizeRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    if err := s.pty.PTYResize(req.ID, req.Rows, req.Cols); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePTYClose(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.PTYCloseRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    if err := s.pty.PTYClose(req.ID); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

func (s *Server) handleFSRead(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.FSReadRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    f, err := os.Open(req.Path)
    if err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    defer f.Close()
    st, _ := f.Stat()
    max := req.MaxBytes
    if max <= 0 { max = 1 << 20 }
    buf := make([]byte, max)
    n, _ := io.ReadFull(f, buf)
    truncated := false
    if st != nil && st.Size() > int64(n) {
        truncated = true
    }
    out := api.FSReadResponse{DataB64: base64.StdEncoding.EncodeToString(buf[:n]), Size: 0, Truncated: truncated}
    if st != nil { out.Size = st.Size() }
    writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleFSWrite(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.FSWriteRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    b, err := base64.StdEncoding.DecodeString(req.Data)
    if err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid base64"})
        return
    }
    mode := os.FileMode(0644)
    if req.Mode != "" {
        var m uint32
        _, err := fmt.Sscanf(req.Mode, "%o", &m)
        if err == nil { mode = os.FileMode(m) }
    }
    if err := os.WriteFile(req.Path, b, mode); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, api.FSWriteResponse{Bytes: len(b)})
}

func (s *Server) handleFSList(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.FSListRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    entries, err := os.ReadDir(req.Path)
    if err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    out := api.FSListResponse{Entries: make([]api.FSEntry, 0, len(entries))}
    for _, e := range entries {
        st, _ := e.Info()
        typ := "other"
        if e.IsDir() { typ = "dir" } else { typ = "file" }
        if st != nil && (st.Mode()&os.ModeSymlink) != 0 { typ = "link" }
        mode := ""
        if st != nil { mode = st.Mode().Perm().String() }
        size := int64(0)
        if st != nil { size = st.Size() }
        out.Entries = append(out.Entries, api.FSEntry{Name: e.Name(), Type: typ, Mode: mode, Size: size})
    }
    writeJSON(w, http.StatusOK, out)
}

// --- tmux bridge ---
func (s *Server) handleBridgeTmuxCreate(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.BridgeTmuxCreateRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    socket := "/tmp/aiterm/tmux-" + req.ID + ".sock"
    session := "ai-" + req.ID
    _ = os.MkdirAll("/tmp/aiterm", 0o755)
    // Prefer interactive bridge if helper exists; fallback to log tail
    _, lookErr := exec.LookPath("aiterm-bridge")
    var cmd *exec.Cmd
    interactive := lookErr == nil
    if interactive {
        // Run the bridge helper inside tmux; it will proxy input/output
        base := r.Host
        if base == "" { base = "127.0.0.1:8099" }
        bridgeCmd := "stty -echo; aiterm-bridge -server 'http://" + base + "' -id '" + req.ID + "'"
        cmd = exec.Command("tmux", "-S", socket, "-f", "/dev/null", "new-session", "-d", "-s", session, "sh", "-lc", bridgeCmd)
    } else {
        logPath, ok := s.pty.LogPath(req.ID)
        if !ok {
            writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such session or no log"})
            return
        }
        tailCmd := "stty -echo; tail -F -n +1 -- '" + logPath + "'"
        cmd = exec.Command("tmux", "-S", socket, "-f", "/dev/null", "new-session", "-d", "-s", session, "sh", "-lc", tailCmd)
    }
    if err := cmd.Run(); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tmux not available or failed"})
        return
    }
    // Apply useful options
    opt := func(args ...string) { _ = exec.Command("tmux", append([]string{"-S", socket}, args...)...).Run() }
    opt("set-option", "-t", session, "status", "off")
    opt("set-option", "-t", session, "mouse", "off")
    opt("set-option", "-t", session, "history-limit", "200000")
    opt("set-option", "-t", session, "allow-rename", "off")
    opt("set-option", "-t", session, "set-titles", "off")
    opt("set-option", "-g", "abandon-focus-on-exit", "on")
    opt("set-option", "-g", "assume-paste-time", "0")
    opt("set-option", "-g", "escape-time", "0")
    // disable alt screen
    opt("set-option", "-g", "terminal-overrides", ",*:smcup@:rmcup@")

    attach := "tmux -S '" + socket + "' attach -t '" + session + "'"
    if !interactive { attach += " -r" }
    // LogPath may be empty in interactive case; try to provide when available
    logPath, _ := s.pty.LogPath(req.ID)
    writeJSON(w, http.StatusOK, api.BridgeTmuxCreateResponse{Socket: socket, Session: session, AttachHint: attach, LogPath: logPath})
}

func (s *Server) handleBridgeTmuxDestroy(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    var req api.BridgeTmuxDestroyRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
        return
    }
    // Kill the session and remove socket
    _ = exec.Command("tmux", "-S", req.Socket, "kill-session", "-t", req.Session).Run()
    _ = os.Remove(req.Socket)
    writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleBridgeTmuxList(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet && r.Method != http.MethodPost { w.WriteHeader(http.StatusMethodNotAllowed); return }
    // Discover sockets and sessions
    baseDir := "/tmp/aiterm"
    entries := []api.BridgeTmuxEntry{}
    socks, _ := filepath.Glob(filepath.Join(baseDir, "tmux-*.sock"))
    for _, sock := range socks {
        // id is basename without prefix/suffix
        bn := filepath.Base(sock)
        id := strings.TrimSuffix(strings.TrimPrefix(bn, "tmux-"), ".sock")
        session := "ai-" + id
        alive := exec.Command("tmux", "-S", sock, "has-session", "-t", session).Run() == nil
        attach := "tmux -S '" + sock + "' attach -t '" + session + "'"
        logp := filepath.Join(baseDir, "sessions", id+".log")
        if _, err := os.Stat(logp); err != nil { logp = "" }
        entries = append(entries, api.BridgeTmuxEntry{ID: id, Socket: sock, Session: session, AttachHint: attach, LogPath: logp, Alive: alive})
    }
    writeJSON(w, http.StatusOK, api.BridgeTmuxListResponse{Bridges: entries})
}

// no health endpoint; kept simple
