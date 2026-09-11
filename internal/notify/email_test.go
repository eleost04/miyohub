package notify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"math/big"
	"mime"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func smtpCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"smtp.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

func TestSMTPEncryptedDeliveryAndPlaintextRefusal(t *testing.T) {
	for _, mode := range []string{"tls", "starttls", "plaintext", "untrusted", "auth-failure"} {
		t.Run(mode, func(t *testing.T) {
			certificate, roots := smtpCertificate(t)
			clientConn, serverConn := net.Pipe()
			done := make(chan struct{})
			var commands []string
			var received []byte
			go func() {
				defer close(done)
				defer serverConn.Close()
				_ = serverConn.SetDeadline(time.Now().Add(3 * time.Second))
				var conn net.Conn = serverConn
				secure := mode != "starttls" && mode != "plaintext"
				if secure {
					tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
					if err := tlsConn.Handshake(); err != nil {
						return
					}
					conn = tlsConn
				}
				protocol := textproto.NewConn(conn)
				_ = protocol.PrintfLine("220 smtp.test ESMTP")
				for {
					line, err := protocol.ReadLine()
					if err != nil {
						return
					}
					verb, _, _ := strings.Cut(line, " ")
					commands = append(commands, verb)
					switch verb {
					case "EHLO":
						_ = protocol.PrintfLine("250-smtp.test")
						if !secure && mode == "starttls" {
							_ = protocol.PrintfLine("250-STARTTLS")
						}
						_ = protocol.PrintfLine("250 AUTH PLAIN")
					case "STARTTLS":
						_ = protocol.PrintfLine("220 Ready")
						tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
						if err := tlsConn.Handshake(); err != nil {
							return
						}
						conn, secure = tlsConn, true
						protocol = textproto.NewConn(conn)
					case "AUTH":
						if !secure {
							t.Error("SMTP credentials sent without TLS")
						}
						encoded := strings.TrimPrefix(line, "AUTH PLAIN ")
						raw, _ := base64.StdEncoding.DecodeString(encoded)
						if string(raw) != "\x00from@example.test\x00smtp-secret-test" {
							t.Error("invalid SMTP authentication")
						}
						if mode == "auth-failure" {
							_ = protocol.PrintfLine("535 private-secret rejected")
						} else {
							_ = protocol.PrintfLine("235 Authenticated")
						}
					case "MAIL", "RCPT":
						_ = protocol.PrintfLine("250 OK")
					case "DATA":
						_ = protocol.PrintfLine("354 End with dot")
						received, _ = protocol.ReadDotBytes()
						_ = protocol.PrintfLine("250 Accepted")
					case "QUIT":
						_ = protocol.PrintfLine("221 Bye")
						return
					default:
						_ = protocol.PrintfLine("500 Unknown command")
					}
				}
			}()
			channel := testChannel("email")
			channel.SMTPSSL = mode != "starttls" && mode != "plaintext"
			sender := Sender{Timeout: time.Second, dialContext: func(context.Context, string, string) (net.Conn, error) { return clientConn, nil }, tlsConfig: &tls.Config{RootCAs: roots}}
			if mode == "untrusted" {
				sender.tlsConfig = nil
			}
			result := sendTest(sender, channel)
			<-done
			if mode == "tls" || mode == "starttls" {
				if !result.OK {
					t.Fatal("encrypted delivery failed", result)
				}
				message, err := mail.ReadMessage(strings.NewReader(string(received)))
				if err != nil {
					t.Fatal(err)
				}
				subject, err := (&mime.WordDecoder{}).DecodeHeader(message.Header.Get("Subject"))
				if err != nil || subject != "测试 <标题>" {
					t.Fatal("invalid UTF-8 subject", subject, err)
				}
				body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, message.Body))
				if err != nil || string(body) != "一行\n<script>正文</script>" {
					t.Fatal("invalid UTF-8 body", err)
				}
				if message.Header.Get("Content-Type") != "text/plain; charset=UTF-8" || !strings.Contains(message.Header.Get("To"), "to@example.test") {
					t.Fatal("invalid MIME headers")
				}
			} else if result.OK || result.Error == "" || strings.Contains(result.Error, "private-secret") {
				t.Fatal("unsafe SMTP error", result)
			}
			if mode == "plaintext" && (strings.Contains(strings.Join(commands, " "), "AUTH") || len(received) > 0) {
				t.Fatal("plaintext server received credentials or a message")
			}
			if mode == "untrusted" && len(commands) != 0 {
				t.Fatal("untrusted certificate was accepted")
			}
		})
	}
}

func TestSMTPContextCancellation(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	sender := Sender{Timeout: 10 * time.Millisecond, dialContext: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
	channel := testChannel("email")
	channel.SMTPSSL = false
	if result := sendTest(sender, channel); result.OK || !strings.Contains(result.Error, "超时") {
		t.Fatal("SMTP cancellation was not bounded", result)
	}
}
