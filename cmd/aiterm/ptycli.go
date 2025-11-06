package main

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "flag"
    "fmt"
    "io"
    "net/http"
    "os"
    "strings"
    "time"

    "ai-terminal/api"
)

func defaultServer(fs *flag.FlagSet) *string {
    def := os.Getenv("AITERM_SERVER")
    if def == "" { def = "http://127.0.0.1:8099" }
    return fs.String("server", def, "aitermd server URL")
}

func ptyOpenCmd(args []string) {
    fs := flag.NewFlagSet("pty-open", flag.ExitOnError)
    server := defaultServer(fs)
    sep := indexOf(args, "--")
    our := args
    rest := []string{}
    if sep >= 0 { our = args[:sep]; rest = args[sep+1:] }
    if err := fs.Parse(our); err != nil { os.Exit(2) }
    argv := fs.Args()
    if len(argv) == 0 && len(rest) > 0 { argv = rest }
    if len(argv) == 0 { fmt.Fprintln(os.Stderr, "missing argv after --"); os.Exit(2) }
    req := api.PTYOpenRequest{Argv: argv, Rows: 24, Cols: 80, Env: map[string]string{"TERM": "dumb", "PS1": ""}}
    body, _ := json.Marshal(req)
    resp, err := http.Post(strings.TrimRight(*server, "/")+"/v1/pty/open", "application/json", bytes.NewReader(body))
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    defer resp.Body.Close()
    io.Copy(os.Stdout, resp.Body)
}

func ptySendCmd(args []string) {
    fs := flag.NewFlagSet("pty-send", flag.ExitOnError)
    server := defaultServer(fs)
    id := fs.String("id", "", "session id")
    data := fs.String("data", "", "data string to send (use --stdin for raw)")
    useStdin := fs.Bool("stdin", false, "read data from stdin")
    if err := fs.Parse(args); err != nil { os.Exit(2) }
    if *id == "" { fmt.Fprintln(os.Stderr, "--id required"); os.Exit(2) }
    var b []byte
    if *useStdin {
        in, _ := io.ReadAll(os.Stdin)
        b = in
    } else {
        b = []byte(*data)
    }
    req := api.PTYSendRequest{ID: *id, DataB64: base64.StdEncoding.EncodeToString(b)}
    body, _ := json.Marshal(req)
    resp, err := http.Post(strings.TrimRight(*server, "/")+"/v1/pty/send", "application/json", bytes.NewReader(body))
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    defer resp.Body.Close()
    io.Copy(os.Stdout, resp.Body)
}

func ptyReadCmd(args []string) {
    fs := flag.NewFlagSet("pty-read", flag.ExitOnError)
    server := defaultServer(fs)
    id := fs.String("id", "", "session id")
    since := fs.Uint64("since", 0, "since seq")
    maxBytes := fs.Int("max-bytes", 65536, "max bytes")
    timeoutStr := fs.String("timeout", "500ms", "timeout")
    if err := fs.Parse(args); err != nil { os.Exit(2) }
    if *id == "" { fmt.Fprintln(os.Stderr, "--id required"); os.Exit(2) }
    to, err := time.ParseDuration(*timeoutStr)
    if err != nil { fmt.Fprintln(os.Stderr, "bad timeout"); os.Exit(2) }
    req := api.PTYReadRequest{ID: *id, SinceSeq: *since, MaxBytes: *maxBytes, TimeoutMS: to.Milliseconds()}
    body, _ := json.Marshal(req)
    resp, err := http.Post(strings.TrimRight(*server, "/")+"/v1/pty/read", "application/json", bytes.NewReader(body))
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    defer resp.Body.Close()
    io.Copy(os.Stdout, resp.Body)
}

