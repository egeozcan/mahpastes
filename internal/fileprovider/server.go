package fileprovider

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Discovery struct {
	Protocol          int    `json:"protocol"`
	Domain            string `json:"domain"`
	Endpoint          string `json:"endpoint"`
	CertificateSHA256 string `json:"certificateSHA256"`
}

type Server struct {
	store     *Store
	http      *http.Server
	cancel    context.CancelFunc
	done      chan struct{}
	once      sync.Once
	Discovery Discovery
}

// Start uses a separate loopback listener and credential from the public API.
// Discovery contains no secret. The extension authenticates the certificate
// against the App Group discovery record before sending the Keychain token.
func Start(ctx context.Context, store *Store, domain, secret string, signal func() error) (*Server, error) {
	if domain == "" || len(secret) < 32 {
		return nil, errors.New("invalid provider enrollment")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Mahpastes File Provider"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(1, 0, 0), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	pin := sha256.Sum256(der)
	workerCtx, cancel := context.WithCancel(ctx)
	s := &Server{store: store, cancel: cancel, done: make(chan struct{}), Discovery: Discovery{Protocol: 1, Domain: domain, Endpoint: "https://" + listener.Addr().String(), CertificateSHA256: hex.EncodeToString(pin[:])}}
	s.http = &http.Server{Handler: handler(store, domain, secret), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return workerCtx }}
	tlsListener := tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	go func() {
		if err := s.http.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("File Provider listener stopped: %v", err)
		}
	}()
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		lastHead := ""
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				_, err := store.Sync(workerCtx)
				if err != nil {
					if workerCtx.Err() == nil {
						log.Printf("File Provider projection: %v", err)
					}
					continue
				}
				head, err := store.Head(workerCtx)
				if err != nil {
					continue
				}
				if head != lastHead && signal != nil {
					if err := signal(); err != nil {
						log.Printf("File Provider notification: %v", err)
						continue
					}
				}
				lastHead = head
			}
		}
	}()
	return s, nil
}

// Store exposes the projection to the host solely for enumerator signalling.
// It does not provide file bytes or bypass the authenticated loopback API.
func (s *Server) Store() *Store { return s.store }

func (s *Server) Close() {
	s.once.Do(func() {
		s.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.http.Shutdown(ctx); err != nil {
			_ = s.http.Close()
		}
		<-s.done
	})
}

func handler(store *Store, domain, secret string) http.Handler {
	semaphore := make(chan struct{}, 4)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "read_only", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Query().Get("domain") != domain {
			http.Error(w, "domain_mismatch", http.StatusForbidden)
			return
		}
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
		case <-r.Context().Done():
			return
		}
		var result any
		var err error
		q := r.URL.Query()
		switch r.URL.Path {
		case "/v1/item":
			result, err = store.Item(r.Context(), q.Get("id"))
		case "/v1/enumerate":
			result, err = store.Enumerate(r.Context(), q.Get("scope"), q.Get("page"))
		case "/v1/changes":
			result, err = store.Changes(r.Context(), q.Get("scope"), q.Get("anchor"))
		case "/v1/anchor":
			var anchor string
			anchor, err = store.Anchor(r.Context(), q.Get("scope"))
			result = map[string]string{"anchor": anchor}
		case "/v1/content":
			started := false
			err = store.Content(r.Context(), q.Get("id"), q.Get("version"), func(i Item) (io.Writer, error) {
				metadata, e := json.Marshal(i)
				if e != nil {
					return nil, e
				}
				w.Header().Set("X-Mahpastes-Item", base64.StdEncoding.EncodeToString(metadata))
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Length", strconv.FormatInt(i.Size, 10))
				started = true
				w.WriteHeader(http.StatusOK)
				return w, nil
			})
			if started {
				if err != nil {
					panic(http.ErrAbortHandler)
				}
				return
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			status := http.StatusInternalServerError
			code := "server_error"
			switch {
			case errors.Is(err, ErrNoSuchItem):
				status = 404
				code = err.Error()
			case errors.Is(err, ErrVersion):
				status = 409
				code = err.Error()
			case errors.Is(err, ErrPage), errors.Is(err, ErrAnchor):
				status = 410
				code = err.Error()
			}
			if status == 500 && !errors.Is(err, context.Canceled) {
				log.Printf("File Provider request: %v", err)
			}
			http.Error(w, code, status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
}
