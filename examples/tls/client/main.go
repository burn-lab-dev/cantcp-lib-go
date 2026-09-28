// Command tls-client is an example of a cantcp TLS 1.3 client with mutual
// TLS: it connects to a server, verifies its certificate, sends one frame
// and prints the reply.
//
// Generate the certificates first: see the header of
// examples/tls/server/main.go or the cantcp documentation
// (docs/TLS-KEYS.md).
//
// Run (the server from examples/tls/server must be listening):
//
//	go run ./examples/tls/client -addr localhost:29536 \
//	  -ca ca.pem -cert client.pem -key client-key.pem
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"log"
	"os"
	"time"

	cantcp "github.com/burn-lab-dev/cantcp-lib-go"
)

func main() {
	addr := flag.String("addr", "localhost:29536", "server address")
	serverName := flag.String("server-name", "localhost", "name verified against the server certificate")
	certFile := flag.String("cert", "client.pem", "TLS client certificate")
	keyFile := flag.String("key", "client-key.pem", "TLS client key")
	caFile := flag.String("ca", "ca.pem", "CA used to verify the server certificate")
	flag.Parse()

	pool := x509.NewCertPool()
	pem, err := os.ReadFile(*caFile)
	if err != nil {
		log.Fatalf("read CA: %v", err)
	}
	if !pool.AppendCertsFromPEM(pem) {
		log.Fatalf("%s: no certificates found", *caFile)
	}
	pair, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		log.Fatalf("load the client key pair: %v", err)
	}
	conf := &tls.Config{
		Certificates: []tls.Certificate{pair},
		RootCAs:      pool,
		ServerName:   *serverName,
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
	}

	conn, err := tls.Dial("tcp", *addr, conf)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	state := conn.ConnectionState()
	log.Printf("connected: TLS %s, server subject %s",
		tls.VersionName(state.Version), state.PeerCertificates[0].Subject)

	frame := cantcp.Frame{ID: 0x123, Type: cantcp.TypeClassic, Data: []byte{0x11, 0x22, 0x33}}
	if err := cantcp.NewEncoder(conn).EncodeFrame(&frame); err != nil {
		log.Fatalf("send: %v", err)
	}
	log.Printf("sent: id=%03X data=% X", frame.ID, frame.Data)

	reply, err := cantcp.NewDecoder(conn).DecodeFrame()
	if err != nil {
		log.Fatalf("receive: %v", err)
	}
	log.Printf("reply: id=%03X data=% X", reply.ID, reply.Data)
}
