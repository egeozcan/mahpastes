package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"

	"go-clipboard/internal/fileprovider"
	"go-clipboard/internal/fpnative"
)

type FileProviderStatus struct {
	Supported    bool   `json:"supported"`
	Enabled      bool   `json:"enabled"`
	Running      bool   `json:"running"`
	Message      string `json:"message"`
	RecoveryPath string `json:"recoveryPath"`
}

type providerEnrollment struct {
	Domain  string `json:"domain"`
	Enabled bool   `json:"enabled"`
}

const providerDomainRemoved = "The Finder location was removed. Use Retry to add it again."

// FileProviderService is bound on every platform. Only the explicitly enabled
// macOS bundle has a native implementation; constructors and bindings are inert.
type FileProviderService struct {
	mu                 sync.Mutex
	native             func(operation, domain string) (fpnative.Result, error)
	ctx                context.Context
	db                 *sql.DB
	dataDir, groupPath string
	enrollment         providerEnrollment
	server             *fileprovider.Server
	message, recovery  string
}

func (s *FileProviderService) callNative(operation, domain string) (fpnative.Result, error) {
	if s.native != nil {
		return s.native(operation, domain)
	}
	return fpnative.Call(operation, domain)
}

func (s *FileProviderService) start(ctx context.Context, db *sql.DB, dataDir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
	s.db = db
	s.dataDir = dataDir
	info, err := s.callNative("info", "")
	if err != nil {
		return
	}
	s.groupPath = info.GroupPath
	b, err := os.ReadFile(filepath.Join(dataDir, "file-provider.json"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		s.message = err.Error()
		return
	}
	if err = json.Unmarshal(b, &s.enrollment); err != nil {
		s.message = err.Error()
		return
	}
	if s.enrollment.Enabled {
		if err = s.connect(false); err != nil {
			s.message = err.Error()
			log.Printf("File Provider: %v", err)
		}
	}
}

func (s *FileProviderService) Status() FileProviderStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Users can remove a domain in Finder while the app keeps running. Report
	// that state on reopening Settings so Retry is available without a restart.
	if s.server != nil && s.enrollment.Enabled {
		result, err := s.callNative("list", s.enrollment.Domain)
		if err != nil {
			s.message = err.Error()
		} else {
			found := false
			for _, id := range result.Domains {
				if id == s.enrollment.Domain {
					found = true
					break
				}
			}
			if !found {
				s.message = providerDomainRemoved
			} else if s.message == providerDomainRemoved {
				s.message = ""
			}
		}
	}
	return s.status()
}

func (s *FileProviderService) status() FileProviderStatus {
	return FileProviderStatus{Supported: s.groupPath != "", Enabled: s.enrollment.Enabled, Running: s.server != nil && s.message == "", Message: s.message, RecoveryPath: s.recovery}
}

func (s *FileProviderService) Enable() (FileProviderStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groupPath == "" || s.db == nil {
		return s.status(), errors.New("Finder integration is unavailable in this build")
	}
	if s.enrollment.Domain == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return s.status(), err
		}
		s.enrollment.Domain = hex.EncodeToString(id[:])
	}
	// Persist identity before registration. A crash/retry uses the same domain.
	s.enrollment.Enabled = true
	if err := s.saveEnrollment(); err != nil {
		s.enrollment.Enabled = false
		return s.status(), err
	}
	if err := s.connect(true); err != nil {
		s.message = err.Error()
		return s.status(), err
	}
	s.message = ""
	return s.status(), nil
}

func (s *FileProviderService) Disable() (FileProviderStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groupPath == "" {
		return s.status(), errors.New("Finder integration is unavailable in this build")
	}
	if s.enrollment.Domain != "" {
		registered, err := s.callNative("list", s.enrollment.Domain)
		if err != nil {
			return s.status(), err
		}
		for _, id := range registered.Domains {
			if id == s.enrollment.Domain {
				result, err := s.callNative("remove", s.enrollment.Domain)
				if err != nil {
					return s.status(), err
				}
				s.recovery = result.RecoveryPath
				break
			}
		}
	}
	wasEnabled := s.enrollment.Enabled
	s.enrollment.Enabled = false
	if err := s.saveEnrollment(); err != nil {
		// Native removal may already have succeeded, but the persisted intent
		// is still enabled. Keep that state and transport available for retry.
		s.enrollment.Enabled = wasEnabled
		s.message = err.Error()
		return s.status(), err
	}
	s.disconnect()
	s.message = ""
	return s.status(), nil
}

func (s *FileProviderService) Reveal() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enrollment.Enabled {
		return errors.New("Finder integration is disabled")
	}
	_, err := s.callNative("reveal", s.enrollment.Domain)
	return err
}

func (s *FileProviderService) connect(register bool) error {
	if len(s.enrollment.Domain) != 32 {
		return errors.New("invalid File Provider enrollment")
	}
	if _, err := hex.DecodeString(s.enrollment.Domain); err != nil {
		return err
	}
	if s.server == nil {
		credential, err := s.callNative("credential", s.enrollment.Domain)
		if err != nil {
			return err
		}
		store, err := fileprovider.Open(s.ctx, s.db)
		if err != nil {
			return err
		}
		domain := s.enrollment.Domain
		server, err := fileprovider.Start(s.ctx, store, domain, credential.Secret, func() { _, _ = s.callNative("signal", domain) })
		if err != nil {
			return err
		}
		b, err := json.Marshal(server.Discovery)
		if err == nil {
			err = writeProviderFile(s.discoveryPath(), b)
		}
		if err != nil {
			server.Close()
			return err
		}
		s.server = server
	}
	result, err := s.callNative("list", s.enrollment.Domain)
	if err != nil {
		return err
	}
	found := false
	for _, id := range result.Domains {
		if id == s.enrollment.Domain {
			found = true
		}
	}
	if !found {
		if !register {
			return errors.New(providerDomainRemoved)
		}
		if _, err = s.callNative("add", s.enrollment.Domain); err != nil {
			return err
		}
	}
	_, err = s.callNative("signal", s.enrollment.Domain)
	return err
}

func (s *FileProviderService) discoveryPath() string {
	return filepath.Join(s.groupPath, "provider-"+s.enrollment.Domain+".json")
}
func (s *FileProviderService) saveEnrollment() error {
	b, err := json.Marshal(s.enrollment)
	if err != nil {
		return err
	}
	return writeProviderFile(filepath.Join(s.dataDir, "file-provider.json"), b)
}

func writeProviderFile(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "provider-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *FileProviderService) disconnect() {
	if s.server != nil {
		s.server.Close()
		s.server = nil
	}
	if s.groupPath != "" && s.enrollment.Domain != "" {
		_ = os.Remove(s.discoveryPath())
	}
}
func (s *FileProviderService) stop() { s.mu.Lock(); defer s.mu.Unlock(); s.disconnect() }
