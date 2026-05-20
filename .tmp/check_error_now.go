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
	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 76, Timeout: 2 * time.Second})
	
	fmt.Println("Device 76 Error Status:")
	for i := 1; i <= 4; i++ {
		status, _ := client.ReadInt32(context.Background(), 104, i)
		errNum, _ := client.ReadInt32(context.Background(), 105, i)
		fmt.Printf("  Instance %d: Status=%d, ErrorNum=%d\n", i, status, errNum)
	}
}
