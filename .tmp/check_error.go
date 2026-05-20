package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func main() {
	conn, _ := mecom.Open(mecom.Endpoint{Network: "tcp", Address: "127.0.0.1:50000"}, 5*time.Second)
	defer conn.Close()
	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 81, Timeout: 2 * time.Second})
	
	fmt.Println("Setting OutputEnable=0 for Instance 2...")
	client.WriteInt32(context.Background(), 2010, 2, 0)
	time.Sleep(500 * time.Millisecond)

	fmt.Println("Sending Error Reset for Instance 1...")
	client.WriteInt32(context.Background(), 1084, 1, 1)
	time.Sleep(500 * time.Millisecond)

	status, _ := client.ReadInt32(context.Background(), 104, 1)
	fmt.Printf("Instance 1 New Status: %d\n", status)
}