func ptyFollowCmd(args []string) {
    fs := flag.NewFlagSet("pty-follow", flag.ExitOnError)
    server := defaultServer(fs)
    id := fs.String("id", "", "session id")
    timeoutStr := fs.String("timeout", "500ms", "read timeout")
    if err := fs.Parse(args); err != nil { os.Exit(2) }
    if *id == "" { fmt.Fprintln(os.Stderr, "--id required"); os.Exit(2) }
    to, err := time.ParseDuration(*timeoutStr)
    if err != nil { fmt.Fprintln(os.Stderr, "bad timeout"); os.Exit(2) }
    since := uint64(0)
    dec := base64.StdEncoding
    for {
        req := api.PTYReadRequest{ID: *id, SinceSeq: since, MaxBytes: 1<<16, TimeoutMS: to.Milliseconds()}
        body, _ := json.Marshal(req)
        resp, err := http.Post(strings.TrimRight(*server, "/")+"/v1/pty/read", "application/json", bytes.NewReader(body))
        if err != nil { fmt.Fprintln(os.Stderr, err); time.Sleep(200*time.Millisecond); continue }
        var rr api.PTYReadResponse
        if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil { resp.Body.Close(); fmt.Fprintln(os.Stderr, err); time.Sleep(200*time.Millisecond); continue }
        resp.Body.Close()
        for _, c := range rr.Chunks {
            b, _ := dec.DecodeString(c.Data)
            os.Stdout.Write(b)
            since = c.Seq
        }
        if rr.Closed {
            break
        }
    }
}

func ptyResizeCmd(args []string) {
    fs := flag.NewFlagSet("pty-resize", flag.ExitOnError)
    server := defaultServer(fs)
    id := fs.String("id", "", "session id")
    rows := fs.Int("rows", 24, "rows")
    cols := fs.Int("cols", 80, "cols")
    if err := fs.Parse(args); err != nil { os.Exit(2) }
    if *id == "" { fmt.Fprintln(os.Stderr, "--id required"); os.Exit(2) }
    req := api.PTYResizeRequest{ID: *id, Rows: *rows, Cols: *cols}
    body, _ := json.Marshal(req)
    resp, err := http.Post(strings.TrimRight(*server, "/")+"/v1/pty/resize", "application/json", bytes.NewReader(body))
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    defer resp.Body.Close()
    io.Copy(os.Stdout, resp.Body)
}

func ptyCloseCmd(args []string) {
    fs := flag.NewFlagSet("pty-close", flag.ExitOnError)
    server := defaultServer(fs)
    id := fs.String("id", "", "session id")
    if err := fs.Parse(args); err != nil { os.Exit(2) }
    if *id == "" { fmt.Fprintln(os.Stderr, "--id required"); os.Exit(2) }
    req := api.PTYCloseRequest{ID: *id}
    body, _ := json.Marshal(req)
    resp, err := http.Post(strings.TrimRight(*server, "/")+"/v1/pty/close", "application/json", bytes.NewReader(body))
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    defer resp.Body.Close()
    io.Copy(os.Stdout, resp.Body)
}

func bridgeListCmd(args []string) {
    fs := flag.NewFlagSet("bridge-list", flag.ExitOnError)
    server := defaultServer(fs)
    asJSON := fs.Bool("json", false, "print raw JSON")
    if err := fs.Parse(args); err != nil { os.Exit(2) }
    url := strings.TrimRight(*server, "/") + "/v1/bridge/tmux/list"
    req, _ := http.NewRequest("GET", url, nil)
    resp, err := http.DefaultClient.Do(req)
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    defer resp.Body.Close()
    if *asJSON {
        io.Copy(os.Stdout, resp.Body)
        return
    }
    var out api.BridgeTmuxListResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    if len(out.Bridges) == 0 { fmt.Println("(no bridges)"); return }
    for _, b := range out.Bridges {
        status := "dead"
        if b.Alive { status = "alive" }
        fmt.Printf("id=%s status=%s\n  socket=%s\n  session=%s\n  attach=%s\n", b.ID, status, b.Socket, b.Session, b.AttachHint)
        if b.LogPath != "" { fmt.Printf("  log=%s\n", b.LogPath) }
    }
}
