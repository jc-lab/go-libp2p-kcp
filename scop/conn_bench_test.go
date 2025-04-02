package scop

import (
	"github.com/jc-lab/go-libp2p-kcp/kcptune"
	"github.com/xtaci/kcp-go/v5"
	"io"
	"net"
	"sync"
	"testing"
)

func newBenchPipe(t *testing.B) (_c net.Conn, _s func() net.Conn) {
	listener, err := kcp.ListenWithOptions("127.0.0.1:0", nil, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	client, err := kcp.DialWithOptions(listener.Addr().String(), nil, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	kcptune.Default(client)
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return client, func() net.Conn {
		conn, err := listener.AcceptKCP()
		if err != nil {
			t.Fatal(err)
		}
		kcptune.Default(conn)
		return conn
	}
}

func BenchmarkThroughput(b *testing.B) {
	clientConn, serverConnGet := newBenchPipe(b)
	defer clientConn.Close()

	payloadSize := 64 * 1024 // 64KB payload
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	var wg sync.WaitGroup

	// Start server
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet())
		if err != nil {
			b.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		buf := make([]byte, payloadSize)
		for {
			n, err := server.Read(buf)
			if err != nil {
				if err != io.EOF {
					b.Errorf("Server read error: %v", err)
				}
				return
			}
			_, err = server.Write(buf[:n]) // Echo back
			if err != nil {
				b.Errorf("Server write error: %v", err)
				return
			}
		}
	}()

	// Connect client
	client, err := Client(clientConn)
	if err != nil {
		b.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	buf := make([]byte, payloadSize)

	b.ResetTimer()
	b.SetBytes(int64(payloadSize * 2)) // Count both directions

	// Run benchmark
	for i := 0; i < b.N; i++ {
		// Write payload
		_, err := client.Write(payload)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}

		// Read echo
		_, err = io.ReadFull(client, buf)
		if err != nil {
			b.Fatalf("Read failed: %v", err)
		}
	}

	b.StopTimer()
	client.Close()
	wg.Wait()
}

// Different payload sizes
func BenchmarkThroughput1KB(b *testing.B)   { benchmarkThroughputSize(b, 1024) }
func BenchmarkThroughput4KB(b *testing.B)   { benchmarkThroughputSize(b, 4096) }
func BenchmarkThroughput64KB(b *testing.B)  { benchmarkThroughputSize(b, 64*1024) }
func BenchmarkThroughput256KB(b *testing.B) { benchmarkThroughputSize(b, 256*1024) }

func benchmarkThroughputSize(b *testing.B, payloadSize int) {
	clientConn, serverConnGet := newBenchPipe(b)
	defer clientConn.Close()

	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	var wg sync.WaitGroup

	// Start server
	wg.Add(1)
	go func() {
		defer wg.Done()
		server, err := Server(serverConnGet())
		if err != nil {
			b.Errorf("Server creation failed: %v", err)
			return
		}
		defer server.Close()

		buf := make([]byte, payloadSize)
		for {
			n, err := server.Read(buf)
			if err != nil {
				if err != io.EOF {
					b.Errorf("Server read error: %v", err)
				}
				return
			}
			_, err = server.Write(buf[:n]) // Echo back
			if err != nil {
				b.Errorf("Server write error: %v", err)
				return
			}
		}
	}()

	// Connect client
	client, err := Client(clientConn)
	if err != nil {
		b.Fatalf("Client connection failed: %v", err)
	}
	defer client.Close()

	buf := make([]byte, payloadSize)

	b.ResetTimer()
	b.SetBytes(int64(payloadSize * 2)) // Count both directions

	// Run benchmark
	for i := 0; i < b.N; i++ {
		// Write payload
		_, err := client.Write(payload)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}

		// Read echo
		_, err = io.ReadFull(client, buf)
		if err != nil {
			b.Fatalf("Read failed: %v", err)
		}
	}

	b.StopTimer()
	client.Close()
	wg.Wait()
}
