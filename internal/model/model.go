package model

import (
	"encoding/json"
	"time"
)

type Provider struct {
	Service     string          `json:"service"`
	DisplayName string          `json:"displayName"`
	Description string          `json:"description,omitempty"`
	Categories  []string        `json:"categories"`
	AuthTypes   []string        `json:"authTypes"`
	Auth        json.RawMessage `json:"auth,omitempty"`
	HomepageURL string          `json:"homepageUrl,omitempty"`
	BaseURL     string          `json:"x-apihub-base-url,omitempty"`
	Credential  *CredentialSpec `json:"x-apihub-credential,omitempty"`
	Actions     []Action        `json:"actions"`
}

type CredentialSpec struct {
	Field  string `json:"field"`
	In     string `json:"in"`
	Name   string `json:"name"`
	Prefix string `json:"prefix,omitempty"`
}

type Action struct {
	ID                  string                 `json:"id"`
	Service             string                 `json:"service"`
	Name                string                 `json:"name"`
	Description         string                 `json:"description"`
	RequiredScopes      []string               `json:"requiredScopes"`
	ProviderPermissions []string               `json:"providerPermissions"`
	InputSchema         map[string]any         `json:"inputSchema"`
	OutputSchema        map[string]any         `json:"outputSchema"`
	Runtime             *HTTPActionRuntime     `json:"x-apihub-runtime,omitempty"`
	Executable          bool                   `json:"executable"`
	Extra               map[string]interface{} `json:"-"`
}

