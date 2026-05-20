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
	
	fmt.Println("Sending Hardware Reset (RS) to Device 76...")
	err := client.Reset(context.Background())
	fmt.Printf("Reset command result: %v\n", err)
	
	fmt.Println("Waiting for device to reboot...")
	time.Sleep(5 * time.Second)

	fmt.Println("Setting Thresholds to max...")
	for inst := 1; inst <= 4; inst++ {
		client.WriteFloat32(context.Background(), 2033, inst, 60.0)
		client.WriteFloat32(context.Background(), 2032, inst, 20.0)
	}

	status, _ := client.ReadInt32(context.Background(), 104, 1)
	fmt.Printf("Device 76 Status after reboot: %d\n", status)
}
