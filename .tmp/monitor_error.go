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
	
	fmt.Println("Monitoring Device 81 Status and Error...")
	for i := 0; i < 30; i++ {
		status, _ := client.ReadInt32(context.Background(), 104, 1)
		errNum, _ := client.ReadInt32(context.Background(), 105, 1)
		fmt.Printf("[%s] Status: %d, ErrorNum: %d\n", time.Now().Format("15:04:05.000"), status, errNum)
		if status == 2 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}
