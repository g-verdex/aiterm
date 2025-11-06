package tests

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestRadare2InteractiveAgainstSimpleProgram(t *testing.T) {
    if _, err := exec.LookPath("radare2"); err != nil { t.Skip("radare2 not found") }
    if _, err := exec.LookPath("gcc"); err != nil { t.Skip("gcc not found") }

    dir := filepath.Clean("..")
    base, stop := startServer(t, dir)
    defer stop()

    // Build the C program
    src := filepath.Join(dir, "tests", "assets", "simpleprogram.c")
    bin := filepath.Join(t.TempDir(), "simpleprogram")
    cmd := exec.Command("gcc", "-g", "-O0", "-fno-pie", "-no-pie", "-o", bin, src)
    if out, err := cmd.CombinedOutput(); err != nil {
        // try without no-pie flags
        cmd = exec.Command("gcc", "-g", "-O0", "-o", bin, src)
        if out2, err2 := cmd.CombinedOutput(); err2 != nil {
            t.Fatalf("gcc failed: %v\n%s\n%s", err, out, out2)
        }
    }

    // Open radare2 PTY
    oreq := ptyOpenReq{Argv: []string{"radare2", "-q", "-e", "scr.color=false", "-e", "scr.prompt=false", bin}, Rows: 30, Cols: 100}
    pb,_ := json.Marshal(oreq)
    ob, err := httpPost(base+"/v1/pty/open", pb)
    if err != nil { t.Fatal(err) }
    var po ptyOpenResp
    if err := json.Unmarshal(ob, &po); err != nil { t.Fatal(err) }

    send := func(s string){
        b,_ := json.Marshal(ptySendReq{ID: po.ID, Data: base64.StdEncoding.EncodeToString([]byte(s))})
        _, _ = httpPost(base+"/v1/pty/send", b)
    }

    // Analyze and query (deep analysis to recover symbols)
    send("e bin.relocs.apply=true\n")
    send("aaa\n")
    send("aflj\n")
    send("izj\n")

    // Read until we observe function JSON with main and strings JSON containing OK/NOPE
    since := uint64(0)
    acc := bytes.NewBuffer(nil)
    deadline := time.Now().Add(6*time.Second)
    for time.Now().Before(deadline) {
        rb,_ := json.Marshal(ptyReadReq{ID: po.ID, Since: since, Max: 1<<16, TimeoutMS: 400})
        data, err := httpPost(base+"/v1/pty/read", rb)
        if err != nil { t.Fatal(err) }
        var rr ptyReadResp
        if err := json.Unmarshal(data, &rr); err != nil { t.Fatal(err) }
        for _, c := range rr.Chunks {
            b, _ := base64.StdEncoding.DecodeString(c.Data)
            acc.Write(b)
            since = c.Seq
        }
        s := stripANSI(acc.String())
        if strings.Contains(s, "\"name\"") && strings.Contains(strings.ToLower(s), "main") && (strings.Contains(s, "\"OK\"") || strings.Contains(s, "\"NOPE\"")) {
            break
        }
    }
    out := stripANSI(acc.String())
    if !(strings.Contains(out, "\"name\"") && strings.Contains(strings.ToLower(out), "main")) {
        t.Fatalf("radare2 aflj output missing function 'main': %s", out)
    }
    if !(strings.Contains(out, "\"OK\"") || strings.Contains(out, "\"NOPE\"")) {
        t.Fatalf("radare2 izj output missing strings OK/NOPE: %s", out)
    }
}
