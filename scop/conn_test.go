// Copyright 2025 JC-Lab.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scop

import (
	"github.com/stretchr/testify/assert"
	"github.com/xtaci/kcp-go/v5"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func newTestPipe(t *testing.T) (_c net.Conn, _s func() net.Conn) {
	listener, err := kcp.ListenWithOptions("127.0.0.1:0", nil, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	client, err := kcp.DialWithOptions(listener.Addr().String(), nil, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return client, func() net.Conn {
		conn, err := listener.AcceptKCP()
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
}

func TestBasicConnectivity(t *testing.T) {
	var wg sync.WaitGroup

	clientConn, serverConnGet := newTestPipe(t)
	defer clientConn.Close()

	// 서버 시작
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet())
		if err != nil {
			t.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		_ = server.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 1024)
		if n, err := server.Read(buf); err != nil {
			t.Error("Read failed", err)
		} else {
			_, _ = server.Write(buf[:n])
		}
	}()

	// 클라이언트 연결
	client, err := Client(clientConn)
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	// 기본 데이터 전송 테스트
	testData := []byte("Hello, World!")
	if _, err := client.Write(testData); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	buf := make([]byte, 1024)
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	} else {
		assert.Equal(t, testData, buf[:n])
	}

	wg.Wait()
}

func TestConnectionRetry(t *testing.T) {
	clientConn, _ := newTestPipe(t)
	defer clientConn.Close()

	// 서버 없이 연결 시도
	_, err := Client(clientConn, WithMaxRetries(2), WithInitialRTO(100*time.Millisecond))
	if err == nil {
		t.Fatal("Expected connection failure, but got success")
	}
}

func TestCloseFromServer(t *testing.T) {
	var wg sync.WaitGroup
	clientConn, serverConnGet := newTestPipe(t)
	defer clientConn.Close()

	// start server
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet())
		if err != nil {
			t.Errorf("Server creation failed: %v", err)
			return
		}

		// Send one message to ensure connection is established
		_, err = server.Write([]byte("test"))
		assert.NoError(t, err)

		// Close server connection
		err = server.Close()
		assert.NoError(t, err)
		assert.Equal(t, StateClosed, server.getState())
	}()

	// Connect client
	client, err := Client(clientConn)
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	// Read the test message
	buf := make([]byte, 1024)
	_, err = client.Read(buf)
	assert.NoError(t, err)

	// Try to readData after server closes connection
	// Should receive EOF or connection reset
	_, err = client.Read(buf)
	assert.Error(t, err)
	assert.True(t, err == io.EOF || err.Error() == "connection reset by peer")

	// Verify client state is changed to closed
	assert.Equal(t, StateClosed, client.getState())

	// Try to write after connection is closed
	_, err = client.Write([]byte("test"))
	assert.Error(t, err)

	wg.Wait()
}

func TestCloseFromClient(t *testing.T) {
	var wg sync.WaitGroup
	clientConn, serverConnGet := newTestPipe(t)
	defer clientConn.Close()

	serverClosed := make(chan struct{})

	// start server
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet())
		if err != nil {
			t.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		// Read until client closes connection
		buf := make([]byte, 1024)
		for {
			_, err = server.Read(buf)
			if err != nil {
				// Should get error when client closes
				assert.True(t, err == io.EOF || err.Error() == "connection reset by peer")
				assert.Equal(t, StateClosed, server.getState())
				close(serverClosed)
				return
			}
		}
	}()

	// Connect client
	client, err := Client(clientConn)
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}

	// Send test message to ensure connection is established
	_, err = client.Write([]byte("test"))
	assert.NoError(t, err)

	// Close client connection
	err = client.Close()
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, client.getState())

	// Try to write after closing
	_, err = client.Write([]byte("test"))
	assert.Error(t, err)

	// Wait for server to detect closure
	select {
	case <-serverClosed:
		// Server detected client closure correctly
	case <-time.After(2 * time.Second):
		t.Fatal("Server failed to detect client closure")
	}

	wg.Wait()
}

func TestDeadlines(t *testing.T) {
	var wg sync.WaitGroup
	clientConn, serverConnGet := newTestPipe(t)
	defer clientConn.Close()

	// start server
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet())
		if err != nil {
			t.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		time.Sleep(2 * time.Second) // Delay to trigger client timeout
	}()

	client, err := Client(clientConn)
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	// Test readData deadline
	err = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	assert.NoError(t, err)

	buf := make([]byte, 1024)
	_, err = client.Read(buf)
	assert.Error(t, err) // Should timeout

	wg.Wait()
}

func TestInvalidPackets(t *testing.T) {
	var wg sync.WaitGroup
	clientConn, serverConnGet := newTestPipe(t)
	defer clientConn.Close()

	// start server
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet())
		if err != nil {
			t.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		// Send invalid version packet
		header := &Header{
			Version: 255, // Invalid version
			Flags:   FlagPSH,
		}
		err = server.writeSegment(header, []byte("test"))
		assert.NoError(t, err)
	}()

	client, err := Client(clientConn)
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	// Try to readData invalid packet
	buf := make([]byte, 1024)
	_, err = client.Read(buf)
	assert.Equal(t, err, ErrInvalidPacket) // Should get ErrInvalidPacket

	wg.Wait()
}

