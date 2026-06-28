package client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// Session represents the active network connection state session.
type Session struct {
	conn           net.Conn
	responseChan   chan string
	disconnectChan chan struct{}
	connectedAt    time.Time
}

// NewSession instantiates a new Session.
func NewSession(conn net.Conn) *Session {
	return &Session{
		conn:           conn,
		responseChan:   make(chan string, 100),
		disconnectChan: make(chan struct{}),
		connectedAt:    time.Now(),
	}
}

// Client manages a persistent TCP connection to the Quectel RGMII AT interface.
type Client struct {
	addr        string
	mu          sync.Mutex
	session     *Session
	urcChan     chan string
	connectChan chan struct{}
	Debug       bool
}

// NewClient creates a new persistent RGMII Client.
func NewClient(addr string) *Client {
	return &Client{
		addr:        addr,
		urcChan:     make(chan string, 100),
		connectChan: make(chan struct{}, 1),
		Debug:       false,
	}
}

// Start kicks off the background connection manager.
func (c *Client) Start(ctx context.Context) {
	go c.reconnectLoop(ctx)
}

// IsConnected returns whether the client has an active session.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session != nil
}

// GetUptime returns the duration of the current connection session, or 0 if disconnected.
func (c *Client) GetUptime() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return 0
	}
	return time.Since(c.session.connectedAt)
}

// URCChan returns the channel for receiving unsolicited notifications.
func (c *Client) URCChan() <-chan string {
	return c.urcChan
}

func (c *Client) reconnectLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		slog.Info("Connecting to modem", "address", c.addr)
		conn, err := net.DialTimeout("tcp", c.addr, 5*time.Second)
		if err != nil {
			slog.Error("Modem connection failed", "error", err, "retry_after", "3s")
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		slog.Info("TCP socket established with modem", "address", c.addr)
		session := NewSession(conn)

		c.mu.Lock()
		c.session = session
		c.mu.Unlock()

		// Signal connection success
		select {
		case c.connectChan <- struct{}{}:
		default:
		}

		// Run reader loop (blocks until EOF / socket read error)
		c.readerLoop(session)

		c.mu.Lock()
		if c.session == session {
			c.session = nil
		}
		c.mu.Unlock()

		slog.Warn("Connection to modem lost; initiating reconnect")
	}
}

func (c *Client) readerLoop(s *Session) {
	defer close(s.disconnectChan)
	defer s.conn.Close()

	buf := make([]byte, 0, 4096)
	readBuf := make([]byte, 2048)

	for {
		n, err := s.conn.Read(readBuf)
		if err != nil {
			break
		}

		if n > 0 {
			if c.Debug {
				slog.Debug("AT RECV", "raw", cleanQuote(string(readBuf[:n])))
			}
			buf = append(buf, readBuf[:n]...)

			// Parse frames in buf
			for len(buf) >= 3 {
				// Re-sync if header byte is not valid RGMII header
				if !isValidHeader(buf[0]) {
					idx := -1
					for i, b := range buf {
						if isValidHeader(b) {
							idx = i
							break
						}
					}
					if idx == -1 {
						buf = nil
						break
					}
					buf = buf[idx:]
					if len(buf) < 3 {
						break
					}
				}

				payloadLen := (int(buf[1]) << 8) | int(buf[2])
				frameLen := 3 + payloadLen

				if len(buf) < frameLen {
					// Incomplete frame, wait for more data
					break
				}

				header := buf[0]
				payload := buf[3:frameLen]
				payloadStr := string(payload)

				if header == 0xe0 {
					// Route unsolicited notifications (URCs)
					select {
					case c.urcChan <- payloadStr:
					default:
					}
					slog.Info("Modem URC received", "payload", strings.TrimSpace(payloadStr))
				} else {
					// Route command response frames
					select {
					case s.responseChan <- payloadStr:
					default:
					}
				}

				buf = buf[frameLen:]
			}
		}
	}
}

// SendCommand sends a command (adding standard line endings if needed) and waits for response under a timeout.
func (c *Client) SendCommand(cmd string, timeout time.Duration) (string, error) {
	if !strings.HasSuffix(cmd, "\r\n") {
		if strings.HasSuffix(cmd, "\r") {
			cmd = cmd + "\n"
		} else {
			cmd = cmd + "\r\n"
		}
	}
	return c.sendCommand(cmd, timeout)
}

