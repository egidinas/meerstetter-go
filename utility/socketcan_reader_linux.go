//go:build linux

package utility

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecomserver"
	"github.com/egidinas/meerstetter-go/socketcan"
)

func socketCANReaderForTarget(_ context.Context, device mecomserver.DeviceConfig, target string) (mecomReader, func(), error) {
	iface, node, ok, err := parseCANopenTarget(target, device.Metadata)
	if err != nil {
		return nil, nil, err
	}
	if ok {
		conn, err := socketcan.Open(iface)
		if err != nil {
			return nil, nil, err
		}
		client := mecom.NewCANopenClient(conn, mecom.ClientConfig{Address: node, Timeout: device.Queue.RequestTimeout})
		return client, func() { _ = conn.Close() }, nil
	}

	iface, addr, ok, err := parseSocketCANTarget(target, device.Metadata)
	if err != nil || !ok {
		return nil, nil, err
	}
	conn, err := socketcan.Open(iface)
	if err != nil {
		return nil, nil, err
	}
	client := mecom.NewCANClient(conn, mecom.ClientConfig{Address: addr, Timeout: device.Queue.RequestTimeout})
	return client, func() { _ = conn.Close() }, nil
}

func parseCANopenTarget(target string, metadata map[string]string) (string, byte, bool, error) {
	const prefix = "canopen:"
	return parseCANNodeTarget(target, metadata, prefix, "CANopen node", []string{"node", "node_id", "canopen_node", "addr"}, []string{"canopen_node", "node_id", "node", "addr"})
}

func parseSocketCANTarget(target string, metadata map[string]string) (string, byte, bool, error) {
	const prefix = "socketcan:"
	return parseCANNodeTarget(target, metadata, prefix, "MeCom address", []string{"addr"}, []string{"mecom_address", "address", "addr"})
}

func parseCANNodeTarget(target string, metadata map[string]string, prefix, label string, queryKeys, metadataKeys []string) (string, byte, bool, error) {
	target = strings.TrimSpace(target)
	if !strings.HasPrefix(target, prefix) {
		return "", 0, false, nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(target, prefix))
	if rest == "" {
		return "", 0, true, fmt.Errorf("%s target requires interface", strings.TrimSuffix(prefix, ":"))
	}
	iface := rest
	addrText := ""
	if strings.Contains(rest, "?") {
		parts := strings.SplitN(rest, "?", 2)
		iface = parts[0]
		values, err := url.ParseQuery(parts[1])
		if err != nil {
			return "", 0, true, err
		}
		for _, key := range queryKeys {
			if addrText = values.Get(key); strings.TrimSpace(addrText) != "" {
				break
			}
		}
	} else if strings.Contains(rest, "@") {
		parts := strings.SplitN(rest, "@", 2)
		iface = parts[0]
		addrText = parts[1]
	}
	iface = strings.TrimSpace(iface)
	if iface == "" {
		return "", 0, true, fmt.Errorf("%s target requires interface", strings.TrimSuffix(prefix, ":"))
	}
	if addrText == "" {
		if addrText = strings.TrimSpace(canTargetAddressHint(metadata, metadataKeys)); addrText == "" {
			return "", 0, true, fmt.Errorf("%s target requires node/address query, for example %scan0?%s=0x4b", strings.TrimSuffix(prefix, ":"), prefix, queryKeys[0])
		}
	}
	addr64, err := strconv.ParseUint(strings.TrimSpace(addrText), 0, 8)
	if err != nil {
		return "", 0, true, fmt.Errorf("invalid %s %q: %w", label, addrText, err)
	}
	if addr64 == 0 {
		return "", 0, true, fmt.Errorf("%s must be non-zero", label)
	}
	return iface, byte(addr64), true, nil
}

func canTargetAddressHint(metadata map[string]string, keys []string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(metadata[key]); value != "" {
			return value
		}
	}
	return ""
}
