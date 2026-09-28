// Command tls-server is an example of a cantcp TLS 1.3 server with mutual
// TLS: it accepts clients, prints the frames they send and answers with a
// frame.
//
// Generate the certificates first, for example with the OpenSSL commands
// from the cantcp documentation (docs/TLS-KEYS.md):
//
//	openssl ecparam -name prime256v1 -genkey -noout -out ca-key.pem
//	openssl req -new -x509 -days 3650 -key ca-key.pem -out ca.pem \
//	  -subj "/CN=cantcp local CA" \
//	  -addext "basicConstraints=critical,CA:TRUE" \
//	  -addext "keyUsage=critical,keyCertSign,cRLSign"
//
//	openssl ecparam -name prime256v1 -genkey -noout -out server-key.pem
//	openssl req -new -key server-key.pem -out server.csr -subj "/CN=localhost"
//	printf 'subjectAltName = DNS:localhost, IP:127.0.0.1\nextendedKeyUsage = serverAuth\n' > server-ext.cnf
//	openssl x509 -req -in server.csr -CA ca.pem -CAkey ca-key.pem \
//	  -CAcreateserial -days 825 -out server.pem -extfile server-ext.cnf
//
//	openssl ecparam -name prime256v1 -genkey -noout -out client-key.pem
//	openssl req -new -key client-key.pem -out client.csr -subj "/CN=cantcp-client"
//	printf 'extendedKeyUsage = clientAuth\n' > client-ext.cnf
//	openssl x509 -req -in client.csr -CA ca.pem -CAkey ca-key.pem \
//	  -CAcreateserial -days 825 -out client.pem -extfile client-ext.cnf
//
// Run:
//
//	go run ./examples/tls/server -addr :29536 \
//	  -cert server.pem -key server-key.pem -ca ca.pem
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	cantcp "github.com/burn-lab-dev/cantcp-lib-go"
)

func main() {
	addr := flag.String("addr", ":29536", "listen address")
	certFile := flag.String("cert", "server.pem", "TLS server certificate")
	keyFile := flag.String("key", "server-key.pem", "TLS server key")
	caFile := flag.String("ca", "ca.pem", "CA used to verify client certificates")
	flag.Parse()

	pool := loadCA(*caFile)
	pair, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		log.Fatalf("load the server key pair: %v", err)
	}
	conf := &tls.Config{
		Certificates: []tls.Certificate{pair},
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
		// Mutual TLS: only clients with a certificate signed by the CA may
		// connect. Use tls.VerifyClientCertIfGiven to make it optional.
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  pool,
	}

	listener, err := tls.Listen("tcp", *addr, conf)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	log.Printf("listening on %s (TLS 1.3, client certificates required)", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatalf("accept: %v", err)
		}
		go serve(conn)
	}
}

// serve handles one client: it prints the frames received and answers with a
// frame carrying the same identifier.
func serve(conn net.Conn) {
	defer conn.Close()
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		log.Printf("client %s: no certificate", conn.RemoteAddr())
		return
	}
	log.Printf("client %s: TLS %s, subject %s",
		conn.RemoteAddr(), tls.VersionName(state.Version), state.PeerCertificates[0].Subject)

	enc := cantcp.NewEncoder(conn)
	dec := cantcp.NewDecoder(conn)
	for {
		frame, err := dec.DecodeFrame()
		if err != nil {
			log.Printf("client %s: %v", conn.RemoteAddr(), err)
			return
		}
		fmt.Printf("frame id=%03X fd=%v data=% X\n",
			frame.ID, frame.Type == cantcp.TypeFd, frame.Data)

		reply := frame
		reply.Data = []byte{0x01, 0x02}
		if err := enc.EncodeFrame(&reply); err != nil {
			log.Printf("client %s: reply: %v", conn.RemoteAddr(), err)
			return
		}
	}
}

// loadCA reads a PEM CA pool.
func loadCA(path string) *x509.CertPool {
	pem, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		log.Fatalf("%s: no certificates found", path)
	}
	return pool
}
