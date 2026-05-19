package mecom

import (
	"context"
	"io"
	"log"
	"net"
	"sync"
)

// ProxyServer allows external MeCom tools (like TEC CoSo) to share the transport
// managed by a Client. It listens on a TCP port and forwards frames to the Client's
// underlying transport, injecting them between the Client's own requests.
type ProxyServer struct {
	Addr   string
	Client *Client

	listener net.Listener
	wg       sync.WaitGroup
	quit     chan struct{}
}

func NewProxyServer(addr string, client *Client) *ProxyServer {
	return &ProxyServer{
		Addr:   addr,
		Client: client,
		quit:   make(chan struct{}),
	}
}

func (s *ProxyServer) Start() error {
	l, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	s.listener = l
	s.wg.Add(1)
	go s.serve()
	return nil
}

func (s *ProxyServer) Stop() {
	close(s.quit)
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
}

func (s *ProxyServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				log.Printf("proxy: accept error: %v", err)
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *ProxyServer) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	log.Printf("proxy: new connection from %v", conn.RemoteAddr())

	for {
		select {
		case <-s.quit:
			return
		default:
			// Read one frame from the external tool
			frame, err := readFrame(conn)
			if err != nil {
				if err != io.EOF {
					log.Printf("proxy: read error from %v: %v", conn.RemoteAddr(), err)
				}
				return
			}

			// Interleave this frame into the main Client's transport.
			// We use a context to ensure we don't hang if the transport is stuck.
			ctx, cancel := context.WithTimeout(context.Background(), s.Client.timeout)
			resp, err := s.Client.roundTripRaw(ctx, frame)
			cancel()

			if err != nil {
				log.Printf("proxy: transport error: %v", err)
				return
			}

			// Send the response back to the external tool
			if _, err := conn.Write(resp); err != nil {
				log.Printf("proxy: write error to %v: %v", conn.RemoteAddr(), err)
				return
			}
		}
	}
}
