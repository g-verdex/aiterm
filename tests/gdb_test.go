package tests

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "io"
    "net/http"
    "os/exec"
    "path/filepath"
    "regexp"
    "runtime"
    "strings"
    "testing"
    "time"
)

func TestGDBInteractiveAgainstSimpleProgram(t *testing.T) {
    // Tool checks
    if _, err := exec.LookPath("gcc"); err != nil { t.Skip("gcc not found") }
    if _, err := exec.LookPath("gdb"); err != nil { t.Skip("gdb not found") }

    base, stop := startServer(t)
    defer stop()

    // Build the C program
    src := filepath.Join(modRoot(t), "tests", "assets", "simpleprogram.c")
    tmp := t.TempDir()
    bin := filepath.Join(tmp, "simpleprogram")
    args := []string{"-g", "-O0", "-fno-pie", "-no-pie", "-o", bin, src}
    // Some environments default PIE; ignore errors if flags unsupported
    cmd := exec.Command("gcc", args...)
    if out, err := cmd.CombinedOutput(); err != nil {
        t.Fatalf("gcc build failed: %v\n%s", err, out)
    }

    // Open gdb PTY session (disable debuginfod and confirmations to avoid prompts)
    oreq := ptyOpenReq{Argv: []string{"gdb", "-q",
        "-ex", "set debuginfod enabled off",
        "-ex", "set confirm off",
        "-ex", "set pagination off",
        bin,
    }, Rows: 30, Cols: 100}
    pb, _ := json.Marshal(oreq)
    resp, err := httpPost(base+"/v1/pty/open", pb)
    if err != nil { t.Fatal(err) }
    var po ptyOpenResp
    if err := json.Unmarshal(resp, &po); err != nil { t.Fatal(err) }

    // Drive gdb: break main; run; info registers; x/i $pc (or $rip);
    send := func(s string){
        b,_ := json.Marshal(ptySendReq{ID: po.ID, Data: base64.StdEncoding.EncodeToString([]byte(s))})
        _, _ = httpPost(base+"/v1/pty/send", b)
    }
    send("break main\n")
    send("run pancx\n")
    // Small delay so output is present
    time.Sleep(300*time.Millisecond)
    // registers
    if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
        send("info registers\n")
    }
    // step and disasm
    send("ni\n")
    if runtime.GOARCH == "amd64" { send("x/i $rip\n") } else { send("x/i $pc\n") }
    send("bt\n")

    // Read and assert
    since := uint64(0)
    acc := bytes.NewBuffer(nil)
    deadline := time.Now().Add(5*time.Second)
    for time.Now().Before(deadline) {
        rb,_ := json.Marshal(ptyReadReq{ID: po.ID, Since: since, Max: 1<<16, TimeoutMS: 300})
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
        // Require a breakpoint hit at main and visible gdb prompt
        brk := strings.Contains(s, "Temporary breakpoint") || strings.Contains(s, "Breakpoint 1")
        if brk && strings.Contains(s, "(gdb)") && strings.Contains(s, "main") {
            // Require some register or disassembly output
            regs := strings.Contains(s, " rip ") || strings.Contains(s, " pc ") || strings.Contains(s, "eax") || strings.Contains(s, "x0")
            dis := strings.Contains(s, "=>") || strings.Contains(s, "<main+")
            if regs || dis {
                break
            }
        }
    }
    out := stripANSI(acc.String())
    if !(strings.Contains(out, "Temporary breakpoint") || strings.Contains(out, "Breakpoint 1")) || !strings.Contains(out, "(gdb)") {
        t.Fatalf("gdb output missing breakpoint/prompt. got: %s", out)
    }
    if !(strings.Contains(out, "rip") || strings.Contains(out, "$rip") || strings.Contains(out, " pc") || strings.Contains(out, "$pc")) {
        t.Fatalf("gdb registers/disasm missing rip/pc. got: %s", out)
    }
}

// small HTTP helper
func httpPost(url string, body []byte) ([]byte, error) {
    req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    return io.ReadAll(resp.Body)
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
var escMisc = regexp.MustCompile(`\x1b\][0-9];.*?\x07`)

func stripANSI(s string) string {
    s = ansiRe.ReplaceAllString(s, "")
    s = escMisc.ReplaceAllString(s, "")
    s = strings.ReplaceAll(s, "\x1b[?2004h", "")
    s = strings.ReplaceAll(s, "\x1b[?2004l", "")
    return s
}
