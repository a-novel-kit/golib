//go:build unix

package postgrespresets

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"errors"
	"syscall"
	"time"

	"github.com/uptrace/bun/driver/pgdriver"
)

// checkIdleConn returns [driver.ErrBadConn] for a pooled connection the server has written to or
// closed while it sat idle. database/sql then discards the connection and runs the query on another
// one, opening a fresh connection when it finds no live one.
//
// PostgreSQL ends every session when it shuts down, as in a restart or a failover, and an idle
// connection keeps that farewell unread until its next query. The check reads the socket once
// without waiting, so it costs no round trip.
func checkIdleConn(_ context.Context, conn *pgdriver.Conn) error {
	netConn := conn.Conn()
	if tlsConn, ok := netConn.(*tls.Conn); ok {
		netConn = tlsConn.NetConn()
	}

	sysConn, ok := netConn.(syscall.Conn)
	if !ok {
		return nil
	}

	rawConn, err := sysConn.SyscallConn()
	if err != nil {
		return driver.ErrBadConn
	}

	// The driver leaves the deadline of its last read behind. Once that deadline passes, every read
	// fails before it reaches the socket.
	err = netConn.SetReadDeadline(time.Time{})
	if err != nil {
		return driver.ErrBadConn
	}

	var idle bool

	err = rawConn.Read(func(fd uintptr) bool {
		var buf [1]byte

		_, readErr := syscall.Read(int(fd), buf[:])
		idle = errors.Is(readErr, syscall.EAGAIN)

		return true
	})
	if err != nil || !idle {
		return driver.ErrBadConn
	}

	return nil
}
