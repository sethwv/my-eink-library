package users

import (
	"database/sql"
	"time"

	"github.com/sethwv/my-sideload-library/internal/mail"
)

func (s *Store) GetSMTPSettings() (mail.Settings, error) {
	var m mail.Settings
	err := s.sql.QueryRow(`SELECT host, port, encryption, username, password, from_name, from_addr FROM smtp_settings WHERE id = 1`).Scan(&m.Host, &m.Port, &m.Encryption, &m.Username, &m.Password, &m.FromName, &m.FromAddress)
	if err == sql.ErrNoRows {
		return mail.Settings{}, nil
	}
	return m, err
}

func (s *Store) SaveSMTPSettings(m mail.Settings) error {
	_, err := s.sql.Exec(`INSERT INTO smtp_settings (id, host, port, encryption, username, password, from_name, from_addr) VALUES (1, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET host = excluded.host, port = excluded.port, encryption = excluded.encryption, username = excluded.username, password = excluded.password, from_name = excluded.from_name, from_addr = excluded.from_addr`, m.Host, m.Port, m.Encryption, m.Username, m.Password, m.FromName, m.FromAddress)
	return err
}

type IntegrationSettings struct {
	HardcoverEnabled        bool
	HardcoverToken          string
	ChaptarrEnabled         bool
	ChaptarrURL             string
	ChaptarrAPIKey          string
	HideNoChaptarrMatch     bool
	HideNoHardcoverMatch    bool
	HardcoverOverwriteCover bool
}

func (s *Store) GetIntegrationSettings() (IntegrationSettings, error) {
	var m IntegrationSettings
	var hardcoverEnabled, chaptarrEnabled, hideNoChaptarr, hideNoHardcover, overwriteCover int
	err := s.sql.QueryRow(`SELECT hardcover_enabled, hardcover_token, chaptarr_enabled, chaptarr_url, chaptarr_api_key, hide_no_chaptarr_match, hide_no_hardcover_match, hardcover_overwrite_cover FROM integration_settings WHERE id = 1`).Scan(&hardcoverEnabled, &m.HardcoverToken, &chaptarrEnabled, &m.ChaptarrURL, &m.ChaptarrAPIKey, &hideNoChaptarr, &hideNoHardcover, &overwriteCover)
	if err == sql.ErrNoRows {
		return IntegrationSettings{}, nil
	}
	if err != nil {
		return IntegrationSettings{}, err
	}
	m.HardcoverEnabled = hardcoverEnabled != 0
	m.ChaptarrEnabled = chaptarrEnabled != 0
	m.HideNoChaptarrMatch = hideNoChaptarr != 0
	m.HideNoHardcoverMatch = hideNoHardcover != 0
	m.HardcoverOverwriteCover = overwriteCover != 0
	return m, nil
}

func (s *Store) SaveIntegrationSettings(m IntegrationSettings) error {
	_, err := s.sql.Exec(`INSERT INTO integration_settings (id, hardcover_enabled, hardcover_token, chaptarr_enabled, chaptarr_url, chaptarr_api_key, hide_no_chaptarr_match, hide_no_hardcover_match, hardcover_overwrite_cover) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET hardcover_enabled = excluded.hardcover_enabled, hardcover_token = excluded.hardcover_token, chaptarr_enabled = excluded.chaptarr_enabled, chaptarr_url = excluded.chaptarr_url, chaptarr_api_key = excluded.chaptarr_api_key, hide_no_chaptarr_match = excluded.hide_no_chaptarr_match, hide_no_hardcover_match = excluded.hide_no_hardcover_match, hardcover_overwrite_cover = excluded.hardcover_overwrite_cover`, boolToInt(m.HardcoverEnabled), m.HardcoverToken, boolToInt(m.ChaptarrEnabled), m.ChaptarrURL, m.ChaptarrAPIKey, boolToInt(m.HideNoChaptarrMatch), boolToInt(m.HideNoHardcoverMatch), boolToInt(m.HardcoverOverwriteCover))
	return err
}

type GeneralSettings struct {
	SiteName   string
	PublicURL  string
	CoverWidth int
	PageSize   int
	SessionTTL time.Duration
}

// KepubSettings controls on-demand KEPUB conversion. It defaults to enabled so
// existing installations retain their current download behavior.
type KepubSettings struct {
	Enabled              bool
	WriteCalibreMetadata bool
}

func (s *Store) GetKepubSettings() (KepubSettings, error) {
	var enabled, writeCalibreMetadata int
	err := s.sql.QueryRow(`SELECT enabled, write_calibre_metadata FROM kepub_settings WHERE id = 1`).Scan(&enabled, &writeCalibreMetadata)
	if err == sql.ErrNoRows {
		return KepubSettings{Enabled: true}, nil
	}
	if err != nil {
		return KepubSettings{}, err
	}
	return KepubSettings{Enabled: enabled != 0, WriteCalibreMetadata: writeCalibreMetadata != 0}, nil
}

func (s *Store) SaveKepubSettings(m KepubSettings) error {
	_, err := s.sql.Exec(`INSERT INTO kepub_settings (id, enabled, write_calibre_metadata) VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET enabled = excluded.enabled, write_calibre_metadata = excluded.write_calibre_metadata`, boolToInt(m.Enabled), boolToInt(m.WriteCalibreMetadata))
	return err
}

func (s *Store) GetGeneralSettings() (GeneralSettings, error) {
	var m GeneralSettings
	var ttlSeconds int64
	err := s.sql.QueryRow(`SELECT site_name, public_url, cover_width, page_size, session_ttl_seconds FROM general_settings WHERE id = 1`).Scan(&m.SiteName, &m.PublicURL, &m.CoverWidth, &m.PageSize, &ttlSeconds)
	if err == sql.ErrNoRows {
		return GeneralSettings{}, nil
	}
	if err != nil {
		return GeneralSettings{}, err
	}
	m.SessionTTL = time.Duration(ttlSeconds) * time.Second
	return m, nil
}

func (s *Store) SaveGeneralSettings(m GeneralSettings) error {
	_, err := s.sql.Exec(`INSERT INTO general_settings (id, site_name, public_url, cover_width, page_size, session_ttl_seconds) VALUES (1, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET site_name = excluded.site_name, public_url = excluded.public_url, cover_width = excluded.cover_width, page_size = excluded.page_size, session_ttl_seconds = excluded.session_ttl_seconds`, m.SiteName, m.PublicURL, m.CoverWidth, m.PageSize, int64(m.SessionTTL/time.Second))
	return err
}
