package mecomserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

const (
	defaultRequestTimeout        = 2 * time.Second
	defaultReconnectDelay        = 500 * time.Millisecond
	defaultClientIdleTimeout     = 30 * time.Second
	defaultCommandIdempotencyTTL = 30 * time.Second
	maxClientFrameBytes          = 4096
)

// DownstreamDial opens the single owned connection to a MeCom device.
type DownstreamDial func(context.Context) (net.Conn, string, error)

// Config configures a TCP MeCom device server. The server accepts many TCP
// clients and serializes requests through one downstream TCP or serial target.
type Config struct {
	Target            string
	Downstream        DownstreamDial
	RequestTimeout    time.Duration
	ReconnectDelay    time.Duration
	ClientIdleTimeout time.Duration
	TraceFrames       bool
	Logger            *log.Logger

	// statsRecorder is set by RouterConfig/HubConfig wrappers to surface
	// per-broker state. Direct callers of Serve do not need to set it.
	statsRecorder *brokerStatsRecorder
}

type request struct {
	ctx    context.Context
	frame  []byte
	result chan response
}

type response struct {
	frame []byte
	err   error
}

// ListenAndServe listens on listenAddr and serves until ctx is cancelled.
func ListenAndServe(ctx context.Context, listenAddr string, cfg Config) error {
	if strings.TrimSpace(listenAddr) == "" {
		return fmt.Errorf("mecomserver: listen address required")
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	return Serve(ctx, ln, cfg)
}

// Serve serializes all client frames through one downstream connection.
func Serve(ctx context.Context, ln net.Listener, cfg Config) error {
	if ln == nil {
		return fmt.Errorf("mecomserver: listener required")
	}
	if cfg.Downstream == nil {
		dial, err := DialTarget(cfg.Target)
		if err != nil {
			return err
		}
		cfg.Downstream = dial
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = defaultReconnectDelay
	}
	if cfg.ClientIdleTimeout <= 0 {
		cfg.ClientIdleTimeout = defaultClientIdleTimeout
	}

	requests := make(chan request, 256)
	go runBroker(ctx, cfg, requests)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if cfg.Logger != nil {
				cfg.Logger.Printf("accept failed: %v", err)
			}
			continue
		}
		go handleClient(ctx, conn, requests, cfg)
	}
}

// DialTarget creates a downstream dialer for a TCP or serial MeCom endpoint.
func DialTarget(target string) (DownstreamDial, error) {
	ep, ok := mecom.ParseTarget(strings.TrimSpace(target))
	if !ok {
		return nil, fmt.Errorf("mecomserver: target required")
	}
	return func(ctx context.Context) (net.Conn, string, error) {
		timeout := defaultRequestTimeout
		if deadline, ok := ctx.Deadline(); ok {
			if d := time.Until(deadline); d > 0 {
				timeout = d
			}
		}
		conn, err := mecom.Open(ep, timeout)
		return conn, ep.String(), err
	}, nil
}

func handleClient(ctx context.Context, conn net.Conn, requests chan<- request, cfg Config) {
	if cfg.ClientIdleTimeout <= 0 {
		cfg.ClientIdleTimeout = defaultClientIdleTimeout
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	handleClientWithSelector(ctx, conn, cfg.Logger, cfg.ClientIdleTimeout, cfg.RequestTimeout, cfg.TraceFrames, func([]byte) (chan<- request, error) {
		return requests, nil
	})
}

func handleClientWithSelector(ctx context.Context, conn net.Conn, logger *log.Logger, idleTimeout, requestTimeout time.Duration, traceFrames bool, selectRequests func([]byte) (chan<- request, error)) {
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	if logger != nil {
		logger.Printf("client connected remote=%s", conn.RemoteAddr())
		defer logger.Printf("client disconnected remote=%s", conn.RemoteAddr())
	}

	reader := bufio.NewReader(conn)
	for {
		if idleTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		frame, err := readBoundedFramePartial(reader, maxClientFrameBytes)
		if err != nil {
			if traceFrames && logger != nil {
				logger.Printf("client read failed remote=%s bytes=%s err=%v", conn.RemoteAddr(), describeBytes(frame), err)
			}
			return
		}
		if len(frame) == 0 {
			continue
		}
		if traceFrames && logger != nil {
			logger.Printf("client -> server remote=%s frame=%s", conn.RemoteAddr(), describeBytes(frame))
		}
		requests, err := selectRequests(frame)
		if err != nil {
			resp := deviceServerError(frame, err)
			if traceFrames && logger != nil {
				logger.Printf("server -> client remote=%s frame=%s error=%v", conn.RemoteAddr(), describeBytes(resp), err)
			}
			if err := writeClientFrame(conn, resp, idleTimeout); err != nil {
				return
			}
			continue
		}
		if requestTimeout <= 0 {
			requestTimeout = defaultRequestTimeout
		}
		reqCtx, cancelReq := context.WithTimeout(ctx, requestTimeout)
		result := make(chan response, 1)
		select {
		case requests <- request{ctx: reqCtx, frame: append([]byte(nil), frame...), result: result}:
		case <-reqCtx.Done():
			cancelReq()
			return
		case <-ctx.Done():
			cancelReq()
			return
		}
		select {
		case res := <-result:
			cancelReq()
			if res.err != nil {
				resp := deviceServerError(frame, res.err)
				if traceFrames && logger != nil {
					logger.Printf("server -> client remote=%s frame=%s error=%v", conn.RemoteAddr(), describeBytes(resp), res.err)
				}
				if err := writeClientFrame(conn, resp, idleTimeout); err != nil {
					return
				}
				continue
			}
			if traceFrames && logger != nil {
				logger.Printf("server -> client remote=%s frame=%s", conn.RemoteAddr(), describeBytes(res.frame))
			}
			if err := writeClientFrame(conn, res.frame, idleTimeout); err != nil {
				return
			}
		case <-reqCtx.Done():
			resp := deviceServerError(frame, reqCtx.Err())
			if traceFrames && logger != nil {
				logger.Printf("server -> client remote=%s frame=%s error=%v", conn.RemoteAddr(), describeBytes(resp), reqCtx.Err())
			}
			_ = writeClientFrame(conn, resp, idleTimeout)
			cancelReq()
			return
		case <-ctx.Done():
			cancelReq()
			return
		}
	}
}

func readBoundedFrame(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	frame, err := readBoundedFramePartial(reader, maxBytes)
	if err != nil {
		return nil, err
	}
	return frame, nil
}

func readBoundedFramePartial(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("mecomserver: max frame size must be positive")
	}
	var frame []byte
	for {
		part, err := reader.ReadSlice(mecom.FrameTerminator)
		frame = append(frame, part...)
		if len(frame) > maxBytes {
			return nil, fmt.Errorf("mecomserver: client frame exceeds %d bytes", maxBytes)
		}
		if err == nil {
			return frame, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return frame, err
		}
	}
}