type HTTPActionRuntime struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type Workspace struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SystemGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type System struct {
	ID              string    `json:"id"`
	SystemKey       string    `json:"systemKey"`
	Name            string    `json:"name"`
	Source          string    `json:"source"`
	GroupID         string    `json:"groupId,omitempty"`
	GroupName       string    `json:"groupName,omitempty"`
	Description     string    `json:"description"`
	HomepageURL     string    `json:"homepageUrl,omitempty"`
	IconKey         string    `json:"iconKey,omitempty"`
	Status          string    `json:"status"`
	CatalogVersion  string    `json:"catalogVersion,omitempty"`
	AuthTemplateIDs []string  `json:"authTemplateIds"`
	ActionCount     int       `json:"actionCount"`
	ExecutableCount int       `json:"executableCount"`
	ConnectionCount int       `json:"connectionCount"`
	Version         int64     `json:"version"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type AuthTemplate struct {
	ID               string          `json:"id"`
	TemplateKey      string          `json:"templateKey"`
	Name             string          `json:"name"`
	Source           string          `json:"source"`
	FlowType         string          `json:"flowType"`
	Status           string          `json:"status"`
	CredentialSchema json.RawMessage `json:"credentialSchema"`
	TokenRequest     json.RawMessage `json:"tokenRequest"`
	InjectionRules   json.RawMessage `json:"injectionRules"`
	Version          int64           `json:"version"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type AuthInstance struct {
	ID                  string          `json:"id"`
	InstanceKey         string          `json:"instanceKey"`
	Name                string          `json:"name"`
	SystemID            string          `json:"systemId"`
	SystemKey           string          `json:"systemKey,omitempty"`
	AuthTemplateID      string          `json:"authTemplateId"`
	AuthTemplateKey     string          `json:"authTemplateKey,omitempty"`
	AuthTemplateFlow    string          `json:"authTemplateFlow,omitempty"`
	Status              string          `json:"status"`
	TokenURL            string          `json:"tokenUrl,omitempty"`
	RefreshURL          string          `json:"refreshUrl,omitempty"`
	TokenPath           string          `json:"tokenPath,omitempty"`
	ExpiryPath          string          `json:"expiryPath,omitempty"`
	HeaderName          string          `json:"headerName,omitempty"`
	HeaderValueTemplate string          `json:"headerValueTemplate,omitempty"`
	PublicConfig        json.RawMessage `json:"publicConfig"`
	SecretBlob          []byte          `json:"-"`
	KeyVersion          int16           `json:"keyVersion"`
	Version             int64           `json:"version"`
	LastTestedAt        *time.Time      `json:"lastTestedAt,omitempty"`
	LastTestStatus      string          `json:"lastTestStatus,omitempty"`
	CreatedAt           time.Time       `json:"createdAt"`
	UpdatedAt           time.Time       `json:"updatedAt"`
}

type Integration struct {
	ID             string          `json:"id"`
	WorkspaceID    string          `json:"-"`
	IntegrationKey string          `json:"integrationKey"`
	Name           string          `json:"name"`
	SystemID       string          `json:"systemId"`
	SystemKey      string          `json:"systemKey"`
	SystemName     string          `json:"systemName"`
	AuthInstanceID string          `json:"authInstanceId"`
	AuthName       string          `json:"authName"`
	BaseURL        string          `json:"baseUrl"`
	Status         string          `json:"status"`
	Settings       json.RawMessage `json:"settings"`
	Version        int64           `json:"version"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type EndUser struct {
	ID          string          `json:"id"`
	ExternalKey string          `json:"externalKey"`
	DisplayName string          `json:"displayName,omitempty"`
	Email       string          `json:"email,omitempty"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

type Connection struct {
	ID               string          `json:"id"`
	WorkspaceID      string          `json:"-"`
	ConnectionKey    string          `json:"connectionKey"`
	Name             string          `json:"name"`
	IntegrationID    string          `json:"integrationId"`
	IntegrationKey   string          `json:"integrationKey"`
	SystemKey        string          `json:"systemKey"`
	AuthInstanceID   string          `json:"authInstanceId"`
	EndUserID        string          `json:"endUserId"`
	EndUserKey       string          `json:"endUserKey"`
	EndUserName      string          `json:"endUserName"`
	EndUserEmail     string          `json:"endUserEmail"`
	Metadata         json.RawMessage `json:"metadata"`
	Status           string          `json:"status"`
	CredentialBlob   []byte          `json:"-"`
	KeyVersion       int16           `json:"keyVersion"`
	Revision         int64           `json:"revision"`
	TokenExpiresAt   *time.Time      `json:"tokenExpiresAt,omitempty"`
	LastVerifiedAt   *time.Time      `json:"lastVerifiedAt,omitempty"`
	LastUsedAt       *time.Time      `json:"lastUsedAt,omitempty"`
	LastErrorCode    string          `json:"lastErrorCode,omitempty"`
	LastErrorMessage string          `json:"lastErrorMessage,omitempty"`
	Tags             []string        `json:"tags"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type ActionDefinition struct {
	ID             string          `json:"id"`
	ActionKey      string          `json:"actionKey"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Source         string          `json:"source"`
	SystemID       string          `json:"systemId"`
	SystemKey      string          `json:"systemKey"`
	IntegrationID  string          `json:"integrationId,omitempty"`
	HTTPMethod     string          `json:"httpMethod"`
	RelativePath   string          `json:"relativePath"`
	RequiredScopes []string        `json:"requiredScopes"`
	InputSchema    json.RawMessage `json:"inputSchema"`
	OutputSchema   json.RawMessage `json:"outputSchema"`
	ExampleInput   json.RawMessage `json:"exampleInput"`
	Executable     bool            `json:"executable"`
	Status         string          `json:"status"`
	Version        int64           `json:"version"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type RuntimeToken struct {
	ID                 string     `json:"id"`
	WorkspaceID        string     `json:"-"`
	Name               string     `json:"name"`
	TokenPrefix        string     `json:"tokenPrefix"`
	TokenHash          string     `json:"-"`
	Status             string     `json:"status"`
	AllowedActions     []string   `json:"allowedActions"`
	BlockedActions     []string   `json:"blockedActions"`
	AllowedConnections []string   `json:"allowedConnections"`
	ExpiresAt          *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt         *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt          *time.Time `json:"revokedAt,omitempty"`
	CreatedBy          string     `json:"createdBy"`
	CreatedAt          time.Time  `json:"createdAt"`
}

type SyncTask struct {
	ID               string          `json:"id"`
	TaskKey          string          `json:"taskKey"`
	Name             string          `json:"name"`
	IntegrationID    string          `json:"integrationId"`
	IntegrationKey   string          `json:"integrationKey"`
	ConnectionID     string          `json:"connectionId"`
	ActionID         string          `json:"actionId"`
	ActionKey        string          `json:"actionKey"`
	Status           string          `json:"status"`
	ScheduleType     string          `json:"scheduleType"`
	CronExpression   string          `json:"cronExpression,omitempty"`
	ScheduleTimezone string          `json:"scheduleTimezone"`
	NextRunAt        *time.Time      `json:"nextRunAt,omitempty"`
	RetryPolicy      json.RawMessage `json:"retryPolicy"`
	Input            json.RawMessage `json:"input"`
	SyncConfig       json.RawMessage `json:"syncConfig"`
	Checkpoint       json.RawMessage `json:"checkpoint"`
	LastSuccessAt    *time.Time      `json:"lastSuccessAt,omitempty"`
	LastErrorAt      *time.Time      `json:"lastErrorAt,omitempty"`
	RecordsActive    int64           `json:"recordsActive"`
	Version          int64           `json:"version"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type WebhookEndpoint struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	PrimaryURL       string     `json:"primaryUrl"`
	FallbackURL      string     `json:"fallbackUrl,omitempty"`
	SecretBlob       []byte     `json:"-"`
	KeyVersion       int16      `json:"keyVersion"`
	Status           string     `json:"status"`
	FailureCount     int        `json:"failureCount"`
	PausedAt         *time.Time `json:"pausedAt,omitempty"`
	SubscribedEvents []string   `json:"subscribedEvents"`
	Version          int64      `json:"version"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type WebhookSource struct {
	ID               string          `json:"id"`
	SourceKey        string          `json:"sourceKey"`
	Name             string          `json:"name"`
	IntegrationID    string          `json:"integrationId"`
	IntegrationKey   string          `json:"integrationKey"`
	Status           string          `json:"status"`
	SignatureType    string          `json:"signatureType"`
	SecretBlob       []byte          `json:"-"`
	KeyVersion       int16           `json:"keyVersion"`
	SubscribedEvents []string        `json:"subscribedEvents"`
	Settings         json.RawMessage `json:"settings"`
	Version          int64           `json:"version"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type WebhookDelivery struct {
	ID             string     `json:"id"`
	EventType      string     `json:"eventType"`
	EndpointName   string     `json:"endpointName"`
	TargetURL      string     `json:"targetUrl"`
	Status         string     `json:"status"`
	AttemptCount   int        `json:"attemptCount"`
	NextAttemptAt  *time.Time `json:"nextAttemptAt,omitempty"`
	LastHTTPStatus int        `json:"lastHttpStatus,omitempty"`
	LastError      string     `json:"lastError,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	DeliveredAt    *time.Time `json:"deliveredAt,omitempty"`
}

type WebhookDeliveryAttempt struct {
	ID                string    `json:"id"`
	WebhookDeliveryID string    `json:"webhookDeliveryId"`
	AttemptNo         int       `json:"attemptNo"`
	TargetURL         string    `json:"targetUrl"`
	RequestID         string    `json:"requestId"`
	HTTPStatus        int       `json:"httpStatus,omitempty"`
	DurationMS        int64     `json:"durationMs"`
	ErrorMessage      string    `json:"errorMessage,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
}

type Job struct {
	ID             string          `json:"id"`
	WorkspaceID    string          `json:"-"`
	Kind           string          `json:"kind"`
	ResourceID     string          `json:"resourceId,omitempty"`
	OperationID    string          `json:"operationId,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	Status         string          `json:"status"`
	Priority       int16           `json:"priority"`
	Attempt        int             `json:"attempt"`
	MaxAttempts    int             `json:"maxAttempts"`
	RunAfter       time.Time       `json:"runAfter"`
	LeaseOwner     string          `json:"leaseOwner,omitempty"`
	LeaseExpiresAt *time.Time      `json:"leaseExpiresAt,omitempty"`
	LastError      string          `json:"lastError,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type OutboxEvent struct {
	ID            string          `json:"id"`
	EventType     string          `json:"eventType"`
	AggregateType string          `json:"aggregateType"`
	AggregateID   string          `json:"aggregateId"`
	DedupeKey     string          `json:"dedupeKey"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type SyncRecord struct {
	ID              string          `json:"id"`
	SyncTaskID      string          `json:"syncTaskId"`
	Model           string          `json:"model"`
	ExternalID      string          `json:"externalId"`
	Payload         json.RawMessage `json:"payload"`
	PayloadHash     string          `json:"payloadHash"`
	SourceCreatedAt *time.Time      `json:"sourceCreatedAt,omitempty"`
	SourceUpdatedAt *time.Time      `json:"sourceUpdatedAt,omitempty"`
	FirstSeenAt     time.Time       `json:"firstSeenAt"`
	LastSeenAt      time.Time       `json:"lastSeenAt"`
	DeletedAt       *time.Time      `json:"deletedAt,omitempty"`
}

type OperationRun struct {
	ID             string           `json:"id"`
	WorkspaceID    string           `json:"-"`
	RequestID      string           `json:"requestId"`
	Kind           string           `json:"kind"`
	Name           string           `json:"name"`
	Status         string           `json:"status"`
	SystemID       string           `json:"systemId,omitempty"`
	IntegrationID  string           `json:"integrationId,omitempty"`
	ConnectionID   string           `json:"connectionId,omitempty"`
	ActionID       string           `json:"actionId,omitempty"`
	SyncTaskID     string           `json:"syncTaskId,omitempty"`
	RuntimeTokenID string           `json:"runtimeTokenId,omitempty"`
	Source         string           `json:"source"`
	HTTPStatus     int              `json:"httpStatus,omitempty"`
	Input          json.RawMessage  `json:"input,omitempty"`
	Output         json.RawMessage  `json:"output,omitempty"`
	ErrorCode      string           `json:"errorCode,omitempty"`
	ErrorMessage   string           `json:"errorMessage,omitempty"`
	StartedAt      time.Time        `json:"startedAt"`
	CompletedAt    *time.Time       `json:"completedAt,omitempty"`
	ExpiresAt      time.Time        `json:"expiresAt"`
	Events         []OperationEvent `json:"events,omitempty"`
}

type OperationEvent struct {
	ID         string          `json:"id"`
	Sequence   int             `json:"sequence"`
	Level      string          `json:"level"`
	Message    string          `json:"message"`
	Attributes json.RawMessage `json:"attributes"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type IdempotencyRecord struct {
	ID             string
	WorkspaceID    string
	RuntimeTokenID string
	Scope          string
	Key            string
	Fingerprint    string
	Status         string
	HTTPStatus     int
	Response       []byte
	OperationID    string
	ExpiresAt      time.Time
}

type TeamMember struct {
	ID          string    `json:"id"`
	UserID      string    `json:"userId"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type AuditLog struct {
	ID            string          `json:"id"`
	RequestID     string          `json:"requestId"`
	ActorType     string          `json:"actorType"`
	ActorID       string          `json:"actorId,omitempty"`
	ActorLabel    string          `json:"actorLabel"`
	Action        string          `json:"action"`
	ResourceType  string          `json:"resourceType"`
	ResourceID    string          `json:"resourceId,omitempty"`
	ResourceLabel string          `json:"resourceLabel,omitempty"`
	IPAddress     string          `json:"ipAddress,omitempty"`
	BeforeData    json.RawMessage `json:"beforeData,omitempty"`
	AfterData     json.RawMessage `json:"afterData,omitempty"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type PlatformSettings struct {
	WorkspaceID            string          `json:"-"`
	PlatformName           string          `json:"platformName"`
	PublicBaseURL          string          `json:"publicBaseUrl"`
	RuntimeParameters      json.RawMessage `json:"runtimeParameters"`
	OperationRetentionDays int             `json:"operationRetentionDays"`
	AuditRetentionDays     *int            `json:"auditRetentionDays,omitempty"`
	Version                int64           `json:"version"`
	UpdatedAt              time.Time       `json:"updatedAt"`
}

type MetricsSummary struct {
	Requests          int64   `json:"requests"`
	Successes         int64   `json:"successes"`
	Failures          int64   `json:"failures"`
	SuccessRate       float64 `json:"successRate"`
	AverageMS         float64 `json:"averageMs"`
	P95MS             float64 `json:"p95Ms"`
	PendingJobs       int64   `json:"pendingJobs"`
	ActiveConnections int64   `json:"activeConnections"`
}
