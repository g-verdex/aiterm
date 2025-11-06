package tests

import (
    "encoding/base64"
    "encoding/json"
    "os/exec"
    "path/filepath"
    "regexp"
    "testing"
    "time"
)

// TestRadare2InteractivePrompt exercises r2 in interactive mode and asserts prompt and text output (not JSON).
func TestRadare2InteractivePrompt(t *testing.T) {
    if _, err := exec.LookPath("radare2"); err != nil { t.Skip("radare2 not found") }
    if _, err := exec.LookPath("gcc"); err != nil { t.Skip("gcc not found") }

    base, stop := startServer(t)
    defer stop()

    // Build target
    src := filepath.Join(modRoot(t), "tests", "assets", "simpleprogram.c")
    bin := filepath.Join(t.TempDir(), "simpleprogram")
    if out, err := exec.Command("gcc", "-g", "-O0", "-fno-pie", "-no-pie", "-o", bin, src).CombinedOutput(); err != nil {
        if out2, err2 := exec.Command("gcc", "-g", "-O0", "-o", bin, src).CombinedOutput(); err2 != nil {
            t.Fatalf("gcc failed: %v\n%s\n%s", err, string(out), string(out2))
        }
    }

    // Open r2 PTY interactively (no -q)
    oreq := ptyOpenReq{Argv: []string{"radare2", bin}, Rows: 30, Cols: 100}
    pb, _ := json.Marshal(oreq)
    ob, err := httpPost(base+"/v1/pty/open", pb)
    if err != nil { t.Fatal(err) }
    var po ptyOpenResp
    if err := json.Unmarshal(ob, &po); err != nil { t.Fatal(err) }

    send := func(s string){
        b,_ := json.Marshal(ptySendReq{ID: po.ID, Data: base64.StdEncoding.EncodeToString([]byte(s))})
        _, _ = httpPost(base+"/v1/pty/send", b)
    }
    // Resolve reloc warning; analyze; list functions; seek to entry0; print disasm
    send("e bin.relocs.apply=true\n")
    send("aaa\n")
    send("afl\n")
    send("s entry0\n")
    send("pd 5\n")

    // Read until prompt and some expected text lines appear
    promptRe := regexp.MustCompile(`\[0x[0-9a-fA-F]+\]>`)
    since := uint64(0)
    out := ""
    deadline := time.Now().Add(8*time.Second)
    for time.Now().Before(deadline) {
        rb,_ := json.Marshal(ptyReadReq{ID: po.ID, Since: since, Max: 1<<16, TimeoutMS: 800})
        data, err := httpPost(base+"/v1/pty/read", rb)
        if err != nil { t.Fatal(err) }
        var rr ptyReadResp
        if err := json.Unmarshal(data, &rr); err != nil { t.Fatal(err) }
        for _, c := range rr.Chunks {
            b, _ := base64.StdEncoding.DecodeString(c.Data)
            out += string(b)
            since = c.Seq
        }
        if promptRe.MatchString(out) && (containsAny(out, "entry0", "sym.imp", "main") && containsAny(out, "push", "mov", "call")) {
            return
        }
    }
    t.Fatalf("did not observe interactive prompt/output; got:\n%s", out)
}

func containsAny(s string, items ...string) bool {
    for _, it := range items { if regexp.MustCompile(regexp.QuoteMeta(it)).MatchString(s) { return true } }
    return false
}

