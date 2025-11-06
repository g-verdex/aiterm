package main

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "flag"
    "fmt"
    "os"
    "strings"
    "time"

    "ai-terminal/internal/term"
)

type runOutput struct {
    RC          int    `json:"rc"`
    StdoutB64   string `json:"stdout"`
    StderrB64   string `json:"stderr"`
    DurationMS  int64  `json:"duration_ms"`
    Cwd         string `json:"cwd"`
    Error       string `json:"error,omitempty"`
}

func main() {
    if len(os.Args) < 2 {
        usage()
        os.Exit(2)
    }
    switch os.Args[1] {
    case "run":
        runCmd(os.Args[2:])
    case "pty-open":
        ptyOpenCmd(os.Args[2:])
    case "pty-send":
        ptySendCmd(os.Args[2:])
    case "pty-read":
        ptyReadCmd(os.Args[2:])
    case "pty-follow":
        ptyFollowCmd(os.Args[2:])
    case "pty-resize":
        ptyResizeCmd(os.Args[2:])
    case "pty-close":
        ptyCloseCmd(os.Args[2:])
    case "bridge-list":
        bridgeListCmd(os.Args[2:])
    default:
        usage()
        os.Exit(2)
    }
}

func usage() {
    fmt.Fprintf(os.Stderr, "Usage:\n")
    fmt.Fprintf(os.Stderr, "  aiterm run [--cwd DIR] [--timeout 5s] [--env KEY=VAL,...] [--stdin-base64 DATA] -- argv...\n")
    fmt.Fprintf(os.Stderr, "  aiterm pty-open [--server URL] -- argv...\n")
    fmt.Fprintf(os.Stderr, "  aiterm pty-send [--server URL] --id ID [--data STRING|--stdin]\n")
    fmt.Fprintf(os.Stderr, "  aiterm pty-read [--server URL] --id ID [--since N] [--timeout 500ms] [--max-bytes N]\n")
    fmt.Fprintf(os.Stderr, "  aiterm pty-follow [--server URL] --id ID [--timeout 500ms]\n")
    fmt.Fprintf(os.Stderr, "  aiterm pty-resize [--server URL] --id ID --rows N --cols N\n")
    fmt.Fprintf(os.Stderr, "  aiterm pty-close [--server URL] --id ID\n")
    fmt.Fprintf(os.Stderr, "  aiterm bridge-list [--server URL] [--json]\n")
}

func runCmd(args []string) {
    fs := flag.NewFlagSet("run", flag.ExitOnError)
    cwd := fs.String("cwd", "", "working directory")
    timeoutStr := fs.String("timeout", "", "timeout (e.g., 5s, 2m)")
    envCSV := fs.String("env", "", "comma-separated KEY=VAL pairs")
    stdinB64 := fs.String("stdin-base64", "", "stdin data as base64")
    // Split on -- to separate our flags from argv
    // Example: aiterm run --timeout 2s -- -- /bin/echo hi
    sep := indexOf(args, "--")
    var our, rest []string
    if sep >= 0 {
        our = args[:sep]
        rest = args[sep+1:]
    } else {
        our = args
        rest = nil
    }
    if err := fs.Parse(our); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(2)
    }
    argv := fs.Args()
    if len(argv) == 0 && len(rest) > 0 {
        argv = rest
    }
    if len(argv) == 0 {
        fmt.Fprintln(os.Stderr, "error: missing argv (provide after --)")
        os.Exit(2)
    }

    var timeout time.Duration
    var err error
    if *timeoutStr != "" {
        timeout, err = time.ParseDuration(*timeoutStr)
        if err != nil {
            fmt.Fprintf(os.Stderr, "invalid --timeout: %v\n", err)
            os.Exit(2)
        }
    }
    envMap := map[string]string{}
    if *envCSV != "" {
        pairs := strings.Split(*envCSV, ",")
        for _, p := range pairs {
            if p == "" { continue }
            kv := strings.SplitN(p, "=", 2)
            if len(kv) != 2 {
                fmt.Fprintf(os.Stderr, "invalid env pair: %q\n", p)
                os.Exit(2)
            }
            envMap[kv[0]] = kv[1]
        }
    }
    var stdin []byte
    if *stdinB64 != "" {
        b, err := base64.StdEncoding.DecodeString(*stdinB64)
        if err != nil {
            fmt.Fprintf(os.Stderr, "invalid --stdin-base64: %v\n", err)
            os.Exit(2)
        }
        stdin = b
    }

    req := term.RunRequest{
        Argv:    argv,
        Cwd:     *cwd,
        Env:     envMap,
        Timeout: timeout,
        Stdin:   stdin,
    }

    res, err := term.ShellRun(context.Background(), req)
    out := runOutput{
        RC:         res.RC,
        StdoutB64:  base64.StdEncoding.EncodeToString(res.Stdout),
        StderrB64:  base64.StdEncoding.EncodeToString(res.Stderr),
        DurationMS: res.Duration.Milliseconds(),
        Cwd:        res.Cwd,
    }
    if err != nil {
        out.Error = err.Error()
    }
    enc := json.NewEncoder(os.Stdout)
    enc.SetEscapeHTML(false)
    if err := enc.Encode(out); err != nil {
        fmt.Fprintf(os.Stderr, "encode error: %v\n", err)
        os.Exit(1)
    }
    // exit code reflects process rc if available
    os.Exit(res.RC)
}

func indexOf(ss []string, s string) int {
    for i, v := range ss {
        if v == s { return i }
    }
    return -1
}