func TestKeepalive(t *testing.T) {
	var wg sync.WaitGroup
	clientConn, serverConnGet := newTestPipe(t)
	defer clientConn.Close()

	keepaliveInterval := 100 * time.Millisecond
	keepaliveTimeout := 300 * time.Millisecond

	// start server
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet(),
			WithKeepaliveInterval(keepaliveInterval),
			WithKeepaliveTimeout(keepaliveTimeout))
		if err != nil {
			t.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		// Wait until connection is closed by keepalive timeout
		buf := make([]byte, 1024)
		for {
			_, err = server.Read(buf)
			if err != nil {
				// Should get error when connection is closed
				assert.True(t, err == io.EOF || err.Error() == "connection reset by peer")
				assert.Equal(t, StateClosed, server.getState())
				return
			}
		}
	}()

	// Connect client with short keepalive interval
	client, err := Client(clientConn,
		WithKeepaliveInterval(keepaliveInterval),
		WithKeepaliveTimeout(keepaliveTimeout))
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	// Connection should be alive initially
	assert.Equal(t, StateEstablished, client.GetState())

	// Sleep longer than keepalive timeout
	time.Sleep(keepaliveTimeout * 2)

	// Connection should be closed due to keepalive timeout
	assert.Equal(t, StateClosed, client.GetState())

	wg.Wait()
}

func TestKeepaliveDisabled(t *testing.T) {
	var wg sync.WaitGroup
	clientConn, serverConnGet := newTestPipe(t)
	defer clientConn.Close()

	// start server with keepalive disabled
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet(),
			WithKeepaliveInterval(0)) // Disable keepalive
		if err != nil {
			t.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		// Server should remain active
		time.Sleep(200 * time.Millisecond)
		assert.Equal(t, StateEstablished, server.GetState())
	}()

	// Connect client with keepalive disabled
	client, err := Client(clientConn,
		WithKeepaliveInterval(0)) // Disable keepalive
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	// Connection should remain established
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, StateEstablished, client.GetState())

	wg.Wait()
}

func TestKeepaliveWithTraffic(t *testing.T) {
	var wg sync.WaitGroup
	clientConn, serverConnGet := newTestPipe(t)
	defer clientConn.Close()

	keepaliveInterval := 100 * time.Millisecond
	keepaliveTimeout := 300 * time.Millisecond

	// start server
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet(),
			WithKeepaliveInterval(keepaliveInterval),
			WithKeepaliveTimeout(keepaliveTimeout))
		if err != nil {
			t.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		buf := make([]byte, 1024)
		for i := 0; i < 3; i++ {
			n, err := server.Read(buf)
			if err != nil {
				t.Errorf("Server readData failed: %v", err)
				return
			}
			_, err = server.Write(buf[:n])
			if err != nil {
				t.Errorf("Server write failed: %v", err)
				return
			}
			time.Sleep(keepaliveInterval * 2) // Wait longer than keepalive interval
		}
	}()

	// Connect client
	client, err := Client(clientConn,
		WithKeepaliveInterval(keepaliveInterval),
		WithKeepaliveTimeout(keepaliveTimeout))
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	// Send data periodically
	testData := []byte("test")
	buf := make([]byte, 1024)
	for i := 0; i < 3; i++ {
		_, err = client.Write(testData)
		assert.NoError(t, err)

		n, err := client.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, testData, buf[:n])

		time.Sleep(keepaliveInterval * 2) // Wait longer than keepalive interval
	}

	// Connection should still be alive because of regular traffic
	assert.Equal(t, StateEstablished, client.GetState())

	wg.Wait()
}

func TestKeepaliveTimeout(t *testing.T) {
	var wg sync.WaitGroup
	clientConn, serverConnGet := newTestPipe(t)
	defer clientConn.Close()

	keepaliveInterval := 100 * time.Millisecond
	keepaliveTimeout := 300 * time.Millisecond

	serverClosed := make(chan struct{})

	// start server that stops responding
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet(),
			WithKeepaliveInterval(keepaliveInterval),
			WithKeepaliveTimeout(keepaliveTimeout))
		if err != nil {
			t.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		// Wait for the first message
		buf := make([]byte, 1024)
		_, err = server.Read(buf)
		assert.NoError(t, err)

		// Stop responding to simulate dead connection
		time.Sleep(keepaliveTimeout * 2)

		// Server should detect client closure
		assert.Equal(t, StateClosed, server.GetState())
		close(serverClosed)
	}()

	// Connect client
	client, err := Client(clientConn,
		WithKeepaliveInterval(keepaliveInterval),
		WithKeepaliveTimeout(keepaliveTimeout))
	if err != nil {
		t.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	// Send initial message
	_, err = client.Write([]byte("test"))
	assert.NoError(t, err)

	// Wait for server to close
	select {
	case <-serverClosed:
		// Server closed as expected
	case <-time.After(keepaliveTimeout * 3):
		t.Fatal("Server failed to close on keepalive timeout")
	}

	// Client should also detect connection closure
	assert.Equal(t, StateClosed, client.GetState())

	wg.Wait()
}
