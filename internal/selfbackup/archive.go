// Package selfbackup backs up the panel itself: its SQLite database and the key that
// decrypts the secrets inside it. A backup is one file, encrypted with a passphrase
// (age, scrypt), so storage that holds it learns nothing: the format is
//
//	age(passphrase) → gzip → tar { manifest.json, panel.db, secret.key }
//
// and can also be opened by hand with `age -d backup.tar.gz.age | tar -xz`.
package selfbackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"
)

const (
	formatVersion = 1
	manifestName  = "manifest.json"
	dbName        = "panel.db"
	keyName       = "secret.key"
	// Extension of backup files, after the timestamped name.
	Extension   = ".tar.gz.age"
	maxManifest = 1 << 20
	maxKey      = 1 << 10
)

// MinPassphrase is the shortest passphrase accepted. Development lowers it
// (DOKYR_WEAK_PASSWORDS); set it before the service is used.
var MinPassphrase = 12

// Manifest describes a backup.
type Manifest struct {
	Format        int       `json:"format"`
	Product       string    `json:"product"`
	PanelVersion  string    `json:"panel_version"`
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	DBSize        int64     `json:"db_size"`
	KeySHA256     string    `json:"key_sha256"`
}

// ErrPassphrase: the passphrase does not open this backup.
var ErrPassphrase = errors.New("wrong passphrase, or not a panel backup")

// Write encrypts a backup of the database snapshot at dbPath and the key at keyPath to w.
func Write(w io.Writer, passphrase string, m Manifest, dbPath, keyPath string) error {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read key: %w", err)
	}
	db, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open snapshot: %w", err)
	}
	defer db.Close()
	info, err := db.Stat()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(key)
	m.Format, m.DBSize, m.KeySHA256 = formatVersion, info.Size(), hex.EncodeToString(sum[:])
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return err
	}
	enc, err := age.Encrypt(w, recipient)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)
	add := func(name string, size int64, r io.Reader) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: m.CreatedAt, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := io.Copy(tw, r)
		return err
	}
	if err := add(manifestName, int64(len(manifest)), bytes.NewReader(manifest)); err != nil {
		return err
	}
	if err := add(dbName, info.Size(), db); err != nil {
		return err
	}
	if err := add(keyName, int64(len(key)), bytes.NewReader(key)); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return enc.Close()
}

// Extract decrypts a backup into dir (created, mode 0700) as panel.db and secret.key,
// and returns its manifest after checking that the key matches it.
func Extract(r io.Reader, passphrase, dir string) (Manifest, error) {
	var m Manifest
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return m, err
	}
	identity.SetMaxWorkFactor(22)
	dec, err := age.Decrypt(r, identity)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) || errors.Is(err, age.ErrIncorrectIdentity) {
			return m, ErrPassphrase
		}
		return m, fmt.Errorf("%w: %v", ErrPassphrase, err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return m, fmt.Errorf("not a panel backup: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return m, err
	}
	tr := tar.NewReader(gz)
	var key []byte
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return m, fmt.Errorf("read backup: %w", err)
		}
		if seen[h.Name] {
			return m, fmt.Errorf("backup lists %s twice", h.Name)
		}
		seen[h.Name] = true
		switch h.Name {
		case manifestName:
			raw, err := io.ReadAll(io.LimitReader(tr, maxManifest))
			if err != nil {
				return m, err
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				return m, fmt.Errorf("backup manifest: %w", err)
			}
			if m.Format != formatVersion {
				return m, fmt.Errorf("backup format %d is not supported by this version", m.Format)
			}
		case keyName:
			if key, err = io.ReadAll(io.LimitReader(tr, maxKey)); err != nil {
				return m, err
			}
			if err := os.WriteFile(filepath.Join(dir, keyName), key, 0o600); err != nil {
				return m, err
			}
		case dbName:
			f, err := os.OpenFile(filepath.Join(dir, dbName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return m, err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return m, fmt.Errorf("write database: %w", err)
			}
		default: // ignored: room for later formats to add files
		}
	}
	if !seen[manifestName] || !seen[dbName] || !seen[keyName] {
		return m, errors.New("backup is incomplete")
	}
	sum := sha256.Sum256(key)
	if hex.EncodeToString(sum[:]) != m.KeySHA256 {
		return m, errors.New("backup key does not match its manifest")
	}
	if info, err := os.Stat(filepath.Join(dir, dbName)); err != nil || info.Size() != m.DBSize {
		return m, errors.New("backup database is truncated")
	}
	return m, nil
}
