package client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"rgmii/commands"
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
	OnConnect   func()
	cmdMu       sync.Mutex // Serializes all commands and interactive sessions
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
		var dialer net.Dialer
		dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
		conn, err := dialer.DialContext(dialCtx, "tcp", c.addr)
		dialCancel()
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

		c.mu.Lock()
		onConnect := c.OnConnect
		c.mu.Unlock()
		if onConnect != nil {
			go onConnect()
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
				slog.Debug("AT RECV", "raw", string(readBuf[:n]))
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

	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()

	c.mu.Lock()
	s := c.session
	if s == nil {
		c.mu.Unlock()
		return "", fmt.Errorf("modem connection offline")
	}
	c.mu.Unlock()

	// Drain any stale messages from response buffer
	for {
		select {
		case <-s.responseChan:
		default:
			goto drained
		}
	}
drained:

	// Frame the request: [0xa4][len_high][len_low][payload]
	req := framePayload(0xa4, []byte(cmd))

	// Send payload
	if err := s.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return "", fmt.Errorf("set write deadline failed: %w", err)
	}
	if c.Debug {
		slog.Debug("AT SEND", "raw", string(req))
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
			if commands.IsTerminalResponse(frame) {
				return output.String(), nil
			}
		case <-s.disconnectChan:
			return output.String(), fmt.Errorf("modem connection dropped during execution")
		case <-timer.C:
			return output.String(), fmt.Errorf("timeout waiting for command response")
		}
	}
}

func isValidHeader(b byte) bool {
	return b == 0xa4 || b == 0xa0 || b == 0xe0 || b == 0xe4
}

func framePayload(header byte, payload []byte) []byte {
	payloadLen := len(payload)
	frame := make([]byte, 3+payloadLen)
	frame[0] = header
	frame[1] = byte((payloadLen >> 8) & 0xff)
	frame[2] = byte(payloadLen & 0xff)
	copy(frame[3:], payload)
	return frame
}

type ClientInteractive struct {
	client      *Client
	s           *Session
	closeOnce   sync.Once
	idleTimer   *time.Timer
	idleTimeout time.Duration
}

func (ci *ClientInteractive) resetTimer() {
	if ci.idleTimer != nil {
		ci.idleTimer.Reset(ci.idleTimeout)
	}
}

func (ci *ClientInteractive) Write(data string) error {
	ci.resetTimer()
	if err := ci.s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("set write deadline failed: %w", err)
	}
	if ci.client.Debug {
		slog.Debug("AT SEND", "raw", string(data))
	}
	if _, err := ci.s.conn.Write([]byte(data)); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	return nil
}

func (ci *ClientInteractive) WriteCmd(cmd string) error {
	ci.resetTimer()
	req := framePayload(0xa4, []byte(cmd))

	if err := ci.s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("set write deadline failed: %w", err)
	}
	if ci.client.Debug {
		slog.Debug("AT SEND", "raw", string(req))
	}
	if _, err := ci.s.conn.Write(req); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	return nil
}

func (ci *ClientInteractive) ReadFrame(timeout time.Duration) (string, error) {
	ci.resetTimer()
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
	ci.closeOnce.Do(func() {
		if ci.idleTimer != nil {
			ci.idleTimer.Stop()
		}
		ci.client.cmdMu.Unlock()
	})
	return nil
}

// StartInteractive begins an interactive session on the client connection.
func (c *Client) StartInteractive() (*ClientInteractive, error) {
	c.cmdMu.Lock()

	c.mu.Lock()
	s := c.session
	if s == nil {
		c.mu.Unlock()
		c.cmdMu.Unlock()
		return nil, fmt.Errorf("modem connection offline")
	}
	c.mu.Unlock()

	// Drain any stale messages from response buffer
	for {
		select {
		case <-s.responseChan:
		default:
			goto interactiveDrained
		}
	}
interactiveDrained:

	ci := &ClientInteractive{
		client:      c,
		s:           s,
		idleTimeout: 30 * time.Second,
	}
	ci.idleTimer = time.AfterFunc(ci.idleTimeout, func() {
		slog.Warn("Interactive session idle timeout reached, closing session")
		ci.Close()
	})

	return ci, nil
}
