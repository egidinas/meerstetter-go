//go:build !linux

package main

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	fmt.Fprintf(os.Stderr, "teccanprobe uses Linux SocketCAN and is not available on %s; use mecompoll over tcp:/serial: or a platform CAN adapter.\n", runtime.GOOS)
	os.Exit(2)
}