func writeClientFrame(conn net.Conn, frame []byte, timeout time.Duration) error {
	if timeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	}
	_, err := conn.Write(frame)
	return err
}

func runBroker(ctx context.Context, cfg Config, requests <-chan request) {
	var conn net.Conn
	var reader *bufio.Reader
	var description string
	closeConn := func() {
		if conn != nil {
			_ = conn.Close()
			conn = nil
			reader = nil
		}
	}
	defer closeConn()

	for {
		select {
		case <-ctx.Done():
			return
		case req := <-requests:
			cfg.statsRecorder.markFrameIn()
			reqCtx := req.ctx
			if reqCtx == nil {
				reqCtx = ctx
			}
			select {
			case <-reqCtx.Done():
				req.result <- response{err: reqCtx.Err()}
				continue
			default:
			}
			if conn == nil {
				dialCtx, cancel := context.WithTimeout(reqCtx, cfg.RequestTimeout)
				nextConn, desc, err := cfg.Downstream(dialCtx)
				cancel()
				if err != nil {
					cfg.statsRecorder.markDisconnected(err)
					req.result <- response{err: err}
					sleepOrDone(ctx, cfg.ReconnectDelay)
					continue
				}
				conn = nextConn
				reader = bufio.NewReader(conn)
				description = desc
				cfg.statsRecorder.markConnected(description)
				if cfg.Logger != nil {
					cfg.Logger.Printf("downstream connected target=%s", description)
				}
			}
			resp, err := exchange(reqCtx, conn, reader, req.frame, cfg.RequestTimeout)
			if err != nil {
				if cfg.Logger != nil {
					cfg.Logger.Printf("downstream exchange failed target=%s err=%v", description, err)
				}
				closeConn()
				cfg.statsRecorder.markDisconnected(err)
				req.result <- response{err: err}
				continue
			}
			cfg.statsRecorder.markFrameOut()
			if cfg.TraceFrames && cfg.Logger != nil {
				cfg.Logger.Printf("downstream exchange target=%s tx=%s rx=%s", description, describeBytes(req.frame), describeBytes(resp))
			}
			req.result <- response{frame: resp}
		}
	}
}

func exchange(ctx context.Context, conn net.Conn, reader *bufio.Reader, frame []byte, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		select {
		case <-reqCtx.Done():
			select {
			case <-done:
				return
			default:
			}
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := reqCtx.Deadline(); ok {
		deadline = ctxDeadline
	}
	_ = conn.SetWriteDeadline(deadline)
	if _, err := conn.Write(frame); err != nil {
		if reqCtx.Err() != nil {
			return nil, reqCtx.Err()
		}
		return nil, err
	}
	_ = conn.SetReadDeadline(deadline)
	resp, err := reader.ReadBytes(mecom.FrameTerminator)
	if err != nil {
		if reqCtx.Err() != nil {
			return nil, reqCtx.Err()
		}
		return nil, err
	}
	return append([]byte(nil), resp...), nil
}

func describeBytes(b []byte) string {
	if len(b) == 0 {
		return "<none>"
	}
	const max = 512
	clipped := b
	suffix := ""
	if len(clipped) > max {
		clipped = clipped[:max]
		suffix = fmt.Sprintf("...(+%d bytes)", len(b)-max)
	}
	printable := make([]byte, 0, len(clipped))
	for _, c := range clipped {
		switch {
		case c == '\r':
			printable = append(printable, '\\', 'r')
		case c == '\n':
			printable = append(printable, '\\', 'n')
		case c >= 32 && c <= 126:
			printable = append(printable, c)
		default:
			printable = append(printable, '.')
		}
	}
	return fmt.Sprintf("len=%d ascii=%q hex=% X%s", len(b), string(printable), clipped, suffix)
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func deviceServerError(requestFrame []byte, err error) []byte {
	frame := []byte(strings.TrimSpace(string(requestFrame)))
	addr := "00"
	seq := "0000"
	if len(frame) >= 7 && frame[0] == '#' {
		addr = string(frame[1:3])
		seq = string(frame[3:7])
	}
	prefix := fmt.Sprintf("!%s%s-03", addr, seq)
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))
}
