package api

type ShellRunRequest struct {
    Argv      []string          `json:"argv"`
    Cwd       string            `json:"cwd,omitempty"`
    Env       map[string]string `json:"env,omitempty"`
    TimeoutMS int64             `json:"timeout_ms,omitempty"`
    StdinB64  string            `json:"stdin,omitempty"`
}

type ShellRunResponse struct {
    RC         int    `json:"rc"`
    StdoutB64  string `json:"stdout"`
    StderrB64  string `json:"stderr"`
    DurationMS int64  `json:"duration_ms"`
    Cwd        string `json:"cwd"`
    Error      string `json:"error,omitempty"`
}

type PTYOpenRequest struct {
    Argv []string          `json:"argv"`
    Rows int               `json:"rows,omitempty"`
    Cols int               `json:"cols,omitempty"`
    Cwd  string            `json:"cwd,omitempty"`
    Env  map[string]string `json:"env,omitempty"`
}

type PTYOpenResponse struct {
    ID string `json:"id"`
}

type PTYSendRequest struct {
    ID      string `json:"id"`
    DataB64 string `json:"data"`
}

type PTYSendResponse struct {
    BytesWritten int `json:"bytes_written"`
}

type PTYReadRequest struct {
    ID        string `json:"id"`
    SinceSeq  uint64 `json:"since_seq,omitempty"`
    MaxBytes  int    `json:"max_bytes,omitempty"`
    TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

type PTYChunk struct {
    Seq   uint64 `json:"seq"`
    Data  string `json:"data"` // base64
    Ts    int64  `json:"ts_ms"`
    Stream string `json:"stream"`
}

type PTYReadResponse struct {
    Chunks []PTYChunk `json:"chunks"`
    Closed bool       `json:"closed"`
}

type PTYResizeRequest struct {
    ID   string `json:"id"`
    Rows int    `json:"rows"`
    Cols int    `json:"cols"`
}

type PTYCloseRequest struct {
    ID string `json:"id"`
}

type FSReadRequest struct {
    Path     string `json:"path"`
    MaxBytes int    `json:"max_bytes,omitempty"`
}

type FSReadResponse struct {
    DataB64   string `json:"data"`
    Size      int64  `json:"size"`
    Truncated bool   `json:"truncated"`
}

type FSWriteRequest struct {
    Path string `json:"path"`
    Data string `json:"data"` // base64
    Mode string `json:"mode,omitempty"`
}

type FSWriteResponse struct {
    Bytes int `json:"bytes"`
}

type FSListRequest struct {
    Path string `json:"path"`
}

type FSEntry struct {
    Name string `json:"name"`
    Type string `json:"type"`
    Mode string `json:"mode"`
    Size int64  `json:"size"`
}

type FSListResponse struct {
    Entries []FSEntry `json:"entries"`
}

type BridgeTmuxCreateRequest struct {
    ID string `json:"id"`
}

type BridgeTmuxCreateResponse struct {
    Socket     string `json:"socket"`
    Session    string `json:"session"`
    AttachHint string `json:"attach_hint"`
    LogPath    string `json:"log_path"`
}

type BridgeTmuxDestroyRequest struct {
    Socket  string `json:"socket"`
    Session string `json:"session"`
}

type BridgeTmuxEntry struct {
    ID         string `json:"id"`
    Socket     string `json:"socket"`
    Session    string `json:"session"`
    AttachHint string `json:"attach_hint"`
    LogPath    string `json:"log_path,omitempty"`
    Alive      bool   `json:"alive"`
}

type BridgeTmuxListResponse struct {
    Bridges []BridgeTmuxEntry `json:"bridges"`
}
