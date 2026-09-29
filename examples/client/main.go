// Command client is a cantcp client example: it listens to frames, sends one
// and can read the server statistics over HTTP.
//
// The client talks to a cantcp server (the cantcpd daemon, see
// https://github.com/burn-lab-dev/cantcp): it opens a plain TCP or TLS 1.3
// connection and drives the cantcp codec.
//
// Run against a local plain daemon:
//
//	go run ./examples/client listen --count 5
//	go run ./examples/client send --id 123 --data 11223344
//
// With TLS and a client certificate (see the cantcp documentation,
// docs/TLS-KEYS.md):
//
//	go run ./examples/client listen --tls --tls-ca ca.pem \
//	  --tls-cert client.pem --tls-key client-key.pem --count 5
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	cantcp "github.com/burn-lab-dev/cantcp-lib-go"
)

// dialTimeout bounds the connection setup.
const dialTimeout = 10 * time.Second

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "client:", err)
		os.Exit(1)
	}
}

// run dispatches the listen and send commands.
func run(args []string) error {
	if len(args) == 0 {
		return errors.New(`no command; use "listen" or "send"`)
	}
	flags := flag.NewFlagSet("client", flag.ContinueOnError)
	server := flags.String("server", "127.0.0.1:29536", "cantcp server address")
	id := flags.String("id", "", "identifier in hexadecimal (send) or the filter id (listen)")
	mask := flags.String("mask", "", "filter mask in hexadecimal (listen; a zero mask accepts everything)")
	data := flags.String("data", "", "payload bytes in hexadecimal (send)")
	asJSON := flags.Bool("json", false, "print one JSON object per frame (listen)")
	count := flags.Int("count", 0, "stop after this many frames (listen, 0 = unlimited)")
	useTLS := flags.Bool("tls", false, "connect over TLS 1.3")
	caFile := flags.String("tls-ca", "", "CA file for the server certificate")
	certFile := flags.String("tls-cert", "", "client certificate (mutual TLS)")
	keyFile := flags.String("tls-key", "", "client key")
	serverName := flags.String("tls-server-name", "", "name verified in the certificate")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	conn, err := dial(ctx, *server, *useTLS, *caFile, *certFile, *keyFile, *serverName)
	if err != nil {
		return err
	}
	defer conn.Close()

	switch args[0] {
	case "listen":
		filterID, err := parseOptionalHex(*id, "id")
		if err != nil {
			return err
		}
		filterMask, err := parseOptionalHex(*mask, "mask")
		if err != nil {
			return err
		}
		return listen(conn, *asJSON, *count, filterID, filterMask)
	case "send":
		return send(conn, *id, *data)
	default:
		return fmt.Errorf("unknown command %q; use listen or send", args[0])
	}
}

// dial opens the connection: plain TCP or TLS 1.3 with the given material.
func dial(ctx context.Context, addr string, useTLS bool, caFile, certFile, keyFile, serverName string) (io.ReadWriteCloser, error) {
	dialer := &net.Dialer{Timeout: dialTimeout}
	if !useTLS {
		return dialer.DialContext(ctx, "tcp", addr)
	}
	conf := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: serverName}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s: no certificates found", caFile)
		}
		conf.RootCAs = pool
	}
	if certFile != "" {
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load client key pair: %w", err)
		}
		conf.Certificates = []tls.Certificate{pair}
	}
	return tls.DialWithDialer(dialer, "tcp", addr, conf)
}

// parseOptionalHex parses an optional hexadecimal value; an empty string is
// zero.
func parseOptionalHex(value, name string) (uint32, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid hexadecimal value %q", name, value)
	}
	return uint32(parsed), nil
}

// listen prints the frames received from the server that pass the filter.
func listen(conn io.ReadWriteCloser, asJSON bool, count int, filterID, filterMask uint32) error {
	decoder := cantcp.NewDecoder(conn)
	printed := 0
	for {
		frame, err := decoder.DecodeFrame()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("receive: %w", err)
		}
		if filterMask != 0 && frame.ID&filterMask != filterID&filterMask {
			continue
		}
		if asJSON {
			obj := struct {
				ID       uint32 `json:"id"`
				FD       bool   `json:"fd"`
				Extended bool   `json:"extended,omitempty"`
				Remote   bool   `json:"remote,omitempty"`
				Data     string `json:"data"`
			}{
				ID:       frame.ID,
				FD:       frame.Type == cantcp.TypeFd,
				Extended: frame.EFF,
				Remote:   frame.RTR,
				Data:     hex.EncodeToString(frame.Data),
			}
			if err := json.NewEncoder(os.Stdout).Encode(obj); err != nil {
				return err
			}
		} else {
			fmt.Printf("id=%03X fd=%v data=% X\n", frame.ID, frame.Type == cantcp.TypeFd, frame.Data)
		}
		printed++
		if count > 0 && printed >= count {
			return nil
		}
	}
}

// send writes one classic frame built from the flags.
func send(conn io.ReadWriteCloser, idHex, dataHex string) error {
	id, err := strconv.ParseUint(idHex, 16, 32)
	if err != nil {
		return fmt.Errorf("invalid identifier %q: want hexadecimal, for example 123", idHex)
	}
	payload, err := hex.DecodeString(dataHex)
	if err != nil {
		return fmt.Errorf("invalid payload %q: want hexadecimal bytes", dataHex)
	}
	frame := cantcp.Frame{ID: uint32(id), Type: cantcp.TypeClassic, EFF: id > 0x7FF, Data: payload}
	if err := cantcp.NewEncoder(conn).EncodeFrame(&frame); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	log.Printf("sent: %s", frame)
	return nil
}
