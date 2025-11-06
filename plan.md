# AI Terminal (MCP‑Ready) — Project Plan

This document defines the scope, architecture, features, milestones, and testing strategy for building a deterministic, agent‑first terminal service usable stand‑alone and as an MCP tool provider. The service focuses on precise process control (non‑PTY and PTY), structured streaming I/O, strict security/policy, and optional human attach via tmux.

## Objectives
- Deterministic process execution with explicit exit codes, cwd, env, timing, and bounded I/O
- Robust PTY sessions with ordered chunk streaming, resize, and signals
- File‑system side‑channel for direct file reads/writes/listing (no TTY hacks)
- Policy and safety guardrails (timeouts, rlimits, output caps, allow/deny)
- Clean JSON‑RPC API (local server) and MCP tool mapping (1:1)
- Optional tmux bridge for human read‑only attach; agent uses structured APIs

## Non‑Goals (initial)
- Full remote orchestration across machines (single host scope first)
- Pixel‑level GUI automation; prefer programmatic/machine APIs
- Shell prompt parsing/screen scraping (avoid by design)

## Use Cases
- Run single commands reliably (compile, test, small scripts)
- Drive shells or REPLs via PTY deterministically
- Interact with editors/tools in non‑interactive modes (e.g., `vim -Es`)
- Integrate program‑specific machine interfaces (radare2/rizin via r2pipe/rzpipe)
- Provide human co‑view on agent sessions (tmux attach ‑r)

## High‑Level Architecture
- Core Go module `term` implementing:
  - Non‑PTY runner over `os/exec`
  - PTY sessions using `creack/pty` (forkpty), ring buffers for output
  - Policy layer (timeouts, rlimits, output caps, env/cwd isolation)
- JSON‑RPC/HTTP server `aitermd` exposing typed endpoints
- CLI client `aiterm` for local/manual and CI/E2E testing
- MCP adapter exposing tools that map 1:1 to server endpoints
- Optional tmux bridge for human attach (not used by agent path)

## API Surface (Server) — v1

All endpoints return structured results, with correlation IDs and durations. Errors carry stable codes and messages.

1) shell.run (non‑PTY)
- Input: `{ argv: string[], cwd?: string, env?: map<string,string>, timeout_ms?: number, stdin?: base64 }`
- Output: `{ rc: number, stdout: base64, stderr: base64, duration_ms: number, cwd: string, usage?: {cpu_ms, max_rss_kb} }`

2) pty.open
- Input: `{ argv: string[], rows?: number=40, cols?: number=120, cwd?: string, env?: map, timeout_ms?: number|null }`
- Output: `{ id: string, started_at: string }`

3) pty.send
- Input: `{ id: string, data: base64 }`
- Output: `{ bytes_written: number }`

4) pty.read
- Input: `{ id: string, since_seq?: uint64=0, max_bytes?: number=65536, timeout_ms?: number=1000, strip_ansi?: boolean=false }`
- Output: `{ chunks: [{ seq: uint64, stream: "stdout"|"stderr", data: base64, ts: string }], closed: boolean }`

5) pty.resize
- Input: `{ id: string, rows: number, cols: number }`
- Output: `{}`

6) pty.signal
- Input: `{ id: string, signal: "INT"|"TERM"|"HUP"|"KILL"|"QUIT" }`
- Output: `{}`

7) pty.close
- Input: `{ id: string }`
- Output: `{ rc?: number, duration_ms?: number }`

8) fs.readFile
- Input: `{ path: string, max_bytes?: number }`
- Output: `{ data: base64, size: number, truncated: boolean }`

9) fs.writeFile
- Input: `{ path: string, data: base64, mode?: string }`
- Output: `{ bytes: number }`

10) fs.list
- Input: `{ path: string }`
- Output: `{ entries: [{ name: string, type: "file"|"dir"|"link"|"other", mode: string, size: number }] }`

11) health.info
- Output: `{ version: string, go_version: string, uptime_s: number }`

## MCP Tool Mapping — v1

Tools map directly to server endpoints with identical parameters/results:
- `shell.run`
- `pty.open`, `pty.send`, `pty.read`, `pty.resize`, `pty.signal`, `pty.close`
- `fs.readFile`, `fs.writeFile`, `fs.list`
- `health.info`

Semantics:
- Strict argv arrays (no implicit shell). If shell is desired, caller passes `argv: ["/bin/bash","-lc", "..."]`.
- Default env isolation (`env -i`) unless env provided.
- ANSI off by default for non‑PTY; PTY raw unless `strip_ansi=true` at read.

## Security & Policy
- Timeouts: per call; PTY idle/read timeouts; hard kill with `SIGKILL` after grace period
- Output caps: max bytes per call/session; drop or truncate with explicit `truncated` flags
- Resource limits: `Setrlimit` for CPU time, address space (where supported), open files
- Allow/Deny lists (config):
  - Allow: full path prefixes or command names
  - Deny: dangerous binaries or patterns
