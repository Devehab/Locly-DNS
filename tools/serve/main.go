// Command serve serves a directory over HTTP on 127.0.0.1. CI uses it to
// test the one-line installers against freshly built release archives.
//
//	go run ./tools/serve -dir dist -port 8765
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

func main() {
	dir := flag.String("dir", "dist", "directory to serve")
	port := flag.Int("port", 8765, "port on 127.0.0.1")
	flag.Parse()
	srv := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)),
		Handler:           http.FileServer(http.Dir(*dir)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("serving %s on http://%s", *dir, srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