// SendRawCommand sends a raw command (without suffix processing) and waits for response under a timeout.
func (c *Client) SendRawCommand(cmd string, timeout time.Duration) (string, error) {
	return c.sendCommand(cmd, timeout)
}

func (c *Client) sendCommand(cmd string, timeout time.Duration) (string, error) {
	if len(cmd) > 2048 {
		return "", fmt.Errorf("command length %d exceeds maximum limit of 2048 bytes", len(cmd))
	}

	c.mu.Lock()
	s := c.session
	if s == nil {
		c.mu.Unlock()
		return "", fmt.Errorf("modem connection offline")
	}
	c.mu.Unlock()

	// Drain any stale messages from response buffer
	for len(s.responseChan) > 0 {
		<-s.responseChan
	}

	// Frame the request: [0xa4][len_high][len_low][payload]
	cmdBytes := []byte(cmd)
	cmdLen := len(cmdBytes)
	req := make([]byte, 3+cmdLen)
	req[0] = 0xa4
	req[1] = byte((cmdLen >> 8) & 0xff)
	req[2] = byte(cmdLen & 0xff)
	copy(req[3:], cmdBytes)

	// Send payload
	if err := s.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return "", fmt.Errorf("set write deadline failed: %w", err)
	}
	if c.Debug {
		slog.Debug("AT SEND", "raw", cleanQuote(string(req)))
	}
	if _, err := s.conn.Write(req); err != nil {
		return "", fmt.Errorf("write command failed: %w", err)
	}

	var output strings.Builder
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case frame := <-s.responseChan:
			output.WriteString(frame)
			if isTerminalResponse(frame) {
				return output.String(), nil
			}
		case <-s.disconnectChan:
			return output.String(), fmt.Errorf("modem connection dropped during execution")
		case <-timer.C:
			return output.String(), fmt.Errorf("timeout waiting for command response")
		}
	}
}

func isTerminalResponse(s string) bool {
	trimmed := strings.TrimSpace(s)
	if strings.HasSuffix(trimmed, "OK") {
		return true
	}
	if strings.HasSuffix(trimmed, "ERROR") {
		return true
	}
	if strings.Contains(trimmed, "+CME ERROR:") {
		return true
	}
	if strings.Contains(trimmed, "+CMS ERROR:") {
		return true
	}
	return false
}

func isValidHeader(b byte) bool {
	return b == 0xa4 || b == 0xa0 || b == 0xe0 || b == 0xe4
}

func cleanQuote(s string) string {
	return s
}

type ClientInteractive struct {
	client *Client
	s      *Session
}

func (ci *ClientInteractive) Write(data string) error {
	if err := ci.s.conn.SetWriteDeadline(time.Now().Add(10*time.Second)); err != nil {
		return fmt.Errorf("set write deadline failed: %w", err)
	}
	if ci.client.Debug {
		slog.Debug("AT SEND", "raw", cleanQuote(data))
	}
	if _, err := ci.s.conn.Write([]byte(data)); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	return nil
}

func (ci *ClientInteractive) WriteCmd(cmd string) error {
	cmdBytes := []byte(cmd)
	cmdLen := len(cmdBytes)
	req := make([]byte, 3+cmdLen)
	req[0] = 0xa4
	req[1] = byte((cmdLen >> 8) & 0xff)
	req[2] = byte(cmdLen & 0xff)
	copy(req[3:], cmdBytes)

	if err := ci.s.conn.SetWriteDeadline(time.Now().Add(10*time.Second)); err != nil {
		return fmt.Errorf("set write deadline failed: %w", err)
	}
	if ci.client.Debug {
		slog.Debug("AT SEND", "raw", cleanQuote(string(req)))
	}
	if _, err := ci.s.conn.Write(req); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	return nil
}

func (ci *ClientInteractive) ReadFrame(timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case frame := <-ci.s.responseChan:
		return frame, nil
	case <-timer.C:
		return "", fmt.Errorf("timeout waiting for frame")
	}
}

func (ci *ClientInteractive) Close() error {
	return nil
}

// StartInteractive begins an interactive session on the client connection.
func (c *Client) StartInteractive() (*ClientInteractive, error) {
	c.mu.Lock()
	s := c.session
	if s == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("modem connection offline")
	}
	c.mu.Unlock()

	// Drain any stale messages from response buffer
	for len(s.responseChan) > 0 {
		<-s.responseChan
	}

	return &ClientInteractive{client: c, s: s}, nil
}
