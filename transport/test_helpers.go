package transport

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

// SSEEvent represents a parsed Server-Sent Event
type SSEEvent struct {
	ID      string
	Event   string
	Data    string
	Retry   int
}

// ParseSSEEvent parses a raw SSE event string into structured data
func ParseSSEEvent(raw string) (*SSEEvent, error) {
	event := &SSEEvent{}
	lines := strings.Split(raw, "\n")
	
	for _, line := range lines {
		if strings.HasPrefix(line, "id:") {
			event.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		} else if strings.HasPrefix(line, "event:") {
			event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += data
		} else if strings.HasPrefix(line, "retry:") {
			retryStr := strings.TrimSpace(strings.TrimPrefix(line, "retry:"))
			fmt.Sscanf(retryStr, "%d", &event.Retry)
		}
	}
	
	if event.Data == "" && event.Event == "" {
		return nil, fmt.Errorf("invalid SSE event: no data or event type")
	}
	
	return event, nil
}

// TestCertificates holds test certificates and keys
type TestCertificates struct {
	CACert     *x509.Certificate
	CAKey      *rsa.PrivateKey
	CAPEMCert  []byte
	CAPEMKey   []byte
	
	ServerCert    *x509.Certificate
	ServerKey     *rsa.PrivateKey
	ServerPEMCert []byte
	ServerPEMKey  []byte
	
	ClientCert    *x509.Certificate
	ClientKey     *rsa.PrivateKey
	ClientPEMCert []byte
	ClientPEMKey  []byte
}

// GenerateTestCertificates creates a complete set of test certificates
func GenerateTestCertificates(hosts []string) (*TestCertificates, error) {
	certs := &TestCertificates{}
	
	// Generate CA certificate
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Test CA"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	
	// Generate CA key
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA key: %w", err)
	}
	
	// Create CA certificate
	caCertDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}
	
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}
	
	certs.CACert = caCert
	certs.CAKey = caKey
	certs.CAPEMCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	certs.CAPEMKey = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})
	
	// Generate server certificate
	serverCert := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization:  []string{"Test Server"},
			Country:       []string{"US"},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	
	// Add hosts and IPs
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			serverCert.IPAddresses = append(serverCert.IPAddresses, ip)
		} else {
			serverCert.DNSNames = append(serverCert.DNSNames, h)
		}
	}
	
	// Generate server key
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server key: %w", err)
	}
	
	// Create server certificate signed by CA
	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverCert, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create server certificate: %w", err)
	}
	
	serverCertParsed, err := x509.ParseCertificate(serverCertDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server certificate: %w", err)
	}
	
	certs.ServerCert = serverCertParsed
	certs.ServerKey = serverKey
	certs.ServerPEMCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER})
	certs.ServerPEMKey = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})
	
	// Generate client certificate
	clientCert := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			Organization:  []string{"Test Client"},
			Country:       []string{"US"},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	
	// Generate client key
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate client key: %w", err)
	}
	
	// Create client certificate signed by CA
	clientCertDER, err := x509.CreateCertificate(rand.Reader, clientCert, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create client certificate: %w", err)
	}
	
	clientCertParsed, err := x509.ParseCertificate(clientCertDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client certificate: %w", err)
	}
	
	certs.ClientCert = clientCertParsed
	certs.ClientKey = clientKey
	certs.ClientPEMCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER})
	certs.ClientPEMKey = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)})
	
	return certs, nil
}

// WriteCertificatesToFiles writes certificates to temporary files for testing
func (tc *TestCertificates) WriteCertificatesToFiles(dir string) (caCertFile, caKeyFile, serverCertFile, serverKeyFile, clientCertFile, clientKeyFile string, err error) {
	// Write CA cert
	caCertFile = dir + "/ca-cert.pem"
	if err = os.WriteFile(caCertFile, tc.CAPEMCert, 0644); err != nil {
		return "", "", "", "", "", "", fmt.Errorf("failed to write CA cert: %w", err)
	}
	
	// Write CA key
	caKeyFile = dir + "/ca-key.pem"
	if err = os.WriteFile(caKeyFile, tc.CAPEMKey, 0600); err != nil {
		return "", "", "", "", "", "", fmt.Errorf("failed to write CA key: %w", err)
	}
	
	// Write server cert
	serverCertFile = dir + "/server-cert.pem"
	if err = os.WriteFile(serverCertFile, tc.ServerPEMCert, 0644); err != nil {
		return "", "", "", "", "", "", fmt.Errorf("failed to write server cert: %w", err)
	}
	
	// Write server key
	serverKeyFile = dir + "/server-key.pem"
	if err = os.WriteFile(serverKeyFile, tc.ServerPEMKey, 0600); err != nil {
		return "", "", "", "", "", "", fmt.Errorf("failed to write server key: %w", err)
	}
	
	// Write client cert
	clientCertFile = dir + "/client-cert.pem"
	if err = os.WriteFile(clientCertFile, tc.ClientPEMCert, 0644); err != nil {
		return "", "", "", "", "", "", fmt.Errorf("failed to write client cert: %w", err)
	}
	
	// Write client key
	clientKeyFile = dir + "/client-key.pem"
	if err = os.WriteFile(clientKeyFile, tc.ClientPEMKey, 0600); err != nil {
		return "", "", "", "", "", "", fmt.Errorf("failed to write client key: %w", err)
	}
	
	return caCertFile, caKeyFile, serverCertFile, serverKeyFile, clientCertFile, clientKeyFile, nil
}

// GenerateExpiredCertificate creates an expired certificate for testing
func GenerateExpiredCertificate(ca *x509.Certificate, caKey *rsa.PrivateKey, hosts []string) (certPEM, keyPEM []byte, err error) {
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(100),
		Subject: pkix.Name{
			Organization:  []string{"Expired Test Server"},
			Country:       []string{"US"},
		},
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     time.Now().Add(-24 * time.Hour), // Already expired
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	
	// Add hosts and IPs
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			cert.IPAddresses = append(cert.IPAddresses, ip)
		} else {
			cert.DNSNames = append(cert.DNSNames, h)
		}
	}
	
	// Generate key
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}
	
	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}
	
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	
	return certPEM, keyPEM, nil
}

// GenerateWrongHostnameCertificate creates a certificate with wrong hostname for testing
func GenerateWrongHostnameCertificate(ca *x509.Certificate, caKey *rsa.PrivateKey) (certPEM, keyPEM []byte, err error) {
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(101),
		Subject: pkix.Name{
			Organization:  []string{"Wrong Hostname Test Server"},
			Country:       []string{"US"},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		DNSNames:     []string{"wrong.example.com"}, // Wrong hostname
	}
	
	// Generate key
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}
	
	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}
	
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	
	return certPEM, keyPEM, nil
}

// CreateTLSConfig creates a TLS config with the given certificates
func CreateTLSConfig(caCertPEM, certPEM, keyPEM []byte, clientAuth tls.ClientAuthType) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load key pair: %w", err)
	}
	
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   clientAuth,
	}
	
	if caCertPEM != nil {
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCertPEM) {
			return nil, fmt.Errorf("failed to add CA certificate to pool")
		}
		tlsConfig.RootCAs = caCertPool
		tlsConfig.ClientCAs = caCertPool
	}
	
	return tlsConfig, nil
}