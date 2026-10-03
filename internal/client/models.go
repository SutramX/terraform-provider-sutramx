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

// Routing is a connection's alert routing: scope "all", "groups" (group_ids)
// or "monitors" (monitor_ids). The API returns only the list of the scope.
type Routing struct {
	Scope      string   `json:"scope"`
	GroupIDs   []string `json:"group_ids,omitempty"`
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

// MaintenanceRecurrence is the repeat rule of a maintenance window.
type MaintenanceRecurrence struct {
	Type     string  `json:"type"`
	Weekdays []int64 `json:"weekdays"`
	Until    *string `json:"until"`
}

// MaintenanceWindow is one entry of GET /maintenance (camelCase, unlike most endpoints).
type MaintenanceWindow struct {
	ID               string                `json:"id"`
	Title            string                `json:"title"`
	Description      string                `json:"description"`
	Status           string                `json:"status"`
	EffectiveStatus  string                `json:"effectiveStatus"`
	StartTime        string                `json:"startTime"`
	EndTime          string                `json:"endTime"`
	Timezone         string                `json:"timezone"`
	Impact           string                `json:"impact"`
	ScopeType        string                `json:"scopeType"`
	MonitorIDs       []string              `json:"monitorIds"`
	GroupIDs         []string              `json:"groupIds"`
	AffectedServices []string              `json:"affectedServices"`
	Recurrence       MaintenanceRecurrence `json:"recurrence"`
}

type EscalationStep struct {
	ID           string  `json:"id"`
	StepOrder    int64   `json:"step_order"`
	DelayMinutes int64   `json:"delay_minutes"`
	Channel      *string `json:"channel"`
	TargetType   *string `json:"target_type"`
	TargetValue  string  `json:"target_value"`
	TargetLabel  *string `json:"target_label"`
}

// EscalationPolicy is one entry of GET /settings/escalation-policies.
type EscalationPolicy struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	IsEnabled bool             `json:"is_enabled"`
	MaxDepth  int64            `json:"max_depth"`
	Steps     []EscalationStep `json:"steps"`
}
