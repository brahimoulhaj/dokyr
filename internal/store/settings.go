package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"

	"panel/internal/id"
	"panel/internal/store/sqlcgen"
)

// Setting keys.
const (
	SettingPanelDomain  = "panel_domain"          // hostname the panel is served on over HTTPS
	SettingACMEEmail    = "acme_email"            // account email for the certificate authority
	SettingPanelIPOff   = "panel_ip_access_off"   // "true": with a panel domain set, requests by IP (unknown hosts) are dropped
	SettingPanelRetired = "panel_domains_retired" // JSON list of hostnames the panel used to be served on
	SettingPanelTLS     = "panel_domain_tls"      // TLS mode of the panel domain: TLSAuto (default) or TLSHTTPOnly
	SettingOnboarded    = "onboarded_at"          // when the owner finished or skipped the onboarding steps
	SettingUpdateAuto   = "update_auto"           // "true": apply panel updates on their own
	SettingUpdateSeen   = "update_seen"           // finished_at of the last update result recorded as an event
	SettingBuildCPUs    = "build_cpus"            // CPU cap shared by Git builds; "0" = none; absent = the environment's default
	SettingBuildMemory  = "build_memory_mb"       // memory cap (MB) shared by Git builds; "0" = none; absent = the environment's default
	SettingBuildMax     = "build_concurrent"      // how many Git builds run at once; absent = the environment's default
	SettingRegistry     = "registry"              // JSON RegistrySettings of the built-in registry
	SettingRegistryS3   = "registry_s3"           // id of the S3 connection the registry stores in; absent = a Docker volume
	SettingHostTermOff  = "host_terminal_off"     // "true": nobody can open the host terminal
	SettingHostTermUsr  = "host_terminal_user"    // user the host terminal opens as; absent = root
	SettingHostTermIdle = "host_terminal_idle"    // minutes without input before a host shell is closed; "0" = never; absent = 30
)

// BuildLimits caps the CPU and memory that Git builds share, and how many of them run at
// once. A zero CPU or memory leaves that resource uncapped.
type BuildLimits struct {
	CPUs       float64 `json:"cpus"`
	MemoryMB   int64   `json:"memory_mb"`
	Concurrent int     `json:"concurrent"` // builds running at once, at least 1
}

// GetBuildLimits returns the build limits saved in Settings; a limit never saved there
// takes its value from def (the environment).
func (s *Store) GetBuildLimits(ctx context.Context, def BuildLimits) (BuildLimits, error) {
	out := def
	if v, ok, err := s.GetSetting(ctx, SettingBuildCPUs); err != nil {
		return def, err
	} else if n, perr := strconv.ParseFloat(v, 64); ok && perr == nil && n >= 0 && !math.IsInf(n, 0) {
		out.CPUs = n
	}
	if v, ok, err := s.GetSetting(ctx, SettingBuildMemory); err != nil {
		return def, err
	} else if n, perr := strconv.ParseInt(v, 10, 64); ok && perr == nil && n >= 0 {
		out.MemoryMB = n
	}
	if v, ok, err := s.GetSetting(ctx, SettingBuildMax); err != nil {
		return def, err
	} else if n, perr := strconv.Atoi(v); ok && perr == nil && n >= 1 {
		out.Concurrent = n
	}
	return out, nil
}

// SetBuildLimits saves the build limits, overriding the environment's defaults. A
// Concurrent below 1 leaves the saved count as it is.
func (s *Store) SetBuildLimits(ctx context.Context, l BuildLimits) error {
	if err := s.SetSetting(ctx, SettingBuildCPUs, strconv.FormatFloat(l.CPUs, 'f', -1, 64)); err != nil {
		return err
	}
	if err := s.SetSetting(ctx, SettingBuildMemory, strconv.FormatInt(l.MemoryMB, 10)); err != nil {
		return err
	}
	if l.Concurrent < 1 {
		return nil
	}
	return s.SetSetting(ctx, SettingBuildMax, strconv.Itoa(l.Concurrent))
}

// GetSetting returns the value and whether the key exists.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	v, err := s.q.GetSetting(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	ts := now()
	return s.q.SetSetting(ctx, sqlcgen.SetSettingParams{ID: id.New(), Key: key, Value: value, CreatedAt: ts, UpdatedAt: ts})
}

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	return s.q.DeleteSetting(ctx, key)
}
