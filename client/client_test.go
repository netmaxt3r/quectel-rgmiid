package client

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestClient_SendCommandLengthLimit(t *testing.T) {
	c := NewClient("127.0.0.1:9999")
	// Command longer than 2048 bytes
	longCmd := string(make([]byte, 2049))
	_, err := c.SendCommand(longCmd, 1*time.Second)
	if err == nil {
		t.Fatalf("Expected error for long command, got nil")
	}
	expectedSub := "exceeds maximum limit of 2048 bytes"
	if !strings.Contains(err.Error(), expectedSub) {
		t.Errorf("Expected error to contain %q, got %q", expectedSub, err.Error())
	}
}

func TestClient_SendCommand(t *testing.T) {
	// Start local mock server on an ephemeral port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Handle mock connection asynchronously
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		if n < 3 {
			return
		}
		// Validate magic byte
		if buf[0] != 0xa4 {
			return
		}
		payloadLen := (int(buf[1]) << 8) | int(buf[2])
		if n != 3+payloadLen {
			return
		}

		// Frame 1: Send initialization ready notification (URC header 0xe0)
		msg1 := "RGMII_ATC_READY\r\n"
		resp1 := make([]byte, 3+len(msg1))
		resp1[0] = 0xe0
		resp1[1] = byte((len(msg1) >> 8) & 0xff)
		resp1[2] = byte(len(msg1) & 0xff)
		copy(resp1[3:], []byte(msg1))

		// Frame 2: Send command OK result code (response header 0xa0)
		msg2 := "\r\nOK\r\n"
		resp2 := make([]byte, 3+len(msg2))
		resp2[0] = 0xa0
		resp2[1] = byte((len(msg2) >> 8) & 0xff)
		resp2[2] = byte(len(msg2) & 0xff)
		copy(resp2[3:], []byte(msg2))

		conn.Write(resp1)
		time.Sleep(10 * time.Millisecond)
		conn.Write(resp2)
	}()

	client := NewClient(addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start background connect loops
	client.Start(ctx)

	// Wait for connectChan signal
	select {
	case <-client.connectChan:
		// connected
	case <-time.After(2 * time.Second):
		t.Fatalf("Timeout waiting for connection to establish")
	}

	// Send command with 2s timeout
	resp, err := client.SendCommand("ATI", 2*time.Second)
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}

	// The URC message (RGMII_ATC_READY) should be routed to URC channel,
	// so the command response output should only contain the second frame (OK)
	expected := "\r\nOK\r\n"
	if resp != expected {
		t.Errorf("Expected response %q, got %q", expected, resp)
	}
}
