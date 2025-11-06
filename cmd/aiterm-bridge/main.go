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
    "os/signal"
    "sync/atomic"
    "syscall"
    "time"

    "ai-terminal/api"
)

func main() {
    server := flag.String("server", "http://127.0.0.1:8099", "aitermd server URL")
    id := flag.String("id", "", "session id to bridge")
    timeout := flag.Duration("timeout", 500*time.Millisecond, "read timeout")
    flag.Parse()
    if *id == "" {
        fmt.Fprintln(os.Stderr, "--id required")
        os.Exit(2)
    }
    var since uint64
    stop := make(chan os.Signal, 1)
    signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
    done := int32(0)

    // Reader goroutine: follow PTY output and print to stdout
    go func() {
        dec := base64.StdEncoding
        for atomic.LoadInt32(&done) == 0 {
            body, _ := json.Marshal(api.PTYReadRequest{ID: *id, SinceSeq: since, MaxBytes: 1<<16, TimeoutMS: timeout.Milliseconds()})
            resp, err := http.Post(trimSlash(*server)+"/v1/pty/read", "application/json", bytes.NewReader(body))
            if err != nil {
                time.Sleep(100 * time.Millisecond)
                continue
            }
            var rr api.PTYReadResponse
            if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
                resp.Body.Close()
                time.Sleep(100 * time.Millisecond)
                continue
            }
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
    }()

    // Writer: read stdin and forward to PTY
    buf := make([]byte, 4096)
    for {
        select {
        case <-stop:
            atomic.StoreInt32(&done, 1)
            return
        default:
            n, err := os.Stdin.Read(buf)
            if n > 0 {
                b64 := base64.StdEncoding.EncodeToString(buf[:n])
                body, _ := json.Marshal(api.PTYSendRequest{ID: *id, DataB64: b64})
                _, _ = http.Post(trimSlash(*server)+"/v1/pty/send", "application/json", bytes.NewReader(body))
            }
            if err == io.EOF {
                time.Sleep(50 * time.Millisecond)
            }
        }
    }
}

func trimSlash(s string) string {
    if len(s) > 0 && s[len(s)-1] == '/' { return s[:len(s)-1] }
    return s
}

