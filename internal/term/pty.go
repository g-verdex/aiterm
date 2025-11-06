package term

import (
    "errors"
    "fmt"
    "io"
    "math/rand"
    "os"
    "os/exec"
    "sync"
    "time"

    ptylib "github.com/creack/pty"
    "golang.org/x/term"
)

// Chunk represents a piece of streamed PTY output.
type Chunk struct {
    Seq    uint64
    Stream string // "stdout" only for PTY
    Data   []byte
    Ts     time.Time
}

// PTYSession holds state for a running PTY process.
type PTYSession struct {
    id      string
    cmd     *exec.Cmd
    pty     *os.File

    mu       sync.Mutex
    chunks   []Chunk
    nextSeq  uint64
    closed   bool
    closedCh chan struct{}
    exitRC   *int

    cond *sync.Cond

    // logging
    logf *os.File
}

// PTYManager manages multiple PTY sessions.
type PTYManager struct {
    mu       sync.Mutex
    sessions map[string]*PTYSession
    // Configuration
    maxBytes int // cap total buffered bytes per session (evict oldest)
    baseDir  string // base directory for session logs
}

func NewPTYManager() *PTYManager {
    return &PTYManager{
        sessions: make(map[string]*PTYSession),
        maxBytes: 1 << 20, // 1 MiB
        baseDir:  "/tmp/aiterm/sessions",
    }
}

// PTYOpen starts a new PTY session with the given argv and dimensions.
func (m *PTYManager) PTYOpen(argv []string, rows, cols int, cwd string, env map[string]string) (string, error) {
    if len(argv) == 0 {
        return "", errors.New("argv must not be empty")
    }
    cmd := exec.Command(argv[0], argv[1:]...)
    if cwd != "" {
        cmd.Dir = cwd
    }
    // Build a minimal env
    var envv []string
    for k, v := range env {
        envv = append(envv, fmt.Sprintf("%s=%s", k, v))
    }
    cmd.Env = envv

    // Start with a pty
    pty, err := ptylib.Start(cmd)
    if err != nil {
        return "", err
    }

    if rows <= 0 {
        rows = 40
    }
    if cols <= 0 {
        cols = 120
    }
    _ = ptylib.Setsize(pty, &ptylib.Winsize{Rows: uint16(rows), Cols: uint16(cols)})

    s := &PTYSession{
        id:       randID(),
        cmd:      cmd,
        pty:      pty,
        chunks:   make([]Chunk, 0, 128),
        nextSeq:  1,
        closedCh: make(chan struct{}),
    }
    s.cond = sync.NewCond(&s.mu)

    // Prepare log file path
    if m.baseDir != "" {
        _ = os.MkdirAll(m.baseDir, 0o755)
        logPath := m.baseDir + "/" + s.id + ".log"
        if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
            s.logf = f
        }
    }

    go s.reader()
    go s.waiter()

    m.mu.Lock()
    m.sessions[s.id] = s
    m.mu.Unlock()
    return s.id, nil
}

func (s *PTYSession) reader() {
    buf := make([]byte, 4096)
    for {
        n, err := s.pty.Read(buf)
        if n > 0 {
            s.mu.Lock()
            data := make([]byte, n)
            copy(data, buf[:n])
            s.chunks = append(s.chunks, Chunk{Seq: s.nextSeq, Stream: "stdout", Data: data, Ts: time.Now()})
            s.nextSeq++
            // enforce cap
            s.enforceCap()
            s.cond.Broadcast()
            s.mu.Unlock()
            // async log write (best-effort)
            if s.logf != nil {
                _, _ = s.logf.Write(data)
            }
        }
        if err != nil {
            if err == io.EOF {
                s.mu.Lock()
                s.closed = true
                close(s.closedCh)
                s.cond.Broadcast()
                s.mu.Unlock()
            }
            return
        }
    }
}

