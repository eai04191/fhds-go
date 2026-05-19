package udp

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// Listener wraps a UDP socket and returns only the most-recent queued packet,
// so the trigger logic never reacts to stale telemetry.
type Listener struct {
	conn    *net.UDPConn
	buf     []byte
	timeout time.Duration
	lost    bool
}

// Open binds a UDP socket at host:port. Caller must Close.
func Open(host string, port int, timeout time.Duration) (*Listener, error) {
	addr := &net.UDPAddr{IP: net.ParseIP(host), Port: port}
	if addr.IP == nil {
		return nil, fmt.Errorf("invalid host: %q", host)
	}
	c, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	// Keep the OS buffer small so we don't queue old packets behind a slow tick.
	_ = c.SetReadBuffer(4096)
	return &Listener{conn: c, buf: make([]byte, 1500), timeout: timeout}, nil
}

func (l *Listener) Close() error {
	if l.conn == nil {
		return nil
	}
	err := l.conn.Close()
	l.conn = nil
	return err
}

func (l *Listener) Lost() bool          { return l.lost }
func (l *Listener) SetLost(lost bool)   { l.lost = lost }
func (l *Listener) LocalAddr() net.Addr { return l.conn.LocalAddr() }

// RecvLatest blocks up to the configured timeout for at least one packet, then
// drains everything else already queued and returns only the newest one.
// Returns (nil, nil, nil) on timeout.
func (l *Listener) RecvLatest() (pkt []byte, from *net.UDPAddr, err error) {
	if err := l.conn.SetReadDeadline(time.Now().Add(l.timeout)); err != nil {
		return nil, nil, err
	}
	n, addr, err := l.conn.ReadFromUDP(l.buf)
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	// Hold a copy of the latest payload + its source.
	out := append([]byte(nil), l.buf[:n]...)
	from = addr

	// Drain whatever else is queued with zero-timeout reads.
	if err := l.conn.SetReadDeadline(time.Now()); err != nil {
		return out, from, nil
	}
	for {
		n, addr, err = l.conn.ReadFromUDP(l.buf)
		if err != nil {
			break
		}
		out = append(out[:0], l.buf[:n]...)
		from = addr
	}
	return out, from, nil
}
