package term

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "time"
)

// RunRequest encapsulates a non-PTY command execution request.
type RunRequest struct {
    Argv    []string
    Cwd     string
    Env     map[string]string // exact environment. If nil, defaults to empty (env -i)
    Timeout time.Duration     // 0 means no timeout
    Stdin   []byte
}

// RunResult provides structured results from a completed process.
type RunResult struct {
    RC       int
    Stdout   []byte
    Stderr   []byte
    Duration time.Duration
    Cwd      string
}

// ShellRun executes a process without a PTY, capturing stdout/stderr and exit code deterministically.
func ShellRun(parentCtx context.Context, req RunRequest) (RunResult, error) {
    var res RunResult

    if len(req.Argv) == 0 || req.Argv[0] == "" {
        return res, errors.New("argv must not be empty")
    }

    // Context with optional timeout
    ctx := parentCtx
    var cancel context.CancelFunc
    if req.Timeout > 0 {
        ctx, cancel = context.WithTimeout(parentCtx, req.Timeout)
        defer cancel()
    }

    cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...)

    if req.Cwd != "" {
        cmd.Dir = req.Cwd
        res.Cwd = req.Cwd
    } else {
        // Capture the current working directory for the result
        if wd, err := os.Getwd(); err == nil {
            res.Cwd = wd
        }
    }

    // Build environment (env -i by default)
    if req.Env != nil {
        env := make([]string, 0, len(req.Env))
        for k, v := range req.Env {
            env = append(env, fmt.Sprintf("%s=%s", k, v))
        }
        cmd.Env = env
    } else {
        cmd.Env = []string{}
    }

    var stdoutBuf, stderrBuf bytes.Buffer
    cmd.Stdout = &stdoutBuf
    cmd.Stderr = &stderrBuf
    if req.Stdin != nil {
        cmd.Stdin = bytes.NewReader(req.Stdin)
    }

    start := time.Now()
    runErr := cmd.Run()
    res.Duration = time.Since(start)
    res.Stdout = stdoutBuf.Bytes()
    res.Stderr = stderrBuf.Bytes()

    // Exit code handling
    if runErr == nil {
        res.RC = 0
        return res, nil
    }
    // If context deadline exceeded, surface a typed error but still return captured output.
    if errors.Is(ctx.Err(), context.DeadlineExceeded) {
        // Try to get exit code if available (process may have been killed)
        if ee, ok := runErr.(*exec.ExitError); ok {
            res.RC = exitCodeFromError(ee)
        } else {
            res.RC = 124 // common timeout code
        }
        return res, fmt.Errorf("timeout after %s: %w", req.Timeout, runErr)
    }
    // Normal non-zero exit
    if ee, ok := runErr.(*exec.ExitError); ok {
        res.RC = exitCodeFromError(ee)
        return res, nil
    }
    // Other errors (e.g., binary not found)
    res.RC = 127
    return res, runErr
}

func exitCodeFromError(ee *exec.ExitError) int {
    // On Unix, extract WaitStatus
    if status, ok := ee.Sys().(interface{ ExitStatus() int }); ok {
        return status.ExitStatus()
    }
    return 1
}