func (s *PTYSession) waiter() {
    _ = s.cmd.Wait()
    s.mu.Lock()
    if s.exitRC == nil {
        rc := exitCodeFromWait(s.cmd.ProcessState)
        s.exitRC = &rc
    }
    s.closed = true
    select {
    case <-s.closedCh:
    default:
        close(s.closedCh)
    }
    s.cond.Broadcast()
    s.mu.Unlock()
}

func (s *PTYSession) enforceCap() {
    // crude eviction by concatenated length
    total := 0
    for i := len(s.chunks) - 1; i >= 0; i-- {
        total += len(s.chunks[i].Data)
        if total > capSize() {
            // drop head up to i
            s.chunks = append([]Chunk(nil), s.chunks[i+1:]...)
            break
        }
    }
}

func capSize() int { return 1 << 20 }

// PTYSend writes data to the session.
func (m *PTYManager) PTYSend(id string, data []byte) (int, error) {
    s := m.get(id)
    if s == nil {
        return 0, errors.New("no such session")
    }
    return s.pty.Write(data)
}

// PTYRead returns chunks with seq > sinceSeq, up to maxBytes or until timeout.
func (m *PTYManager) PTYRead(id string, sinceSeq uint64, maxBytes int, timeout time.Duration) ([]Chunk, bool, error) {
    s := m.get(id)
    if s == nil {
        return nil, false, errors.New("no such session")
    }
    deadline := time.Now().Add(timeout)
    for {
        s.mu.Lock()
        // collect chunks
        var out []Chunk
        bytes := 0
        for _, c := range s.chunks {
            if c.Seq <= sinceSeq { continue }
            out = append(out, c)
            bytes += len(c.Data)
            if maxBytes > 0 && bytes >= maxBytes {
                break
            }
        }
        closed := s.closed
        if len(out) > 0 || closed {
            s.mu.Unlock()
            return out, closed, nil
        }
        // wait
        now := time.Now()
        if timeout > 0 && now.After(deadline) {
            s.mu.Unlock()
            return nil, s.closed, nil
        }
        // Wait with a small timeout by unlocking and sleeping to avoid deadlocks
        s.mu.Unlock()
        time.Sleep(50 * time.Millisecond)
    }
}

// PTYResize updates window size.
func (m *PTYManager) PTYResize(id string, rows, cols int) error {
    s := m.get(id)
    if s == nil {
        return errors.New("no such session")
    }
    return ptylib.Setsize(s.pty, &ptylib.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// PTYClose sends SIGTERM then closes the PTY.
func (m *PTYManager) PTYClose(id string) error {
    s := m.get(id)
    if s == nil { return nil }
    _ = s.cmd.Process.Signal(os.Interrupt)
    select {
    case <-s.closedCh:
    case <-time.After(300 * time.Millisecond):
        _ = s.cmd.Process.Kill()
    }
    _ = s.pty.Close()
    if s.logf != nil { _ = s.logf.Close() }
    m.mu.Lock()
    delete(m.sessions, id)
    m.mu.Unlock()
    return nil
}

func (m *PTYManager) get(id string) *PTYSession {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.sessions[id]
}

func randID() string {
    const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
    b := make([]byte, 8)
    for i := range b {
        b[i] = letters[rand.Intn(len(letters))]
    }
    return string(b)
}

// exitCodeFromWait returns a code similar to shell semantics.
func exitCodeFromWait(ps *os.ProcessState) int {
    if ps == nil { return -1 }
    if ws, ok := ps.Sys().(interface{ ExitStatus() int; Signaled() bool; Signal() os.Signal }); ok {
        if ws.Signaled() {
            // 128 + signal number is typical convention
            // Unfortunately ws.Signal() may not expose number portably; fallback -1
            return -1
        }
        return ws.ExitStatus()
    }
    return -1
}

// Helper to ensure we import term and avoid unused error if not yet used elsewhere
var _ = term.IsTerminal

// LogPath returns the log file path for a session if available.
func (m *PTYManager) LogPath(id string) (string, bool) {
    m.mu.Lock()
    s := m.sessions[id]
    m.mu.Unlock()
    if s == nil || m.baseDir == "" { return "", false }
    return m.baseDir + "/" + id + ".log", true
}
