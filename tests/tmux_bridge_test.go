package tests

import (
    "encoding/json"
    "encoding/base64"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestTmuxBridgeRadare2Integration(t *testing.T) {
    if _, err := exec.LookPath("tmux"); err != nil { t.Skip("tmux not found") }
    if _, err := exec.LookPath("radare2"); err != nil { t.Skip("radare2 not found") }
    if _, err := exec.LookPath("gcc"); err != nil { t.Skip("gcc not found") }

    base, stop := startServer(t)
    defer stop()

    // Build target
    src := filepath.Join(modRoot(t), "tests", "assets", "simpleprogram.c")
    bin := filepath.Join(t.TempDir(), "simpleprogram")
    cmd := exec.Command("gcc", "-g", "-O0", "-fno-pie", "-no-pie", "-o", bin, src)
    _ , _ = cmd.CombinedOutput()
    if _, err := exec.Command("test", "-x", bin).CombinedOutput(); err != nil {
        // try without flags
        cmd = exec.Command("gcc", "-g", "-O0", "-o", bin, src)
        if out2, err2 := cmd.CombinedOutput(); err2 != nil {
            t.Fatalf("gcc failed: %v\n%s", err2, out2)
        }
    }

    // Open r2 PTY
    oreq := ptyOpenReq{Argv: []string{"radare2", "-q", "-e", "scr.color=false", "-e", "scr.prompt=false", bin}, Rows: 30, Cols: 100}
    pb,_ := json.Marshal(oreq)
    ob, err := httpPost(base+"/v1/pty/open", pb)
    if err != nil { t.Fatal(err) }
    var po ptyOpenResp
    if err := json.Unmarshal(ob, &po); err != nil { t.Fatal(err) }

    // Produce output
    _, _ = httpPost(base+"/v1/pty/send", mustJSON(ptySendReq{ID: po.ID, Data: b64("e bin.relocs.apply=true\n")}))
    _, _ = httpPost(base+"/v1/pty/send", mustJSON(ptySendReq{ID: po.ID, Data: b64("aaa\n")}))
    _, _ = httpPost(base+"/v1/pty/send", mustJSON(ptySendReq{ID: po.ID, Data: b64("aflj\n")}))
    _, _ = httpPost(base+"/v1/pty/send", mustJSON(ptySendReq{ID: po.ID, Data: b64("izj\n")}))

    // Create tmux bridge
    bcBody, err := httpPost(base+"/v1/bridge/tmux/create", mustJSON(map[string]string{"id": po.ID}))
    if err != nil { t.Fatal(err) }
    var bc struct{ Socket, Session, AttachHint string }
    if err := json.Unmarshal(bcBody, &bc); err != nil { t.Fatal(err) }
    if bc.Socket == "" || bc.Session == "" { t.Fatalf("bad bridge: %s", string(bcBody)) }

    // Verify session exists
    if err := exec.Command("tmux", "-S", bc.Socket, "has-session", "-t", bc.Session).Run(); err != nil {
        t.Fatalf("tmux session not found: %v", err)
    }

    // Produce output after bridge is up so helper prints to pane
    _, _ = httpPost(base+"/v1/pty/send", mustJSON(ptySendReq{ID: po.ID, Data: b64("aflj\n")}))
    _, _ = httpPost(base+"/v1/pty/send", mustJSON(ptySendReq{ID: po.ID, Data: b64("izj\n")}))

    // Capture pane and assert on content
    deadline := time.Now().Add(8 * time.Second)
    tgt := bc.Session + ":0.0"
    for time.Now().Before(deadline) {
        out, _ := exec.Command("tmux", "-S", bc.Socket, "capture-pane", "-pt", tgt).CombinedOutput()
        s := stripANSI(string(out))
        hasFuncs := strings.Contains(s, "\"name\"") && (strings.Contains(strings.ToLower(s), "main") || strings.Contains(s, "entry0") || strings.Contains(s, "sym.imp"))
        hasStrings := strings.Contains(s, "\"OK\"") || strings.Contains(s, "\"NOPE\"")
        if hasFuncs && hasStrings {
            return
        }
        time.Sleep(200 * time.Millisecond)
    }
    // final capture
    out, _ := exec.Command("tmux", "-S", bc.Socket, "capture-pane", "-pt", tgt).CombinedOutput()
    t.Fatalf("tmux pane did not show expected r2 output:\n%s", string(out))
}

func mustJSON(v interface{}) []byte { b,_ := json.Marshal(v); return b }

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
