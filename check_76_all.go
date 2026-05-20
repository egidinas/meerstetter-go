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
	
	for inst := 1; inst <= 4; inst++ {
		oe, _ := client.ReadInt32(context.Background(), 2010, inst)
		fmt.Printf("Instance %d Output Enable = %d\n", inst, oe)
	}
}
