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
	
	vLimit, _ := client.ReadFloat32(context.Background(), 2033, 1)
	outEnable, _ := client.ReadInt32(context.Background(), 2010, 1)
	fmt.Printf("Device 76: Output Enable = %d, Voltage Limit = %f\n", outEnable, vLimit)
}
