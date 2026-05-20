package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func main() {
	addresses := []byte{75, 76, 81, 84}
	
	conn, err := mecom.Open(mecom.Endpoint{Network: "tcp", Address: "127.0.0.1:50000"}, 5*time.Second)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	for _, addr := range addresses {
		fmt.Printf("\nChecking Device %d...\n", addr)
		
		client := mecom.NewClient(conn, mecom.ClientConfig{Address: addr, Timeout: 2 * time.Second})
		
		status, err := client.ReadInt32(context.Background(), 104, 1)
		if err != nil {
			fmt.Printf("  Error reading status: %v\n", err)
			continue
		}
		
		fmt.Printf("  Status: %d\n", status)
		if status == 2 {
			fmt.Printf("  Device %d is in ERROR state!\n", addr)
			
			for inst := 1; inst <= 4; inst++ {
				// Read current threshold
				threshold, err := client.ReadFloat32(context.Background(), 2033, inst)
				if err != nil {
					continue
				}
				fmt.Printf("  Instance %d Current Voltage Error Threshold: %f V\n", inst, threshold)
				
				// Set threshold to 35.0V
				newThreshold := float32(35.0)
				if threshold < 35.0 {
					fmt.Printf("  Increasing threshold on Instance %d to 35.0 V...\n", inst)
					for i := 0; i < 5; i++ {
						err = client.WriteFloat32(context.Background(), 2033, inst, newThreshold)
						if err == nil {
							break
						}
						time.Sleep(100 * time.Millisecond)
					}
				}
				
				// Clear Error on this instance
				fmt.Printf("  Sending Error Reset on Instance %d...\n", inst)
				for i := 0; i < 5; i++ {
					err = client.WriteInt32(context.Background(), 1084, inst, 1)
					if err == nil {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
			}
			if err != nil {
				fmt.Printf("  Error clearing error after retries: %v\n", err)
			}
			
			time.Sleep(500 * time.Millisecond)
			newStatus, _ := client.ReadInt32(context.Background(), 104, 1)
			fmt.Printf("  New Status: %d\n", newStatus)
		} else {
			fmt.Printf("  Device %d is NOT in error state.\n", addr)
		}
	}
}
