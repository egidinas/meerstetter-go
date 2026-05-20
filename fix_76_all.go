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
	
	fmt.Println("Disabling Output on ALL instances of Device 76...")
	for inst := 1; inst <= 4; inst++ {
		client.WriteInt32(context.Background(), 2010, inst, 0)
		time.Sleep(50 * time.Millisecond)
	}
	
	status, _ := client.ReadInt32(context.Background(), 104, 1)
	fmt.Printf("Device 76 Status after disabling output: %d\n", status)

	if status != 3 {
		fmt.Println("Setting Voltage Limit to 60.0V on ALL instances...")
		for inst := 1; inst <= 4; inst++ {
			err := client.WriteFloat32(context.Background(), 2033, inst, 60.0)
			fmt.Printf("  Instance %d write result: %v\n", inst, err)
			time.Sleep(50 * time.Millisecond)
		}
	} else {
		fmt.Println("Device is STILL in RUN state, cannot write voltage limit!")
	}

	fmt.Println("Resetting Error on Device 76...")
	client.WriteInt32(context.Background(), 1084, 1, 1)

	status, _ = client.ReadInt32(context.Background(), 104, 1)
	fmt.Printf("Final Status: %d\n", status)
}
