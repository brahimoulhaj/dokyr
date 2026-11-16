package store

import (
	"context"
	"encoding/json"
	"errors"
)

const settingObjectStore = "object_store"

// ObjectStoreSettings configure the built-in S3 storage (RustFS). Off until enabled. The
// root credentials are made once and kept when it is turned off, so the data stays
// reachable; SecretKey is sealed with the panel key at rest.
type ObjectStoreSettings struct {
	Enabled bool   `json:"enabled"`
	Domain  string `json:"domain"`   // optional: serves the S3 API outside the server, through the proxy
	TLSMode string `json:"tls_mode"` // TLSAuto ("" too) or TLSHTTPOnly
	// ConnectionID is the S3 connection the panel made for it (backups, the registry).
	ConnectionID string `json:"connection_id"`
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"-"`
}

type storedObjectStore struct {
	ObjectStoreSettings
	SealedSecret string `json:"secret_key"`
}

// GetObjectStoreSettings returns the saved settings; the zero value (off) when never saved.
func (s *Store) GetObjectStoreSettings(ctx context.Context) (ObjectStoreSettings, error) {
	raw, ok, err := s.GetSetting(ctx, settingObjectStore)
	if err != nil || !ok {
		return ObjectStoreSettings{}, err
	}
	var st storedObjectStore
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return ObjectStoreSettings{}, err
	}
	out := st.ObjectStoreSettings
	if st.SealedSecret != "" {
		if s.box == nil {
			return out, errors.New("object storage: no encryption key loaded")
		}
		plain, err := s.box.Decrypt(st.SealedSecret)
		if err != nil {
			return out, err
		}
		out.SecretKey = string(plain)
	}
	return out, nil
}

func (s *Store) SetObjectStoreSettings(ctx context.Context, in ObjectStoreSettings) error {
	st := storedObjectStore{ObjectStoreSettings: in}
	if in.SecretKey != "" {
		if s.box == nil {
			return errors.New("object storage: no encryption key loaded")
		}
		sealed, err := s.box.Encrypt([]byte(in.SecretKey))
		if err != nil {
			return err
		}
		st.SealedSecret = sealed
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, settingObjectStore, string(raw))
}
