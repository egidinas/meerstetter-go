//go:build !linux

package utility

import (
	"context"
	"fmt"
	"strings"

	"github.com/egidinas/meerstetter-go/mecomserver"
)

func socketCANReaderForTarget(_ context.Context, _ mecomserver.DeviceConfig, target string) (mecomReader, func(), error) {
	target = strings.TrimSpace(strings.ToLower(target))
	if strings.HasPrefix(target, "socketcan:") || strings.HasPrefix(target, "canopen:") {
		return nil, nil, fmt.Errorf("socketcan/canopen targets require linux")
	}
	return nil, nil, nil
}
