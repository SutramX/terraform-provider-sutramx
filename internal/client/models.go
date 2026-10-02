package client

import "encoding/json"

// Monitor is the subset of the monitor object the provider manages.
type Monitor struct {
	ID              string          `json:"id"`
	ExternalID      *string         `json:"external_id"`
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	URL             *string         `json:"url"`
	IntervalSeconds int64           `json:"interval_seconds"`
	IsActive        bool            `json:"is_active"`
	Config          json.RawMessage `json:"config"`
	Tags            []string        `json:"tags"`
	ProbeRegions    []string        `json:"probe_regions"`
	HeartbeatURL    *string         `json:"heartbeat_url"`
}

// MonitorUpsertResponse is PUT /automation/monitors/:key.
type MonitorUpsertResponse struct {
	Action  string  `json:"action"`
	Monitor Monitor `json:"monitor"`
}

type StatusPageMonitor struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Section *string `json:"section"`
}

type StatusPage struct {
	ID                string              `json:"id"`
	Slug              string              `json:"slug"`
	Title             string              `json:"title"`
	Description       *string             `json:"description"`
	IsPublic          bool                `json:"is_public"`
	LogoURL           *string             `json:"logo_url"`
	AccentColor       *string             `json:"accent_color"`
	FaviconURL        *string             `json:"favicon_url"`
	ShowResponseTimes bool                `json:"show_response_times"`
	IsWhitelabel      bool                `json:"is_whitelabel"`
	CustomDomain      *string             `json:"custom_domain"`
	Monitors          []StatusPageMonitor `json:"monitors"`
}

type Routing struct {
	Scope      string   `json:"scope"`
	MonitorIDs []string `json:"monitor_ids,omitempty"`
}

type Connection struct {
	ID              string         `json:"id"`
	IntegrationType string         `json:"integration_type"`
	Name            string         `json:"name"`
	Status          string         `json:"status"`
	Config          map[string]any `json:"config"`
	Routing         Routing        `json:"routing"`
}

type IntegrationsResponse struct {
	Connections []Connection `json:"connections"`
}

// ConnectionSaveResponse is POST /integrations/:type/connections and PUT /integrations/connections/:id.
type ConnectionSaveResponse struct {
	ConnectionID  string  `json:"connection_id"`
	Name          string  `json:"name"`
	SigningSecret *string `json:"signing_secret"`
}

type Region struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	City      *string  `json:"city"`
	Country   *string  `json:"country"`
	Continent *string  `json:"continent"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Online    bool     `json:"online"`
}

type RegionsResponse struct {
	Regions []Region `json:"regions"`
}

type MarketPrice struct {
	Monthly  float64 `json:"monthly"`
	Annual   float64 `json:"annual"`
	Currency string  `json:"currency"`
}

type PlanLimits struct {
	Monitors           *int64 `json:"monitors"`
	MinIntervalSeconds *int64 `json:"minIntervalSeconds"`
	ProbeLocations     *int64 `json:"probeLocations"`
	StatusPages        *int64 `json:"statusPages"`
	APIKeys            *int64 `json:"apiKeys"`
}

type Plan struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	IsPopular   bool                   `json:"isPopular"`
	SortOrder   int64                  `json:"sortOrder"`
	Markets     []string               `json:"markets"`
	Pricing     map[string]MarketPrice `json:"pricing"`
	Limits      PlanLimits             `json:"limits"`
}

type PlansResponse struct {
	Plans []Plan `json:"plans"`
}
