package tests

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "net"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

type shellRunReq struct{
    Argv []string `json:"argv"`
}
type shellRunResp struct{
    RC int `json:"rc"`
    Stdout string `json:"stdout"`
}

type ptyOpenReq struct{
    Argv []string `json:"argv"`
    Rows int `json:"rows"`
    Cols int `json:"cols"`
    Env map[string]string `json:"env,omitempty"`
}
type ptyOpenResp struct{ ID string `json:"id"` }
type ptySendReq struct{ ID string `json:"id"`; Data string `json:"data"` }
type ptyReadReq struct{ ID string `json:"id"`; Since uint64 `json:"since_seq"`; Max int `json:"max_bytes"`; TimeoutMS int64 `json:"timeout_ms"` }
type ptyReadResp struct{ Chunks []struct{ Seq uint64 `json:"seq"`; Data string `json:"data"` } `json:"chunks"`; Closed bool `json:"closed"` }

func buildBinaries(t *testing.T, dir string) (aitermd, aiterm, bridge string) {
    t.Helper()
    run := func(out, pkg string){
        cmd := exec.Command("go","build","-o",out,pkg)
        cmd.Dir = dir
        cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
        outb, err := cmd.CombinedOutput()
        if err != nil { t.Fatalf("build %s: %v\n%s", pkg, err, outb) }
    }
    bindir := filepath.Join(dir, "bin")
    _ = os.MkdirAll(bindir, 0o755)
    aitermd = filepath.Join(bindir, "aitermd")
    aiterm = filepath.Join(bindir, "aiterm")
    bridge = filepath.Join(bindir, "aiterm-bridge")
    run(aitermd, "./cmd/aitermd")
    run(aiterm, "./cmd/aiterm")
    run(bridge, "./cmd/aiterm-bridge")
    return
}

func pickPort(t *testing.T) string {
    t.Helper()
    l, err := net.Listen("tcp", ":0")
    if err != nil { t.Fatal(err) }
    addr := l.Addr().String()
    _ = l.Close()
    _, port, _ := net.SplitHostPort(addr)
    return port
}

func startServer(t *testing.T, dir string) (baseURL string, stop func()) {
    t.Helper()
    aitermd, _, _ := buildBinaries(t, dir)
    port := pickPort(t)
    cmd := exec.Command(aitermd, "-addr", ":"+port)
    cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s:%s", filepath.Join(dir,"bin"), os.Getenv("PATH")))
    cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
    if err := cmd.Start(); err != nil { t.Fatalf("start server: %v", err) }
    baseURL = "http://127.0.0.1:"+port
    // wait for health via shell.run
    deadline := time.Now().Add(5*time.Second)
    for time.Now().Before(deadline) {
        if pingShellRun(baseURL) { break }
        time.Sleep(100*time.Millisecond)
    }
    if !pingShellRun(baseURL) { t.Fatalf("server not ready on %s", baseURL) }
    stop = func(){ _ = cmd.Process.Kill(); _ = cmd.Wait() }
    return
}

func pingShellRun(base string) bool {
    b,_ := json.Marshal(shellRunReq{Argv: []string{"/bin/echo","ok"}})
    resp, err := http.Post(base+"/v1/shell/run","application/json", bytes.NewReader(b))
    if err != nil { return false }
    resp.Body.Close(); return true
}

func TestShellRunAndPTYFlow(t *testing.T){
    dir := filepath.Clean("..")
    base, stop := startServer(t, dir)
    defer stop()
    // shell.run
    b,_ := json.Marshal(shellRunReq{Argv: []string{"/bin/echo","ok"}})
    resp, err := http.Post(base+"/v1/shell/run","application/json", bytes.NewReader(b))
    if err != nil { t.Fatal(err) }
    var out shellRunResp
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { t.Fatal(err) }
    resp.Body.Close()
    if out.RC != 0 { t.Fatalf("rc=%d", out.RC) }
    dec, _ := base64.StdEncoding.DecodeString(out.Stdout)
    if strings.TrimSpace(string(dec)) != "ok" { t.Fatalf("stdout=%q", dec) }

    // PTY: bash echo
    oreq := ptyOpenReq{Argv: []string{"/bin/bash","--noprofile","--norc","-i"}, Rows:24, Cols: 80, Env: map[string]string{"TERM":"dumb","PS1":""}}
    pb,_ := json.Marshal(oreq)
    resp2, err := http.Post(base+"/v1/pty/open","application/json", bytes.NewReader(pb))
    if err != nil { t.Fatal(err) }
    var po ptyOpenResp
    if err := json.NewDecoder(resp2.Body).Decode(&po); err != nil { t.Fatal(err) }
    resp2.Body.Close()
    // send echo and exit
    sb,_ := json.Marshal(ptySendReq{ID: po.ID, Data: base64.StdEncoding.EncodeToString([]byte("echo hi_from_test\nexit\n"))})
    _, _ = http.Post(base+"/v1/pty/send","application/json", bytes.NewReader(sb))
    // read loop
    since := uint64(0)
    acc := ""
    for i:=0;i<20;i++{
        rb,_ := json.Marshal(ptyReadReq{ID: po.ID, Since: since, Max: 1<<16, TimeoutMS: 300})
        resp3, err := http.Post(base+"/v1/pty/read","application/json", bytes.NewReader(rb))
        if err != nil { t.Fatal(err) }
        var rr ptyReadResp
        if err := json.NewDecoder(resp3.Body).Decode(&rr); err != nil { t.Fatal(err) }
        resp3.Body.Close()
        for _, c := range rr.Chunks {
            b, _ := base64.StdEncoding.DecodeString(c.Data)
            acc += string(b)
            since = c.Seq
        }
        if strings.Contains(acc, "hi_from_test") { break }
        time.Sleep(50*time.Millisecond)
    }
    if !strings.Contains(acc, "hi_from_test") {
        t.Fatalf("pty output missing token: %q", acc)
    }
}
