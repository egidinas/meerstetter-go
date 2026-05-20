package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func main() {
	conn, err := mecom.Open(mecom.Endpoint{Network: "tcp", Address: "127.0.0.1:50000"}, 10*time.Second)
	if err != nil {
		fmt.Printf("Connection failed: %v\n", err)
		return
	}
	defer conn.Close()

	for _, addr := range []int{75, 76, 81, 84} {
		client := mecom.NewClient(conn, mecom.ClientConfig{Address: byte(addr), Timeout: 3 * time.Second})
		status, err := client.ReadInt32(context.Background(), 104, 1)
		if err != nil {
			fmt.Printf("Device %d Status Error: %v\n", addr, err)
		} else {
			fmt.Printf("Device %d Status = %d\n", addr, status)
		}
	}
}
