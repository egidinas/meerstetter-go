package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func main() {
	conn, _ := mecom.Open(mecom.Endpoint{Network: "tcp", Address: "127.0.0.1:50000"}, 10*time.Second)
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 76, Timeout: 10 * time.Second})
	
	err := client.WriteInt32(context.Background(), 2010, 2, 0)
	fmt.Printf("Instance 2 Output Disable Result: %v\n", err)

	oe, _ := client.ReadInt32(context.Background(), 2010, 2)
	fmt.Printf("Instance 2 Output Enable = %d\n", oe)
}
