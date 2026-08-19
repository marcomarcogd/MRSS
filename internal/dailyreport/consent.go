package dailyreport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"MRSS/internal/config"
	"MRSS/internal/models"
)

const CloudProcessingDisclosureVersion = 1

type CloudProcessingDestination struct {
	ProfileID   *int64 `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	Endpoint    string `json:"endpoint"`
}

type CloudProcessingStatus struct {
	DisclosureVersion int                         `json:"disclosure_version"`
	Required          bool                        `json:"required"`
	Accepted          bool                        `json:"accepted"`
	AcceptedVersion   *int                        `json:"accepted_version"`
	AcceptedAt        *time.Time                  `json:"accepted_at"`
	Destination       *CloudProcessingDestination `json:"destination"`
}

type CloudConsentRequiredError struct {
	CloudProcessing CloudProcessingStatus
}

func (e *CloudConsentRequiredError) Error() string {
	return "cloud processing consent is required for the current AI destination"
}

type resolvedDestination struct {
	public      *CloudProcessingDestination
	fingerprint string
}

// AIProviderStore is the non-UI data needed to resolve the exact provider used
// by daily reports. Secret fields remain internal and must never be logged or
// serialized into report snapshots.
type AIProviderStore interface {
	GetAIProfile(int64) (*models.AIProfile, error)
	GetDefaultAIProfile() (*models.AIProfile, error)
	GetSetting(string) (string, error)
	GetEncryptedSetting(string) (string, error)
}

type ResolvedAIProvider struct {
	ProfileID     *int64 `json:"-"`
	ProfileName   string `json:"-"`
	APIKey        string `json:"-"`
	Endpoint      string `json:"-"`
	Model         string `json:"-"`
	CustomHeaders string `json:"-"`
	Legacy        bool   `json:"-"`
}

type AIProviderResolver struct{ store AIProviderStore }

func NewAIProviderResolver(store AIProviderStore) *AIProviderResolver {
	return &AIProviderResolver{store: store}
}

// Resolve returns the provider whose destination is covered by consent. It
// mirrors the summary-profile -> default-profile -> legacy fallback order.
func (r *AIProviderResolver) Resolve(reportConfig *models.DailyReportConfig) (*ResolvedAIProvider, error) {
	profile, err := r.resolveProfile(reportConfig)
	if err != nil {
		return nil, err
	}
	if profile != nil {
		id := profile.ID
		return &ResolvedAIProvider{
			ProfileID: &id, ProfileName: profile.Name, APIKey: profile.APIKey,
			Endpoint: profile.Endpoint, Model: profile.Model, CustomHeaders: profile.CustomHeaders,
		}, nil
	}
	endpoint, _ := r.store.GetSetting("ai_endpoint")
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, nil
	}
	apiKey, _ := r.store.GetEncryptedSetting("ai_api_key")
	if apiKey == "" && endpointsEquivalent(endpoint, strings.TrimSpace(config.Get().AIEndpoint)) {
		return nil, nil
	}
	model, _ := r.store.GetSetting("ai_model")
	customHeaders, _ := r.store.GetSetting("ai_custom_headers")
	return &ResolvedAIProvider{
		ProfileName: "Legacy AI configuration", APIKey: apiKey, Endpoint: endpoint,
		Model: model, CustomHeaders: customHeaders, Legacy: true,
	}, nil
}

func (r *AIProviderResolver) resolveProfile(reportConfig *models.DailyReportConfig) (*models.AIProfile, error) {
	if reportConfig != nil && reportConfig.AIProfileID != nil && *reportConfig.AIProfileID > 0 {
		return r.store.GetAIProfile(*reportConfig.AIProfileID)
	}
	profileIDText, _ := r.store.GetSetting("ai_summary_profile_id")
	if profileID, err := strconv.ParseInt(profileIDText, 10, 64); err == nil && profileID > 0 {
		profile, profileErr := r.store.GetAIProfile(profileID)
		if profileErr != nil {
			return nil, profileErr
		}
		if profile != nil {
			return profile, nil
		}
	}
	return r.store.GetDefaultAIProfile()
}

func (s *Service) GetCloudProcessing(config *models.DailyReportConfig) (CloudProcessingStatus, error) {
	if config == nil {
		var err error
		config, err = s.store.GetDailyReportConfig()
		if err != nil {
			return CloudProcessingStatus{}, err
		}
	}
	destination, err := s.resolveCloudDestination(config)
	if err != nil {
		return CloudProcessingStatus{}, err
	}
	status := CloudProcessingStatus{
		DisclosureVersion: CloudProcessingDisclosureVersion,
		Accepted:          destination == nil,
		Destination:       nil,
	}
	if destination == nil {
		return status, nil
	}
	status.Required = true
	status.Destination = destination.public
	status.Accepted = config.CloudConsentVersion == CloudProcessingDisclosureVersion &&
		config.CloudConsentAt != nil &&
		config.CloudConsentFingerprint != "" &&
		config.CloudConsentFingerprint == destination.fingerprint
	if status.Accepted {
		version := config.CloudConsentVersion
		status.AcceptedVersion = &version
		status.AcceptedAt = config.CloudConsentAt
	}
	return status, nil
}

func (s *Service) GrantCloudProcessingConsent(version int) (CloudProcessingStatus, error) {
	if version != CloudProcessingDisclosureVersion {
		return CloudProcessingStatus{}, fmt.Errorf("version must be %d", CloudProcessingDisclosureVersion)
	}
	config, err := s.GetConfig()
	if err != nil {
		return CloudProcessingStatus{}, err
	}
	destination, err := s.resolveCloudDestination(config)
	if err != nil {
		return CloudProcessingStatus{}, err
	}
	if destination == nil {
		config.CloudConsentVersion = 0
		config.CloudConsentAt = nil
		config.CloudConsentFingerprint = ""
	} else {
		now := s.clock.Now().UTC()
		config.CloudConsentVersion = CloudProcessingDisclosureVersion
		config.CloudConsentAt = &now
		config.CloudConsentFingerprint = destination.fingerprint
	}
	if err := s.store.SaveDailyReportConfig(config, config.FeedIDs); err != nil {
		return CloudProcessingStatus{}, err
	}
	return s.GetCloudProcessing(config)
}

func (s *Service) RevokeCloudProcessingConsent() (CloudProcessingStatus, error) {
	config, err := s.GetConfig()
	if err != nil {
		return CloudProcessingStatus{}, err
	}
	config.Enabled = false
	config.CloudConsentVersion = 0
	config.CloudConsentAt = nil
	config.CloudConsentFingerprint = ""
	if err := s.store.SaveDailyReportConfig(config, config.FeedIDs); err != nil {
		return CloudProcessingStatus{}, err
	}
	s.mu.RLock()
	wake := s.wake
	s.mu.RUnlock()
	if wake != nil {
		wake()
	}
	return s.GetCloudProcessing(config)
}

func (s *Service) ensureCloudProcessingConsent(config *models.DailyReportConfig) error {
	status, err := s.GetCloudProcessing(config)
	if err != nil {
		return err
	}
	if status.Required && !status.Accepted {
		return &CloudConsentRequiredError{CloudProcessing: status}
	}
	return nil
}

// invalidateStaleCloudConsent clears a grant whenever the selected destination
// changes, including a temporary switch to local-only processing. Returning to
// an older profile therefore requires a fresh user decision instead of silently
// reviving a historical fingerprint. Model-only changes keep the same grant.
func (s *Service) invalidateStaleCloudConsent(config *models.DailyReportConfig) (bool, error) {
	if config == nil || (config.CloudConsentVersion == 0 && config.CloudConsentAt == nil && config.CloudConsentFingerprint == "") {
		return false, nil
	}
	destination, err := s.resolveCloudDestination(config)
	if err != nil {
		return false, err
	}
	fingerprint := ""
	if destination != nil {
		fingerprint = destination.fingerprint
	}
	if config.CloudConsentVersion == CloudProcessingDisclosureVersion &&
		config.CloudConsentAt != nil &&
		config.CloudConsentFingerprint != "" &&
		config.CloudConsentFingerprint == fingerprint {
		return false, nil
	}
	config.CloudConsentVersion = 0
	config.CloudConsentAt = nil
	config.CloudConsentFingerprint = ""
	return true, nil
}

// EnsureCloudProcessingConsent is used by network-capable generators before
// every request. It checks the persisted grant, not the run's configuration
// snapshot, and binds it to the exact provider about to receive RSS data.
func (s *Service) EnsureCloudProcessingConsent(config *models.DailyReportConfig, provider *ResolvedAIProvider) error {
	if provider == nil {
		return nil
	}
	attempted, err := resolvedProviderDestination(provider)
	if err != nil {
		return err
	}
	if attempted == nil {
		// Loopback providers are local processing and never need cloud consent.
		return nil
	}

	current, err := s.GetConfig()
	if err != nil {
		return err
	}
	status, err := s.GetCloudProcessing(current)
	if err != nil {
		return err
	}
	currentDestination, err := s.resolveCloudDestination(current)
	if err != nil {
		return err
	}
	if !status.Accepted || currentDestination == nil || currentDestination.fingerprint != attempted.fingerprint {
		return &CloudConsentRequiredError{CloudProcessing: status}
	}
	return nil
}

func (s *Service) resolveCloudDestination(config *models.DailyReportConfig) (*resolvedDestination, error) {
	provider, err := NewAIProviderResolver(s.store).Resolve(config)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, nil
	}
	return resolvedProviderDestination(provider)
}

func resolvedProviderDestination(provider *ResolvedAIProvider) (*resolvedDestination, error) {
	if provider == nil {
		return nil, nil
	}
	identity := "legacy"
	profileID := int64(0)
	if provider.ProfileID != nil {
		profileID = *provider.ProfileID
		identity = "profile:" + strconv.FormatInt(profileID, 10)
	}
	return makeResolvedDestination(profileID, provider.ProfileName, provider.Endpoint, identity)
}

func makeResolvedDestination(profileID int64, profileName, endpoint, identity string) (*resolvedDestination, error) {
	canonical, local, err := CanonicalCloudEndpoint(endpoint)
	if err != nil || local || canonical == "" {
		return nil, nil
	}
	var publicProfileID *int64
	if profileID > 0 {
		id := profileID
		publicProfileID = &id
	}
	name := strings.TrimSpace(profileName)
	if len([]rune(name)) > 120 {
		name = string([]rune(name)[:120])
	}
	digest := sha256.Sum256([]byte(identity + "\x00" + canonical))
	return &resolvedDestination{
		public: &CloudProcessingDestination{
			ProfileID: publicProfileID, ProfileName: name, Endpoint: canonical,
		},
		fingerprint: hex.EncodeToString(digest[:]),
	}, nil
}

func CanonicalCloudEndpoint(raw string) (canonical string, local bool, err error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", false, fmt.Errorf("AI endpoint is invalid")
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return "", true, nil
	}
	if ip := net.ParseIP(hostname); ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
		return "", true, nil
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String(), false, nil
}

func endpointsEquivalent(left, right string) bool {
	leftCanonical, _, leftErr := CanonicalCloudEndpoint(left)
	rightCanonical, _, rightErr := CanonicalCloudEndpoint(right)
	return leftErr == nil && rightErr == nil && leftCanonical == rightCanonical
}
