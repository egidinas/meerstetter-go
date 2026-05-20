package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func writeFloat32Retry(client *mecom.Client, id, inst int, val float32) {
	for i := 0; i < 10; i++ {
		err := client.WriteFloat32(context.Background(), id, inst, val)
		if err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Printf("Failed to write %d:%d after retries\n", id, inst)
}

func writeInt32Retry(client *mecom.Client, id, inst int, val int32) {
	for i := 0; i < 10; i++ {
		err := client.WriteInt32(context.Background(), id, inst, val)
		if err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Printf("Failed to write %d:%d after retries\n", id, inst)
}

func main() {
	addresses := []byte{75, 76, 81, 84}
	
	conn, _ := mecom.Open(mecom.Endpoint{Network: "tcp", Address: "127.0.0.1:50000"}, 5*time.Second)
	defer conn.Close()

	for _, addr := range addresses {
		client := mecom.NewClient(conn, mecom.ClientConfig{Address: addr, Timeout: 2 * time.Second})
		fmt.Printf("Disabling errors on Device %d...\n", addr)
		
		for inst := 1; inst <= 4; inst++ {
			writeInt32Retry(client, 2010, inst, 0)
			writeFloat32Retry(client, 2033, inst, 60.0)
			writeFloat32Retry(client, 2032, inst, 20.0)
			writeInt32Retry(client, 1084, inst, 1)
		}
		
		time.Sleep(100 * time.Millisecond)
		status, _ := client.ReadInt32(context.Background(), 104, 1)
		fmt.Printf("Device %d New Status: %d\n", addr, status)
	}
}