- Env sanitization: redaction of sensitive env keys in logs; opt‑in inheritance
- CWD constraints: optional jail root (chroot/landlock in future)
- Audit: per‑call structured logs with argv, cwd, env keys (not values), durations, rc, byte counts

## Observability
- Structured logs (JSON) with correlation IDs
- Optional per‑session transcript (PTY) with size bounds
- Basic metrics: counts, durations, bytes, failures by endpoint; Prometheus export

## Optional Human Attach (tmux Bridge)
- Start PTY shell inside tmux or mirror PTY to tmux
- Tmux config hardening: disable alt‑screen, `escape-time 0`, `history-limit 200k`, `status off`
- User attach via socket: `tmux -S <sock> attach -t <sess> -r`
- Authorization: socket group/ACL; default read‑only
- Agent never scrapes tmux; uses core PTY APIs

## Program‑Specific Integrations (Phase 2)
- Radare2/rizin via pipes (r2pipe/rzpipe semantics)
  - Endpoint set: `r2.open`, `r2.cmd`, `r2.cmdj`, `r2.close`
  - Open flags: `-q0 -nn -e scr.color=false -e scr.prompt=false`
  - JSON outputs validated and bounded
- Git helper (optional): limited read‑only operations (status, log) under allowlist

## Current Status (Done)
- Core non‑PTY runner (`shell.run`) with timeouts/env/cwd/stdin capture
- PTY manager: open/send/read/resize/close with chunked streaming
- HTTP server `aitermd` endpoints for shell, pty, fs
- CLI `aiterm` including `run`, `pty-*`, and `pty-follow`
- Human bridge: tmux with interactive helper (`aiterm-bridge`) fallback to log tail
- Integration tests covering server, PTY flow, fs ops, tmux bridge, and r2/radare banner/counters

## Next Tasks (This Phase)
- Restructure tests under `tests/` as black‑box integration tests
  - Move existing tests from `internal/*` into `tests/`, adapting them to drive the built server/CLI
  - Keep skips for missing tools (tmux, r2/rizin)
- Add C test asset (simple crackme)
  - `tests/simpleprogram.c` compiled with `-g -O0 -fno-pie -no-pie`
  - Clear symbols and simple control flow (e.g., `check(const char*)`, `main`) for gdb targets
- GDB interactive E2E test
  - Start `aitermd` (helper in PATH), open PTY with `gdb -q ./simpleprogram`
  - Commands: `break main`, `run`, `info registers`, `si/ni`, `x/i $rip`, `bt`, `continue`, `quit`
  - Assertions: breakpoint hit text, register names (e.g., `rip`), disassembly line, backtrace frames
  - Skip if `gcc` or `gdb` unavailable
- Manual test recipe (docs)
  - Quick steps to rebuild, start server, build simpleprogram, and attach tmux bridge to a gdb session
- CI stability
  - Conservative timeouts; bounded read loops; ensure cleanup of processes/sockets

## Near‑Term (Optional)
- Health/metrics endpoint and “list bridges” endpoint to enumerate active bridges
- Basic allow/deny policy and output caps activation

## Testing Strategy
- Black‑box integration tests in `tests/` driving server/CLI
- PTY stability tests (resize, signals, caps, concurrency)
- Golden JSON responses for CLI and server endpoints (where helpful)
- Race detector and leak checks (goroutine/FD) in CI
- Fuzz tests for stream framing and argv/env handling
- Tool‑gated tests: r2/rizin, tmux, gdb/gcc (skipped if missing)

## Error Model
- Every error has: `code` (stable string), `message`, `details` (structured)
- Common codes: `ETIMEOUT`, `ECANCELED`, `ERLIMIT`, `EDENIED`, `EBADARGS`, `EIO`, `ENOTFOUND`, `ESESSIONCLOSED`

## Configuration
- YAML/TOML/JSON config file (env‑overridable):
  - Policy: allow/deny lists, timeouts, caps, rlimits
  - Logging level/format, metrics bind addr
  - Tmux bridge toggle and socket path

## Security Considerations
- No shell expansion unless explicitly requested
- Zero default env (`env -i`) with explicit allowlist pass‑through
- Redact sensitive env keys in logs (e.g., `TOKEN`, `SECRET`, `KEY`)
- Bounds on all untrusted inputs (argv length, env size, I/O bytes)

## Deliverables
- Restructured tests in `tests/` covering shell/pty/fs/bridge flows
- C crackme program and build script integrated into tests
- Automated gdb E2E test validating interactive debugging via PTY
- Updated docs: manual gdb-and-bridge quickstart

## Future Work
- Namespacing (landlock/chroot), cgroups for CPU/mem isolation
- Remote execution agents and multiplexed control plane
- Artifact store for large stdout/stderr spills
- Session recording/replay visualization

## Acceptance Criteria (MVP)
- `shell.run` and full PTY lifecycle pass integration tests on Linux
- Deterministic JSON outputs including rc, duration, and bounds
- Enforced timeouts and output caps verified by tests
- Minimal docs and CLI examples available and usable
