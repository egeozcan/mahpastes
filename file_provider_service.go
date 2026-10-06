package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"go-clipboard/internal/fileprovider"
	"go-clipboard/internal/fpfuse"
	"go-clipboard/internal/fpnative"
)

type FileProviderStatus struct {
	Supported    bool   `json:"supported"`
	Enabled      bool   `json:"enabled"`
	Running      bool   `json:"running"`
	Message      string `json:"message"`
	RecoveryPath string `json:"recoveryPath"`
	// Location names where the clips appear ("Finder" or "file manager") and
	// Path is the mount point when the location is a plain directory.
	Location string `json:"location"`
	Path     string `json:"path"`
}

type providerEnrollment struct {
	Domain  string `json:"domain"`
	Enabled bool   `json:"enabled"`
	// MountPath is the Linux mount point chosen at first enable. It stays put
	// across launches even if the defaults change.
	MountPath string `json:"mountPath,omitempty"`
	// CreatedMountDir records that the app created MountPath, so disabling
	// removes only a directory the user did not make.
	CreatedMountDir bool `json:"createdMountDir,omitempty"`
}

const providerDomainRemoved = "The Finder location was removed. Use Retry to add it again."
const providerFolderUnmounted = "The folder was unmounted outside Mahpastes. Use Retry to mount it again."

// FileProviderService is bound on every platform. The explicitly enabled macOS
// bundle registers a Finder File Provider domain; Linux builds serve the same
// projection as a FUSE mount from the app process. Elsewhere it is inert.
type FileProviderService struct {
	mu                 sync.Mutex
	native             func(operation, domain string) (fpnative.Result, error)
	ctx                context.Context
	db                 *sql.DB
	dataDir, groupPath string
	enrollment         providerEnrollment
	server             *fileprovider.Server
	message, recovery  string
	// Linux FUSE backend. mountMode selects it; mount is the live mount.
	mountMode bool
	mount     *fpfuse.Mount
	// onMount reports the mount directory ("" when unmounted) so watch
	// folders and imports can stay out of it.
	onMount func(dir string)
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
	if s.native == nil && fpfuse.Supported() {
		s.startMount()
		return
	}
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
	if s.mountMode {
		// Someone can unmount the folder behind the app's back (fusermount3 -u,
		// or a file manager's eject). Report it so Retry can mount it again.
		if s.mount != nil && !s.mount.Alive() {
			s.disconnect()
			s.message = providerFolderUnmounted
		}
		return s.status()
	}
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
	if s.mountMode {
		return FileProviderStatus{Supported: true, Enabled: s.enrollment.Enabled, Running: s.mount != nil, Message: s.message, Location: "file manager", Path: s.mountPath()}
	}
	return FileProviderStatus{Supported: s.groupPath != "", Enabled: s.enrollment.Enabled, Running: s.server != nil && s.message == "", Message: s.message, RecoveryPath: s.recovery, Location: "Finder"}
}

