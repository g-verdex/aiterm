package main

import (
    "flag"
    "fmt"
    "log"
    "net/http"

    "ai-terminal/internal/server"
)

func main() {
    addr := flag.String("addr", ":5011", "listen address")
    flag.Parse()

    srv := server.New()
    h := srv.Handler()
    log.Printf("aitermd listening on %s", *addr)
    if err := http.ListenAndServe(*addr, h); err != nil {
        fmt.Println("server error:", err)
    }
}
