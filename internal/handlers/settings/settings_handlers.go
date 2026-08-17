package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"MRSS/internal/handlers/core"
	"MRSS/internal/handlers/response"
	appUtils "MRSS/internal/utils"
)

var (
	errInvalidStartupValue     = errors.New("startup_on_boot must be true or false")
	enableStartupRegistration  = appUtils.EnableStartup
	disableStartupRegistration = appUtils.DisableStartup
)

type startupSettingChange struct {
	previous  bool
	requested bool
	changed   bool
}

// safeGetEncryptedSetting safely retrieves an encrypted setting, returning empty string on error.
// This prevents JSON encoding errors when encrypted data is corrupted or cannot be decrypted.
func safeGetEncryptedSetting(h *core.Handler, key string) string {
	value, err := h.DB.GetEncryptedSetting(key)
	if err != nil {
		log.Printf("Warning: Failed to decrypt setting %s: %v. Returning empty string.", key, err)
		return ""
	}
	return sanitizeValue(value)
}

// safeGetSetting safely retrieves a setting, returning empty string on error.
func safeGetSetting(h *core.Handler, key string) string {
	value, err := h.DB.GetSetting(key)
	if err != nil {
		log.Printf("Warning: Failed to retrieve setting %s: %v. Returning empty string.", key, err)
		return ""
	}
	return sanitizeValue(value)
}

// sanitizeValue removes control characters that could break JSON encoding.
func sanitizeValue(value string) string {
	// Remove control characters that could break JSON
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1 // Remove control characters except tab, newline, carriage return
		}
		return r
	}, value)
}

// HandleSettings handles GET and POST requests for application settings.
// Uses the definition-driven approach from settings_base.go for cleaner code.
func HandleSettings(h *core.Handler, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Get all settings using the definition-driven approach
		settings := GetAllSettings(h)
		response.JSON(w, settings)

	case http.MethodPost:
		// Parse request body as a generic map
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, err, http.StatusBadRequest)
			return
		}

		startupChange, err := prepareStartupSettingChange(h, req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errInvalidStartupValue) {
				status = http.StatusBadRequest
			}
			log.Printf("Failed to prepare startup setting change: %v", err)
			response.Error(w, err, status)
			return
		}
		delete(req, "startup_on_boot")

		if mode, ok := req["translation_mode"]; ok {
			mode = strings.ToLower(strings.TrimSpace(mode))
			if mode != "manual" && mode != "auto" && mode != "off" {
				log.Printf("Warning: invalid translation_mode %q; falling back to manual", req["translation_mode"])
				mode = "manual"
			}
			req["translation_mode"] = mode
			// Keep the legacy toggle as a downgrade-compatible mirror. Manual mode
			// must stay disabled so an older build never starts translating on open.
			if mode == "auto" {
				req["translation_enabled"] = "true"
			} else {
				req["translation_enabled"] = "false"
			}
		}

		wasFreshRSSEnabled := false
		if _, ok := req["freshrss_enabled"]; ok {
			currentValue, err := h.DB.GetSetting("freshrss_enabled")
			if err == nil {
				wasFreshRSSEnabled = currentValue == "true"
			}
		}

		// Save settings using the definition-driven approach
		if err := SaveSettings(h, req); err != nil {
			log.Printf("Failed to save settings: %v", err)
			response.Error(w, err, http.StatusInternalServerError)
			return
		}

		if shouldCleanupFreshRSSData(wasFreshRSSEnabled, req["freshrss_enabled"]) {
			if err := h.DB.CleanupFreshRSSData(); err != nil {
				log.Printf("Failed to cleanup FreshRSS data after disabling sync: %v", err)
				response.Error(w, err, http.StatusInternalServerError)
				return
			}
		}

		if err := persistStartupSettingChange(h, startupChange); err != nil {
			log.Printf("Failed to update startup registration: %v", err)
			response.Error(w, err, http.StatusInternalServerError)
			return
		}

		// Re-fetch all settings after save to return updated values
		settings := GetAllSettings(h)
		response.JSON(w, settings)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func prepareStartupSettingChange(h *core.Handler, req map[string]string) (*startupSettingChange, error) {
	requestedValue, ok := req["startup_on_boot"]
	if !ok {
		return nil, nil
	}

	requested, err := parseStartupSetting(requestedValue)
	if err != nil {
		return nil, err
	}

	currentValue, err := h.DB.GetSetting("startup_on_boot")
	if err != nil {
		return nil, fmt.Errorf("failed to read current startup setting: %w", err)
	}
	previous := strings.EqualFold(strings.TrimSpace(currentValue), "true")

	return &startupSettingChange{
		previous:  previous,
		requested: requested,
		changed:   previous != requested,
	}, nil
}

func parseStartupSetting(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errInvalidStartupValue
	}
}

func persistStartupSettingChange(h *core.Handler, change *startupSettingChange) error {
	if change == nil || !change.changed {
		return nil
	}

	if err := setStartupRegistration(change.requested); err != nil {
		return fmt.Errorf("failed to apply startup registration: %w", err)
	}

	value := "false"
	if change.requested {
		value = "true"
	}
	if err := h.DB.SetSetting("startup_on_boot", value); err != nil {
		rollbackErr := setStartupRegistration(change.previous)
		if rollbackErr != nil {
			return fmt.Errorf("failed to persist startup setting: %w; failed to roll back startup registration: %v", err, rollbackErr)
		}
		return fmt.Errorf("failed to persist startup setting: %w", err)
	}

	return nil
}

func setStartupRegistration(enabled bool) error {
	if enabled {
		return enableStartupRegistration()
	}
	return disableStartupRegistration()
}

func shouldCleanupFreshRSSData(wasEnabled bool, newValue string) bool {
	return wasEnabled && strings.EqualFold(strings.TrimSpace(newValue), "false")
}