func (s *FileProviderService) Enable() (FileProviderStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mountMode {
		return s.enableMount()
	}
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
	if s.mountMode {
		return s.disableMount()
	}
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
	if s.mountMode {
		if s.mount == nil {
			return errors.New("the clips folder is not mounted")
		}
		// xdg-open hands the directory to the desktop's default file manager.
		cmd := exec.Command("xdg-open", s.mount.Dir())
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("open file manager: %w", err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
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
		server, err := fileprovider.Start(s.ctx, store, domain, credential.Secret, func() error { return s.signalProjection(domain, store) })
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
	return s.cleanupStaleDomainsAndSignal(result.Domains)
}

// A Mahpastes installation serves a single Finder location. A development
// build or a prior app bundle can leave a second domain behind because macOS
// keeps domains independently from extension registration. Retire those old
// domains once the current enrollment is available, preserving any downloads
// in a recovery location rather than deleting them.
func (s *FileProviderService) removeStaleDomains(domains []string) error {
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		if domain == "" || domain == s.enrollment.Domain {
			continue
		}
		if _, duplicate := seen[domain]; duplicate {
			continue
		}
		seen[domain] = struct{}{}
		result, err := s.callNative("remove", domain)
		if err != nil {
			return fmt.Errorf("remove stale Finder location: %w", err)
		}
		if result.RecoveryPath != "" {
			s.recovery = result.RecoveryPath
		}
	}
	return nil
}

// Stale-domain cleanup must never make the current location unavailable. The
// next startup or explicit Enable retries a transient removal failure.
func (s *FileProviderService) cleanupStaleDomainsAndSignal(domains []string) error {
	if err := s.removeStaleDomains(domains); err != nil {
		log.Printf("File Provider stale-domain cleanup: %v", err)
	}
	return s.signalProjection(s.enrollment.Domain, s.serverStore())
}

func (s *FileProviderService) serverStore() *fileprovider.Store {
	if s.server == nil {
		return nil
	}
	return s.server.Store()
}

func (s *FileProviderService) signalProjection(domain string, store *fileprovider.Store) error {
	operation := "signal"
	if store != nil {
		scopes, err := store.Scopes(s.ctx)
		if err != nil {
			// The server retains its prior notification head on an error, so the
			// next worker tick retries with every dynamic tag scope included.
			return fmt.Errorf("collect File Provider enumerator scopes: %w", err)
		}
		if len(scopes) > 0 {
			operation += ":" + strings.Join(scopes, ",")
		}
	}
	_, err := s.callNative(operation, domain)
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
	if s.mount != nil {
		s.mount.Close()
		s.mount = nil
		if s.onMount != nil {
			s.onMount("")
		}
	}
	if s.server != nil {
		s.server.Close()
		s.server = nil
	}
	if s.groupPath != "" && s.enrollment.Domain != "" {
		_ = os.Remove(s.discoveryPath())
	}
}
func (s *FileProviderService) stop() { s.mu.Lock(); defer s.mu.Unlock(); s.disconnect() }

// The Linux backend reuses the enrollment file and projection store, but needs
// no domain registration, credential or loopback server: the app process
// answers filesystem requests directly from the store.

func (s *FileProviderService) startMount() {
	s.mountMode = true
	b, err := os.ReadFile(filepath.Join(s.dataDir, "file-provider.json"))
	if errors.Is(err, os.ErrNotExist) {
		s.message = availabilityMessage()
		return
	}
	if err == nil {
		err = json.Unmarshal(b, &s.enrollment)
	}
	if err == nil && s.enrollment.Enabled {
		err = s.connectMount()
	} else if err == nil {
		s.message = availabilityMessage()
	}
	if err != nil {
		s.message = err.Error()
		log.Printf("File manager mount: %v", err)
	}
}

func availabilityMessage() string {
	if err := fpfuse.Available(); err != nil {
		return err.Error()
	}
	return ""
}

func (s *FileProviderService) enableMount() (FileProviderStatus, error) {
	if s.db == nil {
		return s.status(), errors.New("the clips folder is unavailable until the app has started")
	}
	if s.enrollment.MountPath == "" {
		s.enrollment.MountPath = defaultMountPath(s.dataDir)
	}
	s.enrollment.Enabled = true
	if err := s.saveEnrollment(); err != nil {
		s.enrollment.Enabled = false
		return s.status(), err
	}
	if err := s.connectMount(); err != nil {
		s.message = err.Error()
		return s.status(), err
	}
	s.message = ""
	return s.status(), nil
}

func (s *FileProviderService) disableMount() (FileProviderStatus, error) {
	wasEnabled := s.enrollment.Enabled
	s.enrollment.Enabled = false
	if err := s.saveEnrollment(); err != nil {
		s.enrollment.Enabled = wasEnabled
		s.message = err.Error()
		return s.status(), err
	}
	s.disconnect()
	// Leave no empty placeholder behind in the home directory; Remove refuses
	// a directory that is still a busy mount or has gained other contents.
	if path := s.enrollment.MountPath; path != "" && s.enrollment.CreatedMountDir {
		if err := os.Remove(path); err == nil || errors.Is(err, os.ErrNotExist) {
			// A directory the user makes there later is theirs.
			s.enrollment.CreatedMountDir = false
			if err := s.saveEnrollment(); err != nil {
				log.Printf("File manager mount: %v", err)
			}
		}
	}
	s.message = availabilityMessage()
	return s.status(), nil
}

func (s *FileProviderService) connectMount() error {
	if s.mount != nil {
		if s.mount.Alive() {
			return nil
		}
		// Unmounted from outside the app before Status noticed: mount again.
		s.disconnect()
	}
	if s.enrollment.MountPath == "" {
		s.enrollment.MountPath = defaultMountPath(s.dataDir)
	}
	store, err := fileprovider.Open(s.ctx, s.db)
	if err != nil {
		return err
	}
	mount, err := fpfuse.Start(s.ctx, store, s.enrollment.MountPath)
	if err != nil {
		return err
	}
	s.mount = mount
	if mount.Created() && !s.enrollment.CreatedMountDir {
		s.enrollment.CreatedMountDir = true
		if err := s.saveEnrollment(); err != nil {
			log.Printf("File manager mount: %v", err)
		}
	}
	if s.onMount != nil {
		s.onMount(mount.Dir())
	}
	return nil
}

func (s *FileProviderService) mountPath() string {
	if s.enrollment.MountPath != "" {
		return s.enrollment.MountPath
	}
	return defaultMountPath(s.dataDir)
}

// The standard data directory mounts at ~/Mahpastes. A custom
// MAHPASTES_DATA_DIR (a development or second profile) mounts inside its own
// data directory so two instances never compete for one mount point.
// MAHPASTES_MOUNT_DIR overrides both.
func defaultMountPath(dataDir string) string {
	if dir := os.Getenv("MAHPASTES_MOUNT_DIR"); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil && os.Getenv("MAHPASTES_DATA_DIR") == "" {
		return filepath.Join(home, "Mahpastes")
	}
	return filepath.Join(dataDir, "Mahpastes")
}
