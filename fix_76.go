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
	
	fmt.Println("Disabling Output on Device 76...")
	err := client.WriteInt32(context.Background(), 2010, 1, 0)
	fmt.Printf("Disable output result: %v\n", err)
	
	fmt.Println("Setting Voltage Limit to 60.0V on Device 76...")
	err = client.WriteFloat32(context.Background(), 2033, 1, 60.0)
	fmt.Printf("Write voltage limit result: %v\n", err)

	fmt.Println("Resetting Error on Device 76...")
	err = client.WriteInt32(context.Background(), 1084, 1, 1)
	fmt.Printf("Reset error result: %v\n", err)

	status, _ := client.ReadInt32(context.Background(), 104, 1)
	fmt.Printf("Final Status: %d\n", status)
}
