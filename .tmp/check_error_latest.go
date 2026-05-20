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
	
	for i := 1; i <= 4; i++ {
		temp, _ := client.ReadFloat32(context.Background(), 1000, i)
		min, _ := client.ReadFloat32(context.Background(), 4034, i)
		max, _ := client.ReadFloat32(context.Background(), 4035, i)
		sink, _ := client.ReadFloat32(context.Background(), 1001, i)
		vLimit, _ := client.ReadFloat32(context.Background(), 5013, i)
		fmt.Printf("Instance %d: ObjTemp=%f, Min=%f, Max=%f, SinkTemp=%f, VolLimit=%f\n", i, temp, min, max, sink, vLimit)
	}
}
