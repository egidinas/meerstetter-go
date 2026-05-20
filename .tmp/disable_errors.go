package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func main() {
	addresses := []byte{75, 76, 81, 84}
	
	conn, _ := mecom.Open(mecom.Endpoint{Network: "tcp", Address: "127.0.0.1:50000"}, 5*time.Second)
	defer conn.Close()

	for _, addr := range addresses {
		client := mecom.NewClient(conn, mecom.ClientConfig{Address: addr, Timeout: 2 * time.Second})
		fmt.Printf("Disabling errors on Device %d...\n", addr)
		
		for inst := 1; inst <= 4; inst++ {
			// Disable Output first to prevent instant re-trip
			client.WriteInt32(context.Background(), 2010, inst, 0)
			
			// Max out Voltage Error Threshold
			client.WriteFloat32(context.Background(), 2033, inst, 60.0)
			
			// Max out Current Error Threshold
			client.WriteFloat32(context.Background(), 2032, inst, 20.0)
			
			// Send Error Reset
			client.WriteInt32(context.Background(), 1084, inst, 1)
			
			time.Sleep(50 * time.Millisecond)
		}
		
		// Check Status
		status, _ := client.ReadInt32(context.Background(), 104, 1)
		fmt.Printf("Device %d New Status: %d\n", addr, status)
	}
}
