package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: unixbridge <sock-path>")
		os.Exit(2)
	}
	sockPath := os.Args[1]
	deadline := time.Now().Add(12 * time.Second)

	var conn net.Conn
	var err error
	for {
		conn, err = net.DialTimeout("unix", sockPath, 500*time.Millisecond)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "unixbridge: timed out dialing %s: %v\n", sockPath, err)
			os.Exit(1)
		}
		time.Sleep(60 * time.Millisecond)
	}
	defer conn.Close()

	_, _ = fmt.Fprintln(os.Stderr, "__READY__")

	go func() {
		_, _ = io.Copy(conn, os.Stdin)
		if uc, ok := conn.(*net.UnixConn); ok {
			_ = uc.CloseWrite()
		} else {
			_ = conn.Close()
		}
	}()

	_, _ = io.Copy(os.Stdout, conn)
}
