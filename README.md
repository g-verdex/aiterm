AI Terminal (MCP‑Ready) — Headless Terminal + PTY Server/CLI

Overview
- Deterministic command execution (non‑PTY) and robust PTY sessions over a simple HTTP server.
- Human attach via tmux bridge (interactive helper) or programmatic streaming via CLI.
- Tested end‑to‑end with GDB and radare2/rizin driving a small debug‑built ELF.

Current Status
- Core implemented and covered by integration tests:
  - Non‑PTY runner: argv/env/cwd/stdin, exit code, stdout/stderr, timeout.
  - PTY manager: open, send, read (seq‑ordered chunks), resize, close.
  - HTTP server endpoints: /v1/shell/run, /v1/pty/{open,send,read,resize,close}, /v1/fs/{read,write,list}.
  - Tmux bridge: create/destroy/list; interactive helper (aiterm‑bridge) for live input/output.
  - CLI: aiterm (run, pty‑open/send/read/follow/resize/close, bridge‑list).
  - Integration tests: shell/pty basics, GDB interactive, radare2 interactive, tmux bridge + r2 output.

Build
- Requirements: Go 1.22+ (tested on 1.24), tmux (optional), gcc/gdb/radare2 (for tests), jq (for examples).
- Build server and CLI:
  - go build -o ./bin/aitermd ./cmd/aitermd
  - go build -o ./bin/aiterm ./cmd/aiterm
  - go build -o ./bin/aiterm-bridge ./cmd/aiterm-bridge

Run
- Start server (default port 8099 recommended for CLI QoL):
  - ./bin/aitermd -addr :8099
- Default CLI server:
  - If --server is omitted, CLI uses http://127.0.0.1:8099 (override with AITERM_SERVER env var).

CLI Usage
- Non‑PTY command:
  - ./bin/aiterm run -- /bin/echo hi
- PTY lifecycle:
  - Open:  ./bin/aiterm pty-open -- /bin/bash --noprofile --norc -i
  - Send:  ./bin/aiterm pty-send --id <ID> --data $'echo hello\n'
  - Read:  ./bin/aiterm pty-read --id <ID> --timeout 500ms
  - Follow: ./bin/aiterm pty-follow --id <ID>
  - Resize: ./bin/aiterm pty-resize --id <ID> --rows 40 --cols 120
  - Close:  ./bin/aiterm pty-close --id <ID>
- Bridges (human attach):
  - List:   ./bin/aiterm bridge-list
  - Create: curl -sS -H 'Content-Type: application/json' -d '{"id":"<ID>"}' http://127.0.0.1:8099/v1/bridge/tmux/create
  - Attach: tmux -S '/tmp/aiterm/tmux-<id>.sock' attach -t 'ai-<id>'

GDB Quickstart
- Build the sample target: gcc -g -O0 -fno-pie -no-pie -o tests/build/simpleprogram tests/assets/simpleprogram.c
- Open GDB PTY:
  - ./bin/aiterm pty-open -- gdb -q \
    -ex 'set debuginfod enabled off' -ex 'set confirm off' -ex 'set pagination off' \
    tests/build/simpleprogram
- Send basic commands:
  - ./bin/aiterm pty-send --id <ID> --data $'break main\nrun pancx\ninfo registers\nni\nx/i $rip\nbt\n'
- Create bridge and attach (optional) to interact live via tmux.

Radare2 Quickstart
- Open r2 PTY: ./bin/aiterm pty-open -- radare2 -q -e scr.color=false -e scr.prompt=false tests/build/simpleprogram
- Send analysis commands:
  - ./bin/aiterm pty-send --id <ID> --data $'e bin.relocs.apply=true\naaa\naflj\nizj\n'
- Create bridge and attach to view JSON outputs live in tmux.

Testing
- go test ./tests -v (skips tool‑gated tests if gdb/gcc/radare2/tmux are missing)
- The suite dynamically builds binaries, launches the server on a random port, and validates interactive flows.

Notes
- The HTTP server is intentionally simple (no global daemon management). CLI defaults to 127.0.0.1:8099 when --server is omitted.
- For remote or CI usage, set AITERM_SERVER to the full server URL.
