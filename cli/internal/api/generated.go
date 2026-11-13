package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oapi-codegen/runtime"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

const (
	HTTPBearerScopes = "HTTPBearer.Scopes"
)

const (
	Authorized           DeviceTokenState = "authorized"
	AwaitingConfirmation DeviceTokenState = "awaiting_confirmation"
	Expired              DeviceTokenState = "expired"
	Pending              DeviceTokenState = "pending"
)

const (
	Bearer DeviceTokenTokenType = "Bearer"
)

const (
	OriginViewScopeInactive OriginViewScope = "inactive"
	OriginViewScopeLocal    OriginViewScope = "local"
	OriginViewScopeMixed    OriginViewScope = "mixed"
	OriginViewScopeRemote   OriginViewScope = "remote"
)

const (
	RelayAttachRuntimeKindCodex           RelayAttachRuntimeKind = "codex"
	RelayAttachRuntimeKindDeepseekHarness RelayAttachRuntimeKind = "deepseek-harness"
)

const (
	RelayCancellationRuntimeKindCodex           RelayCancellationRuntimeKind = "codex"
	RelayCancellationRuntimeKindDeepseekHarness RelayCancellationRuntimeKind = "deepseek-harness"
)

const (
	Attention  RelayInteractionKind = "attention"
	Permission RelayInteractionKind = "permission"
	Question   RelayInteractionKind = "question"
)

const (
	Cancelled RelayInteractionCloseState = "cancelled"
	Resolved  RelayInteractionCloseState = "resolved"
)

const (
	RelayResultOutcomeCompleted            RelayResultOutcome = "completed"
	RelayResultOutcomeEmptyOutput          RelayResultOutcome = "empty_output"
	RelayResultOutcomeFailed               RelayResultOutcome = "failed"
	RelayResultOutcomeInterruptUnavailable RelayResultOutcome = "interrupt_unavailable"
	RelayResultOutcomeInterrupted          RelayResultOutcome = "interrupted"
	RelayResultOutcomeMixed                RelayResultOutcome = "mixed"
	RelayResultOutcomeNotSubmitted         RelayResultOutcome = "not_submitted"
	RelayResultOutcomeOutputTooLarge       RelayResultOutcome = "output_too_large"
	RelayResultOutcomeUnknown              RelayResultOutcome = "unknown"
)

const (
	Codex           RelayViewRuntimeKind = "codex"
	DeepseekHarness RelayViewRuntimeKind = "deepseek-harness"
)

const (
	None        RetryAction = "none"
	Reconcile   RetryAction = "reconcile"
	SameRequest RetryAction = "same_request"
	UserAction  RetryAction = "user_action"
)

type DeviceClaim struct {
	ClaimSecret *string `json:"claim_secret,omitempty"`
}

type DeviceCreated struct {
	ClaimSecret     string             `json:"claim_secret"`
	DeviceId        openapi_types.UUID `json:"device_id"`
	ExpiresAt       time.Time          `json:"expires_at"`
	PollInterval    *int               `json:"poll_interval,omitempty"`
	Simulated       *bool              `json:"simulated,omitempty"`
	UserCode        string             `json:"user_code"`
	VerificationUri string             `json:"verification_uri"`
}

type DeviceStart struct {
	DeviceName string `json:"device_name"`
}

type DeviceToken struct {
	AccessToken *string               `json:"access_token"`
	ExpiresAt   *time.Time            `json:"expires_at"`
	State       DeviceTokenState      `json:"state"`
	TokenType   *DeviceTokenTokenType `json:"token_type,omitempty"`
}

type DeviceTokenState string

type DeviceTokenTokenType string

type ErrorBody struct {
	Code        string      `json:"code"`
	Message     string      `json:"message"`
	RetryAction RetryAction `json:"retry_action"`
	RetryAfter  *int        `json:"retry_after"`
}

type InteractionView struct {
	Answer *string            `json:"answer"`
	Id     openapi_types.UUID `json:"id"`
	State  string             `json:"state"`
}

type OriginView struct {
	Scope OriginViewScope `json:"scope"`
}

type OriginViewScope string

type RelayAsyncAnswer struct {
	Generation     int                  `json:"generation"`
	Id             openapi_types.UUID   `json:"id"`
	InteractionIds []openapi_types.UUID `json:"interaction_ids"`
}

type RelayAttach struct {
	DeviceName      string                  `json:"device_name"`
	NativeSession   string                  `json:"native_session"`
	Project         string                  `json:"project"`
	RuntimeInstance *string                 `json:"runtime_instance,omitempty"`
	RuntimeKind     *RelayAttachRuntimeKind `json:"runtime_kind,omitempty"`
	SessionName     string                  `json:"session_name"`
}

type RelayAttachRuntimeKind string

type RelayCancellation struct {
	Id              openapi_types.UUID            `json:"id"`
	NativeSession   string                        `json:"native_session"`
	RuntimeInstance *string                       `json:"runtime_instance,omitempty"`
	RuntimeKind     *RelayCancellationRuntimeKind `json:"runtime_kind,omitempty"`
}

type RelayCancellationRuntimeKind string

type RelayGeneration struct {
	Generation int `json:"generation"`
}

type RelayInteraction struct {
	AsyncQuestion *bool                   `json:"async_question,omitempty"`
	Choices       *map[string]interface{} `json:"choices"`
	Generation    int                     `json:"generation"`
	Id            openapi_types.UUID      `json:"id"`
	Kind          RelayInteractionKind    `json:"kind"`
	MessageIds    []openapi_types.UUID    `json:"message_ids"`
	NativeSession string                  `json:"native_session"`
	Prompt        string                  `json:"prompt"`
	TurnId        string                  `json:"turn_id"`
}

type RelayInteractionKind string

type RelayInteractionClose struct {
	Generation int                        `json:"generation"`
	State      RelayInteractionCloseState `json:"state"`
}

type RelayInteractionCloseState string

type RelayMessage struct {
	Content      string             `json:"content"`
	CreatedAt    time.Time          `json:"created_at"`
	DispatchedAt *time.Time         `json:"dispatched_at"`
	Generation   int                `json:"generation"`
	Id           openapi_types.UUID `json:"id"`
	Kind         string             `json:"kind"`
	State        string             `json:"state"`
}

type RelayOrigin struct {
	ForResult     *bool    `json:"for_result,omitempty"`
	Generation    int      `json:"generation"`
	MessageIds    []string `json:"message_ids"`
	NativeSession string   `json:"native_session"`
}

type RelayReadiness struct {
	Available  bool `json:"available"`
	Generation int  `json:"generation"`
}

type RelayResult struct {
	Generation     int                `json:"generation"`
	MessageIds     *[]string          `json:"message_ids,omitempty"`
	Outcome        RelayResultOutcome `json:"outcome"`
	Summary        *string            `json:"summary,omitempty"`
	SummaryPending *bool              `json:"summary_pending,omitempty"`
	Text           *string            `json:"text,omitempty"`
}

type RelayResultOutcome string

type RelaySummary struct {
	ClaimId    *openapi_types.UUID `json:"claim_id"`
	Generation int                 `json:"generation"`
	Summary    string              `json:"summary"`
}

type RelaySummaryClaim struct {
	ClaimId    openapi_types.UUID `json:"claim_id"`
	Generation int                `json:"generation"`
}

type RelaySummaryClaimView struct {
	Job *RelaySummaryJob `json:"job"`
}

type RelaySummaryJob struct {
	Generation int                `json:"generation"`
	MessageId  openapi_types.UUID `json:"message_id"`
	Text       string             `json:"text"`
}

type RelayView struct {
	AsyncInteractions *[]openapi_types.UUID `json:"async_interactions,omitempty"`
	Cancellations     *[]RelayCancellation  `json:"cancellations,omitempty"`
	DeliveryWarning   *bool                 `json:"delivery_warning,omitempty"`
	Desired           bool                  `json:"desired"`
	DeviceName        string                `json:"device_name"`
	Generation        int                   `json:"generation"`
	Guidance          string                `json:"guidance"`
	Messages          *[]RelayMessage       `json:"messages,omitempty"`
	NativeSession     string                `json:"native_session"`
	Online            bool                  `json:"online"`
	Project           string                `json:"project"`
	RuntimeInstance   *string               `json:"runtime_instance,omitempty"`
	RuntimeKind       *RelayViewRuntimeKind `json:"runtime_kind,omitempty"`
	SessionName       string                `json:"session_name"`
	Status            string                `json:"status"`
	UserId            openapi_types.UUID    `json:"user_id"`
}

type RelayViewRuntimeKind string

type RetryAction string

type UserView struct {
	DisplayName   *string            `json:"display_name"`
	FeishuAccount *string            `json:"feishu_account"`
	SessionId     openapi_types.UUID `json:"session_id"`
	UserId        openapi_types.UUID `json:"user_id"`
}

type StatusV1SessionRelayGetParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type AsyncAnswerV1SessionRelayAsyncAnswersPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type AttachV1SessionRelayAttachPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type DetachV1SessionRelayDetachPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type InteractionCreateV1SessionRelayInteractionsPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type InteractionReadV1SessionRelayInteractionsIdentGetParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type InteractionCloseV1SessionRelayInteractionsIdentClosePostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type CancellationV1SessionRelayMessagesMessageIdCancellationGetParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type CancelledV1SessionRelayMessagesMessageIdCancelledPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type DispatchV1SessionRelayMessagesMessageIdDispatchPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type ResultV1SessionRelayMessagesMessageIdResultPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type SummaryV1SessionRelayMessagesMessageIdSummaryPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type WaitingV1SessionRelayMessagesMessageIdWaitingPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type WorkingV1SessionRelayMessagesMessageIdWorkingPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type OriginV1SessionRelayOriginPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type PollV1SessionRelayPollPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type ReadinessV1SessionRelayReadinessPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type SummaryClaimV1SessionRelaySummariesClaimPostParams struct {
	XRemoteAgentSessionRelayRuntime *string `json:"X-Remote-Agent-Session-Relay-Runtime,omitempty"`
}

type StartV1AuthDevicesPostJSONRequestBody = DeviceStart

type ClaimV1AuthDevicesDeviceIdTokenPostJSONRequestBody = DeviceClaim

type AsyncAnswerV1SessionRelayAsyncAnswersPostJSONRequestBody = RelayAsyncAnswer

type AttachV1SessionRelayAttachPostJSONRequestBody = RelayAttach

type DetachV1SessionRelayDetachPostJSONRequestBody = RelayGeneration

type InteractionCreateV1SessionRelayInteractionsPostJSONRequestBody = RelayInteraction

type InteractionCloseV1SessionRelayInteractionsIdentClosePostJSONRequestBody = RelayInteractionClose

type DispatchV1SessionRelayMessagesMessageIdDispatchPostJSONRequestBody = RelayGeneration

type ResultV1SessionRelayMessagesMessageIdResultPostJSONRequestBody = RelayResult

type SummaryV1SessionRelayMessagesMessageIdSummaryPostJSONRequestBody = RelaySummary

type WaitingV1SessionRelayMessagesMessageIdWaitingPostJSONRequestBody = RelayGeneration

type WorkingV1SessionRelayMessagesMessageIdWorkingPostJSONRequestBody = RelayGeneration

type OriginV1SessionRelayOriginPostJSONRequestBody = RelayOrigin

type ReadinessV1SessionRelayReadinessPostJSONRequestBody = RelayReadiness

type SummaryClaimV1SessionRelaySummariesClaimPostJSONRequestBody = RelaySummaryClaim

type RequestEditorFn func(ctx context.Context, req *http.Request) error

type HttpRequestDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	Server string

	Client HttpRequestDoer

	RequestEditors []RequestEditorFn
}

type ClientOption func(*Client) error

func NewClient(server string, opts ...ClientOption) (*Client, error) {

	client := Client{
		Server: server,
	}

	for _, o := range opts {
		if err := o(&client); err != nil {
			return nil, err
		}
	}

	if !strings.HasSuffix(client.Server, "/") {
		client.Server += "/"
	}

	if client.Client == nil {
		client.Client = &http.Client{}
	}
	return &client, nil
}

func WithHTTPClient(doer HttpRequestDoer) ClientOption {
	return func(c *Client) error {
		c.Client = doer
		return nil
	}
}

func WithRequestEditorFn(fn RequestEditorFn) ClientOption {
	return func(c *Client) error {
		c.RequestEditors = append(c.RequestEditors, fn)
		return nil
	}
}

type ClientInterface interface {
	StartV1AuthDevicesPostWithBody(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	StartV1AuthDevicesPost(ctx context.Context, body StartV1AuthDevicesPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	ClaimV1AuthDevicesDeviceIdTokenPostWithBody(ctx context.Context, deviceId openapi_types.UUID, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	ClaimV1AuthDevicesDeviceIdTokenPost(ctx context.Context, deviceId openapi_types.UUID, body ClaimV1AuthDevicesDeviceIdTokenPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	LogoutV1AuthSessionDelete(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error)

	MeV1MeGet(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error)

	StatusV1SessionRelayGet(ctx context.Context, params *StatusV1SessionRelayGetParams, reqEditors ...RequestEditorFn) (*http.Response, error)

	AsyncAnswerV1SessionRelayAsyncAnswersPostWithBody(ctx context.Context, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	AsyncAnswerV1SessionRelayAsyncAnswersPost(ctx context.Context, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, body AsyncAnswerV1SessionRelayAsyncAnswersPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	AttachV1SessionRelayAttachPostWithBody(ctx context.Context, params *AttachV1SessionRelayAttachPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	AttachV1SessionRelayAttachPost(ctx context.Context, params *AttachV1SessionRelayAttachPostParams, body AttachV1SessionRelayAttachPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	DetachV1SessionRelayDetachPostWithBody(ctx context.Context, params *DetachV1SessionRelayDetachPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	DetachV1SessionRelayDetachPost(ctx context.Context, params *DetachV1SessionRelayDetachPostParams, body DetachV1SessionRelayDetachPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	InteractionCreateV1SessionRelayInteractionsPostWithBody(ctx context.Context, params *InteractionCreateV1SessionRelayInteractionsPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	InteractionCreateV1SessionRelayInteractionsPost(ctx context.Context, params *InteractionCreateV1SessionRelayInteractionsPostParams, body InteractionCreateV1SessionRelayInteractionsPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	InteractionReadV1SessionRelayInteractionsIdentGet(ctx context.Context, ident openapi_types.UUID, params *InteractionReadV1SessionRelayInteractionsIdentGetParams, reqEditors ...RequestEditorFn) (*http.Response, error)

	InteractionCloseV1SessionRelayInteractionsIdentClosePostWithBody(ctx context.Context, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	InteractionCloseV1SessionRelayInteractionsIdentClosePost(ctx context.Context, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, body InteractionCloseV1SessionRelayInteractionsIdentClosePostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	CancellationV1SessionRelayMessagesMessageIdCancellationGet(ctx context.Context, messageId openapi_types.UUID, params *CancellationV1SessionRelayMessagesMessageIdCancellationGetParams, reqEditors ...RequestEditorFn) (*http.Response, error)

	CancelledV1SessionRelayMessagesMessageIdCancelledPost(ctx context.Context, messageId openapi_types.UUID, params *CancelledV1SessionRelayMessagesMessageIdCancelledPostParams, reqEditors ...RequestEditorFn) (*http.Response, error)

	DispatchV1SessionRelayMessagesMessageIdDispatchPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	DispatchV1SessionRelayMessagesMessageIdDispatchPost(ctx context.Context, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, body DispatchV1SessionRelayMessagesMessageIdDispatchPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	ResultV1SessionRelayMessagesMessageIdResultPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	ResultV1SessionRelayMessagesMessageIdResultPost(ctx context.Context, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, body ResultV1SessionRelayMessagesMessageIdResultPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	SummaryV1SessionRelayMessagesMessageIdSummaryPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	SummaryV1SessionRelayMessagesMessageIdSummaryPost(ctx context.Context, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, body SummaryV1SessionRelayMessagesMessageIdSummaryPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	WaitingV1SessionRelayMessagesMessageIdWaitingPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	WaitingV1SessionRelayMessagesMessageIdWaitingPost(ctx context.Context, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, body WaitingV1SessionRelayMessagesMessageIdWaitingPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	WorkingV1SessionRelayMessagesMessageIdWorkingPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	WorkingV1SessionRelayMessagesMessageIdWorkingPost(ctx context.Context, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, body WorkingV1SessionRelayMessagesMessageIdWorkingPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	OriginV1SessionRelayOriginPostWithBody(ctx context.Context, params *OriginV1SessionRelayOriginPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	OriginV1SessionRelayOriginPost(ctx context.Context, params *OriginV1SessionRelayOriginPostParams, body OriginV1SessionRelayOriginPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	PollV1SessionRelayPollPost(ctx context.Context, params *PollV1SessionRelayPollPostParams, reqEditors ...RequestEditorFn) (*http.Response, error)

	ReadinessV1SessionRelayReadinessPostWithBody(ctx context.Context, params *ReadinessV1SessionRelayReadinessPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	ReadinessV1SessionRelayReadinessPost(ctx context.Context, params *ReadinessV1SessionRelayReadinessPostParams, body ReadinessV1SessionRelayReadinessPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)

	SummaryClaimV1SessionRelaySummariesClaimPostWithBody(ctx context.Context, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error)

	SummaryClaimV1SessionRelaySummariesClaimPost(ctx context.Context, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, body SummaryClaimV1SessionRelaySummariesClaimPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error)
}

func (c *Client) StartV1AuthDevicesPostWithBody(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewStartV1AuthDevicesPostRequestWithBody(c.Server, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) StartV1AuthDevicesPost(ctx context.Context, body StartV1AuthDevicesPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewStartV1AuthDevicesPostRequest(c.Server, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) ClaimV1AuthDevicesDeviceIdTokenPostWithBody(ctx context.Context, deviceId openapi_types.UUID, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewClaimV1AuthDevicesDeviceIdTokenPostRequestWithBody(c.Server, deviceId, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) ClaimV1AuthDevicesDeviceIdTokenPost(ctx context.Context, deviceId openapi_types.UUID, body ClaimV1AuthDevicesDeviceIdTokenPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewClaimV1AuthDevicesDeviceIdTokenPostRequest(c.Server, deviceId, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) LogoutV1AuthSessionDelete(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewLogoutV1AuthSessionDeleteRequest(c.Server)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) MeV1MeGet(ctx context.Context, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewMeV1MeGetRequest(c.Server)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) StatusV1SessionRelayGet(ctx context.Context, params *StatusV1SessionRelayGetParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewStatusV1SessionRelayGetRequest(c.Server, params)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) AsyncAnswerV1SessionRelayAsyncAnswersPostWithBody(ctx context.Context, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewAsyncAnswerV1SessionRelayAsyncAnswersPostRequestWithBody(c.Server, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) AsyncAnswerV1SessionRelayAsyncAnswersPost(ctx context.Context, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, body AsyncAnswerV1SessionRelayAsyncAnswersPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewAsyncAnswerV1SessionRelayAsyncAnswersPostRequest(c.Server, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) AttachV1SessionRelayAttachPostWithBody(ctx context.Context, params *AttachV1SessionRelayAttachPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewAttachV1SessionRelayAttachPostRequestWithBody(c.Server, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) AttachV1SessionRelayAttachPost(ctx context.Context, params *AttachV1SessionRelayAttachPostParams, body AttachV1SessionRelayAttachPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewAttachV1SessionRelayAttachPostRequest(c.Server, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) DetachV1SessionRelayDetachPostWithBody(ctx context.Context, params *DetachV1SessionRelayDetachPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewDetachV1SessionRelayDetachPostRequestWithBody(c.Server, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) DetachV1SessionRelayDetachPost(ctx context.Context, params *DetachV1SessionRelayDetachPostParams, body DetachV1SessionRelayDetachPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewDetachV1SessionRelayDetachPostRequest(c.Server, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) InteractionCreateV1SessionRelayInteractionsPostWithBody(ctx context.Context, params *InteractionCreateV1SessionRelayInteractionsPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewInteractionCreateV1SessionRelayInteractionsPostRequestWithBody(c.Server, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) InteractionCreateV1SessionRelayInteractionsPost(ctx context.Context, params *InteractionCreateV1SessionRelayInteractionsPostParams, body InteractionCreateV1SessionRelayInteractionsPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewInteractionCreateV1SessionRelayInteractionsPostRequest(c.Server, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) InteractionReadV1SessionRelayInteractionsIdentGet(ctx context.Context, ident openapi_types.UUID, params *InteractionReadV1SessionRelayInteractionsIdentGetParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewInteractionReadV1SessionRelayInteractionsIdentGetRequest(c.Server, ident, params)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) InteractionCloseV1SessionRelayInteractionsIdentClosePostWithBody(ctx context.Context, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewInteractionCloseV1SessionRelayInteractionsIdentClosePostRequestWithBody(c.Server, ident, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) InteractionCloseV1SessionRelayInteractionsIdentClosePost(ctx context.Context, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, body InteractionCloseV1SessionRelayInteractionsIdentClosePostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewInteractionCloseV1SessionRelayInteractionsIdentClosePostRequest(c.Server, ident, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) CancellationV1SessionRelayMessagesMessageIdCancellationGet(ctx context.Context, messageId openapi_types.UUID, params *CancellationV1SessionRelayMessagesMessageIdCancellationGetParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewCancellationV1SessionRelayMessagesMessageIdCancellationGetRequest(c.Server, messageId, params)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) CancelledV1SessionRelayMessagesMessageIdCancelledPost(ctx context.Context, messageId openapi_types.UUID, params *CancelledV1SessionRelayMessagesMessageIdCancelledPostParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewCancelledV1SessionRelayMessagesMessageIdCancelledPostRequest(c.Server, messageId, params)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) DispatchV1SessionRelayMessagesMessageIdDispatchPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewDispatchV1SessionRelayMessagesMessageIdDispatchPostRequestWithBody(c.Server, messageId, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) DispatchV1SessionRelayMessagesMessageIdDispatchPost(ctx context.Context, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, body DispatchV1SessionRelayMessagesMessageIdDispatchPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewDispatchV1SessionRelayMessagesMessageIdDispatchPostRequest(c.Server, messageId, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) ResultV1SessionRelayMessagesMessageIdResultPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewResultV1SessionRelayMessagesMessageIdResultPostRequestWithBody(c.Server, messageId, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) ResultV1SessionRelayMessagesMessageIdResultPost(ctx context.Context, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, body ResultV1SessionRelayMessagesMessageIdResultPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewResultV1SessionRelayMessagesMessageIdResultPostRequest(c.Server, messageId, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) SummaryV1SessionRelayMessagesMessageIdSummaryPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewSummaryV1SessionRelayMessagesMessageIdSummaryPostRequestWithBody(c.Server, messageId, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) SummaryV1SessionRelayMessagesMessageIdSummaryPost(ctx context.Context, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, body SummaryV1SessionRelayMessagesMessageIdSummaryPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewSummaryV1SessionRelayMessagesMessageIdSummaryPostRequest(c.Server, messageId, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) WaitingV1SessionRelayMessagesMessageIdWaitingPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewWaitingV1SessionRelayMessagesMessageIdWaitingPostRequestWithBody(c.Server, messageId, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) WaitingV1SessionRelayMessagesMessageIdWaitingPost(ctx context.Context, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, body WaitingV1SessionRelayMessagesMessageIdWaitingPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewWaitingV1SessionRelayMessagesMessageIdWaitingPostRequest(c.Server, messageId, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) WorkingV1SessionRelayMessagesMessageIdWorkingPostWithBody(ctx context.Context, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewWorkingV1SessionRelayMessagesMessageIdWorkingPostRequestWithBody(c.Server, messageId, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) WorkingV1SessionRelayMessagesMessageIdWorkingPost(ctx context.Context, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, body WorkingV1SessionRelayMessagesMessageIdWorkingPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewWorkingV1SessionRelayMessagesMessageIdWorkingPostRequest(c.Server, messageId, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) OriginV1SessionRelayOriginPostWithBody(ctx context.Context, params *OriginV1SessionRelayOriginPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewOriginV1SessionRelayOriginPostRequestWithBody(c.Server, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) OriginV1SessionRelayOriginPost(ctx context.Context, params *OriginV1SessionRelayOriginPostParams, body OriginV1SessionRelayOriginPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewOriginV1SessionRelayOriginPostRequest(c.Server, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) PollV1SessionRelayPollPost(ctx context.Context, params *PollV1SessionRelayPollPostParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewPollV1SessionRelayPollPostRequest(c.Server, params)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) ReadinessV1SessionRelayReadinessPostWithBody(ctx context.Context, params *ReadinessV1SessionRelayReadinessPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewReadinessV1SessionRelayReadinessPostRequestWithBody(c.Server, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) ReadinessV1SessionRelayReadinessPost(ctx context.Context, params *ReadinessV1SessionRelayReadinessPostParams, body ReadinessV1SessionRelayReadinessPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewReadinessV1SessionRelayReadinessPostRequest(c.Server, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) SummaryClaimV1SessionRelaySummariesClaimPostWithBody(ctx context.Context, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewSummaryClaimV1SessionRelaySummariesClaimPostRequestWithBody(c.Server, params, contentType, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) SummaryClaimV1SessionRelaySummariesClaimPost(ctx context.Context, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, body SummaryClaimV1SessionRelaySummariesClaimPostJSONRequestBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewSummaryClaimV1SessionRelaySummariesClaimPostRequest(c.Server, params, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func NewStartV1AuthDevicesPostRequest(server string, body StartV1AuthDevicesPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewStartV1AuthDevicesPostRequestWithBody(server, "application/json", bodyReader)
}

func NewStartV1AuthDevicesPostRequestWithBody(server string, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/auth/devices")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	return req, nil
}

func NewClaimV1AuthDevicesDeviceIdTokenPostRequest(server string, deviceId openapi_types.UUID, body ClaimV1AuthDevicesDeviceIdTokenPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewClaimV1AuthDevicesDeviceIdTokenPostRequestWithBody(server, deviceId, "application/json", bodyReader)
}

func NewClaimV1AuthDevicesDeviceIdTokenPostRequestWithBody(server string, deviceId openapi_types.UUID, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "device_id", runtime.ParamLocationPath, deviceId)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/auth/devices/%s/token", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	return req, nil
}

func NewLogoutV1AuthSessionDeleteRequest(server string) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/auth/session")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("DELETE", queryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	return req, nil
}

func NewMeV1MeGetRequest(server string) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/me")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", queryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	return req, nil
}

func NewStatusV1SessionRelayGetRequest(server string, params *StatusV1SessionRelayGetParams) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", queryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewAsyncAnswerV1SessionRelayAsyncAnswersPostRequest(server string, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, body AsyncAnswerV1SessionRelayAsyncAnswersPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewAsyncAnswerV1SessionRelayAsyncAnswersPostRequestWithBody(server, params, "application/json", bodyReader)
}

func NewAsyncAnswerV1SessionRelayAsyncAnswersPostRequestWithBody(server string, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/async-answers")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewAttachV1SessionRelayAttachPostRequest(server string, params *AttachV1SessionRelayAttachPostParams, body AttachV1SessionRelayAttachPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewAttachV1SessionRelayAttachPostRequestWithBody(server, params, "application/json", bodyReader)
}

func NewAttachV1SessionRelayAttachPostRequestWithBody(server string, params *AttachV1SessionRelayAttachPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/attach")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewDetachV1SessionRelayDetachPostRequest(server string, params *DetachV1SessionRelayDetachPostParams, body DetachV1SessionRelayDetachPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewDetachV1SessionRelayDetachPostRequestWithBody(server, params, "application/json", bodyReader)
}

func NewDetachV1SessionRelayDetachPostRequestWithBody(server string, params *DetachV1SessionRelayDetachPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/detach")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewInteractionCreateV1SessionRelayInteractionsPostRequest(server string, params *InteractionCreateV1SessionRelayInteractionsPostParams, body InteractionCreateV1SessionRelayInteractionsPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewInteractionCreateV1SessionRelayInteractionsPostRequestWithBody(server, params, "application/json", bodyReader)
}

func NewInteractionCreateV1SessionRelayInteractionsPostRequestWithBody(server string, params *InteractionCreateV1SessionRelayInteractionsPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/interactions")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewInteractionReadV1SessionRelayInteractionsIdentGetRequest(server string, ident openapi_types.UUID, params *InteractionReadV1SessionRelayInteractionsIdentGetParams) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "ident", runtime.ParamLocationPath, ident)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/interactions/%s", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", queryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewInteractionCloseV1SessionRelayInteractionsIdentClosePostRequest(server string, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, body InteractionCloseV1SessionRelayInteractionsIdentClosePostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewInteractionCloseV1SessionRelayInteractionsIdentClosePostRequestWithBody(server, ident, params, "application/json", bodyReader)
}

func NewInteractionCloseV1SessionRelayInteractionsIdentClosePostRequestWithBody(server string, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "ident", runtime.ParamLocationPath, ident)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/interactions/%s/close", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewCancellationV1SessionRelayMessagesMessageIdCancellationGetRequest(server string, messageId openapi_types.UUID, params *CancellationV1SessionRelayMessagesMessageIdCancellationGetParams) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "message_id", runtime.ParamLocationPath, messageId)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/messages/%s/cancellation", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", queryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewCancelledV1SessionRelayMessagesMessageIdCancelledPostRequest(server string, messageId openapi_types.UUID, params *CancelledV1SessionRelayMessagesMessageIdCancelledPostParams) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "message_id", runtime.ParamLocationPath, messageId)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/messages/%s/cancelled", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewDispatchV1SessionRelayMessagesMessageIdDispatchPostRequest(server string, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, body DispatchV1SessionRelayMessagesMessageIdDispatchPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewDispatchV1SessionRelayMessagesMessageIdDispatchPostRequestWithBody(server, messageId, params, "application/json", bodyReader)
}

func NewDispatchV1SessionRelayMessagesMessageIdDispatchPostRequestWithBody(server string, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "message_id", runtime.ParamLocationPath, messageId)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/messages/%s/dispatch", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewResultV1SessionRelayMessagesMessageIdResultPostRequest(server string, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, body ResultV1SessionRelayMessagesMessageIdResultPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewResultV1SessionRelayMessagesMessageIdResultPostRequestWithBody(server, messageId, params, "application/json", bodyReader)
}

func NewResultV1SessionRelayMessagesMessageIdResultPostRequestWithBody(server string, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "message_id", runtime.ParamLocationPath, messageId)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/messages/%s/result", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewSummaryV1SessionRelayMessagesMessageIdSummaryPostRequest(server string, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, body SummaryV1SessionRelayMessagesMessageIdSummaryPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewSummaryV1SessionRelayMessagesMessageIdSummaryPostRequestWithBody(server, messageId, params, "application/json", bodyReader)
}

func NewSummaryV1SessionRelayMessagesMessageIdSummaryPostRequestWithBody(server string, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "message_id", runtime.ParamLocationPath, messageId)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/messages/%s/summary", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewWaitingV1SessionRelayMessagesMessageIdWaitingPostRequest(server string, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, body WaitingV1SessionRelayMessagesMessageIdWaitingPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewWaitingV1SessionRelayMessagesMessageIdWaitingPostRequestWithBody(server, messageId, params, "application/json", bodyReader)
}

func NewWaitingV1SessionRelayMessagesMessageIdWaitingPostRequestWithBody(server string, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "message_id", runtime.ParamLocationPath, messageId)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/messages/%s/waiting", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewWorkingV1SessionRelayMessagesMessageIdWorkingPostRequest(server string, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, body WorkingV1SessionRelayMessagesMessageIdWorkingPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewWorkingV1SessionRelayMessagesMessageIdWorkingPostRequestWithBody(server, messageId, params, "application/json", bodyReader)
}

func NewWorkingV1SessionRelayMessagesMessageIdWorkingPostRequestWithBody(server string, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	var pathParam0 string

	pathParam0, err = runtime.StyleParamWithLocation("simple", false, "message_id", runtime.ParamLocationPath, messageId)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/messages/%s/working", pathParam0)
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewOriginV1SessionRelayOriginPostRequest(server string, params *OriginV1SessionRelayOriginPostParams, body OriginV1SessionRelayOriginPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewOriginV1SessionRelayOriginPostRequestWithBody(server, params, "application/json", bodyReader)
}

func NewOriginV1SessionRelayOriginPostRequestWithBody(server string, params *OriginV1SessionRelayOriginPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/origin")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewPollV1SessionRelayPollPostRequest(server string, params *PollV1SessionRelayPollPostParams) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/poll")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewReadinessV1SessionRelayReadinessPostRequest(server string, params *ReadinessV1SessionRelayReadinessPostParams, body ReadinessV1SessionRelayReadinessPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewReadinessV1SessionRelayReadinessPostRequestWithBody(server, params, "application/json", bodyReader)
}

func NewReadinessV1SessionRelayReadinessPostRequestWithBody(server string, params *ReadinessV1SessionRelayReadinessPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/readiness")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func NewSummaryClaimV1SessionRelaySummariesClaimPostRequest(server string, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, body SummaryClaimV1SessionRelaySummariesClaimPostJSONRequestBody) (*http.Request, error) {
	var bodyReader io.Reader
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyReader = bytes.NewReader(buf)
	return NewSummaryClaimV1SessionRelaySummariesClaimPostRequestWithBody(server, params, "application/json", bodyReader)
}

func NewSummaryClaimV1SessionRelaySummariesClaimPostRequestWithBody(server string, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, contentType string, body io.Reader) (*http.Request, error) {
	var err error

	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	operationPath := fmt.Sprintf("/v1/session-relay/summaries/claim")
	if operationPath[0] == '/' {
		operationPath = "." + operationPath
	}

	queryURL, err := serverURL.Parse(operationPath)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", queryURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", contentType)

	if params != nil {

		if params.XRemoteAgentSessionRelayRuntime != nil {
			var headerParam0 string

			headerParam0, err = runtime.StyleParamWithLocation("simple", false, "X-Remote-Agent-Session-Relay-Runtime", runtime.ParamLocationHeader, *params.XRemoteAgentSessionRelayRuntime)
			if err != nil {
				return nil, err
			}

			req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", headerParam0)
		}

	}

	return req, nil
}

func (c *Client) applyEditors(ctx context.Context, req *http.Request, additionalEditors []RequestEditorFn) error {
	for _, r := range c.RequestEditors {
		if err := r(ctx, req); err != nil {
			return err
		}
	}
	for _, r := range additionalEditors {
		if err := r(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

type ClientWithResponses struct {
	ClientInterface
}

func NewClientWithResponses(server string, opts ...ClientOption) (*ClientWithResponses, error) {
	client, err := NewClient(server, opts...)
	if err != nil {
		return nil, err
	}
	return &ClientWithResponses{client}, nil
}

func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) error {
		newBaseURL, err := url.Parse(baseURL)
		if err != nil {
			return err
		}
		c.Server = newBaseURL.String()
		return nil
	}
}

type ClientWithResponsesInterface interface {
	StartV1AuthDevicesPostWithBodyWithResponse(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*StartV1AuthDevicesPostResponse, error)

	StartV1AuthDevicesPostWithResponse(ctx context.Context, body StartV1AuthDevicesPostJSONRequestBody, reqEditors ...RequestEditorFn) (*StartV1AuthDevicesPostResponse, error)

	ClaimV1AuthDevicesDeviceIdTokenPostWithBodyWithResponse(ctx context.Context, deviceId openapi_types.UUID, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*ClaimV1AuthDevicesDeviceIdTokenPostResponse, error)

	ClaimV1AuthDevicesDeviceIdTokenPostWithResponse(ctx context.Context, deviceId openapi_types.UUID, body ClaimV1AuthDevicesDeviceIdTokenPostJSONRequestBody, reqEditors ...RequestEditorFn) (*ClaimV1AuthDevicesDeviceIdTokenPostResponse, error)

	LogoutV1AuthSessionDeleteWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*LogoutV1AuthSessionDeleteResponse, error)

	MeV1MeGetWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*MeV1MeGetResponse, error)

	StatusV1SessionRelayGetWithResponse(ctx context.Context, params *StatusV1SessionRelayGetParams, reqEditors ...RequestEditorFn) (*StatusV1SessionRelayGetResponse, error)

	AsyncAnswerV1SessionRelayAsyncAnswersPostWithBodyWithResponse(ctx context.Context, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*AsyncAnswerV1SessionRelayAsyncAnswersPostResponse, error)

	AsyncAnswerV1SessionRelayAsyncAnswersPostWithResponse(ctx context.Context, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, body AsyncAnswerV1SessionRelayAsyncAnswersPostJSONRequestBody, reqEditors ...RequestEditorFn) (*AsyncAnswerV1SessionRelayAsyncAnswersPostResponse, error)

	AttachV1SessionRelayAttachPostWithBodyWithResponse(ctx context.Context, params *AttachV1SessionRelayAttachPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*AttachV1SessionRelayAttachPostResponse, error)

	AttachV1SessionRelayAttachPostWithResponse(ctx context.Context, params *AttachV1SessionRelayAttachPostParams, body AttachV1SessionRelayAttachPostJSONRequestBody, reqEditors ...RequestEditorFn) (*AttachV1SessionRelayAttachPostResponse, error)

	DetachV1SessionRelayDetachPostWithBodyWithResponse(ctx context.Context, params *DetachV1SessionRelayDetachPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*DetachV1SessionRelayDetachPostResponse, error)

	DetachV1SessionRelayDetachPostWithResponse(ctx context.Context, params *DetachV1SessionRelayDetachPostParams, body DetachV1SessionRelayDetachPostJSONRequestBody, reqEditors ...RequestEditorFn) (*DetachV1SessionRelayDetachPostResponse, error)

	InteractionCreateV1SessionRelayInteractionsPostWithBodyWithResponse(ctx context.Context, params *InteractionCreateV1SessionRelayInteractionsPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*InteractionCreateV1SessionRelayInteractionsPostResponse, error)

	InteractionCreateV1SessionRelayInteractionsPostWithResponse(ctx context.Context, params *InteractionCreateV1SessionRelayInteractionsPostParams, body InteractionCreateV1SessionRelayInteractionsPostJSONRequestBody, reqEditors ...RequestEditorFn) (*InteractionCreateV1SessionRelayInteractionsPostResponse, error)

	InteractionReadV1SessionRelayInteractionsIdentGetWithResponse(ctx context.Context, ident openapi_types.UUID, params *InteractionReadV1SessionRelayInteractionsIdentGetParams, reqEditors ...RequestEditorFn) (*InteractionReadV1SessionRelayInteractionsIdentGetResponse, error)

	InteractionCloseV1SessionRelayInteractionsIdentClosePostWithBodyWithResponse(ctx context.Context, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*InteractionCloseV1SessionRelayInteractionsIdentClosePostResponse, error)

	InteractionCloseV1SessionRelayInteractionsIdentClosePostWithResponse(ctx context.Context, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, body InteractionCloseV1SessionRelayInteractionsIdentClosePostJSONRequestBody, reqEditors ...RequestEditorFn) (*InteractionCloseV1SessionRelayInteractionsIdentClosePostResponse, error)

	CancellationV1SessionRelayMessagesMessageIdCancellationGetWithResponse(ctx context.Context, messageId openapi_types.UUID, params *CancellationV1SessionRelayMessagesMessageIdCancellationGetParams, reqEditors ...RequestEditorFn) (*CancellationV1SessionRelayMessagesMessageIdCancellationGetResponse, error)

	CancelledV1SessionRelayMessagesMessageIdCancelledPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *CancelledV1SessionRelayMessagesMessageIdCancelledPostParams, reqEditors ...RequestEditorFn) (*CancelledV1SessionRelayMessagesMessageIdCancelledPostResponse, error)

	DispatchV1SessionRelayMessagesMessageIdDispatchPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*DispatchV1SessionRelayMessagesMessageIdDispatchPostResponse, error)

	DispatchV1SessionRelayMessagesMessageIdDispatchPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, body DispatchV1SessionRelayMessagesMessageIdDispatchPostJSONRequestBody, reqEditors ...RequestEditorFn) (*DispatchV1SessionRelayMessagesMessageIdDispatchPostResponse, error)

	ResultV1SessionRelayMessagesMessageIdResultPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*ResultV1SessionRelayMessagesMessageIdResultPostResponse, error)

	ResultV1SessionRelayMessagesMessageIdResultPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, body ResultV1SessionRelayMessagesMessageIdResultPostJSONRequestBody, reqEditors ...RequestEditorFn) (*ResultV1SessionRelayMessagesMessageIdResultPostResponse, error)

	SummaryV1SessionRelayMessagesMessageIdSummaryPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*SummaryV1SessionRelayMessagesMessageIdSummaryPostResponse, error)

	SummaryV1SessionRelayMessagesMessageIdSummaryPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, body SummaryV1SessionRelayMessagesMessageIdSummaryPostJSONRequestBody, reqEditors ...RequestEditorFn) (*SummaryV1SessionRelayMessagesMessageIdSummaryPostResponse, error)

	WaitingV1SessionRelayMessagesMessageIdWaitingPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*WaitingV1SessionRelayMessagesMessageIdWaitingPostResponse, error)

	WaitingV1SessionRelayMessagesMessageIdWaitingPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, body WaitingV1SessionRelayMessagesMessageIdWaitingPostJSONRequestBody, reqEditors ...RequestEditorFn) (*WaitingV1SessionRelayMessagesMessageIdWaitingPostResponse, error)

	WorkingV1SessionRelayMessagesMessageIdWorkingPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*WorkingV1SessionRelayMessagesMessageIdWorkingPostResponse, error)

	WorkingV1SessionRelayMessagesMessageIdWorkingPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, body WorkingV1SessionRelayMessagesMessageIdWorkingPostJSONRequestBody, reqEditors ...RequestEditorFn) (*WorkingV1SessionRelayMessagesMessageIdWorkingPostResponse, error)

	OriginV1SessionRelayOriginPostWithBodyWithResponse(ctx context.Context, params *OriginV1SessionRelayOriginPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*OriginV1SessionRelayOriginPostResponse, error)

	OriginV1SessionRelayOriginPostWithResponse(ctx context.Context, params *OriginV1SessionRelayOriginPostParams, body OriginV1SessionRelayOriginPostJSONRequestBody, reqEditors ...RequestEditorFn) (*OriginV1SessionRelayOriginPostResponse, error)

	PollV1SessionRelayPollPostWithResponse(ctx context.Context, params *PollV1SessionRelayPollPostParams, reqEditors ...RequestEditorFn) (*PollV1SessionRelayPollPostResponse, error)

	ReadinessV1SessionRelayReadinessPostWithBodyWithResponse(ctx context.Context, params *ReadinessV1SessionRelayReadinessPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*ReadinessV1SessionRelayReadinessPostResponse, error)

	ReadinessV1SessionRelayReadinessPostWithResponse(ctx context.Context, params *ReadinessV1SessionRelayReadinessPostParams, body ReadinessV1SessionRelayReadinessPostJSONRequestBody, reqEditors ...RequestEditorFn) (*ReadinessV1SessionRelayReadinessPostResponse, error)

	SummaryClaimV1SessionRelaySummariesClaimPostWithBodyWithResponse(ctx context.Context, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*SummaryClaimV1SessionRelaySummariesClaimPostResponse, error)

	SummaryClaimV1SessionRelaySummariesClaimPostWithResponse(ctx context.Context, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, body SummaryClaimV1SessionRelaySummariesClaimPostJSONRequestBody, reqEditors ...RequestEditorFn) (*SummaryClaimV1SessionRelaySummariesClaimPostResponse, error)
}

type StartV1AuthDevicesPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON201      *DeviceCreated
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r StartV1AuthDevicesPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r StartV1AuthDevicesPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type ClaimV1AuthDevicesDeviceIdTokenPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *DeviceToken
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r ClaimV1AuthDevicesDeviceIdTokenPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r ClaimV1AuthDevicesDeviceIdTokenPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type LogoutV1AuthSessionDeleteResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r LogoutV1AuthSessionDeleteResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r LogoutV1AuthSessionDeleteResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type MeV1MeGetResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *UserView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r MeV1MeGetResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r MeV1MeGetResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type StatusV1SessionRelayGetResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *RelayView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r StatusV1SessionRelayGetResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r StatusV1SessionRelayGetResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type AsyncAnswerV1SessionRelayAsyncAnswersPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *map[string]bool
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r AsyncAnswerV1SessionRelayAsyncAnswersPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r AsyncAnswerV1SessionRelayAsyncAnswersPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type AttachV1SessionRelayAttachPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *RelayView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r AttachV1SessionRelayAttachPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r AttachV1SessionRelayAttachPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type DetachV1SessionRelayDetachPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *RelayView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r DetachV1SessionRelayDetachPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r DetachV1SessionRelayDetachPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type InteractionCreateV1SessionRelayInteractionsPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *InteractionView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r InteractionCreateV1SessionRelayInteractionsPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r InteractionCreateV1SessionRelayInteractionsPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type InteractionReadV1SessionRelayInteractionsIdentGetResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *InteractionView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r InteractionReadV1SessionRelayInteractionsIdentGetResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r InteractionReadV1SessionRelayInteractionsIdentGetResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type InteractionCloseV1SessionRelayInteractionsIdentClosePostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *InteractionView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r InteractionCloseV1SessionRelayInteractionsIdentClosePostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r InteractionCloseV1SessionRelayInteractionsIdentClosePostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type CancellationV1SessionRelayMessagesMessageIdCancellationGetResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *map[string]bool
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r CancellationV1SessionRelayMessagesMessageIdCancellationGetResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r CancellationV1SessionRelayMessagesMessageIdCancellationGetResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type CancelledV1SessionRelayMessagesMessageIdCancelledPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *map[string]bool
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r CancelledV1SessionRelayMessagesMessageIdCancelledPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r CancelledV1SessionRelayMessagesMessageIdCancelledPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type DispatchV1SessionRelayMessagesMessageIdDispatchPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *map[string]bool
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r DispatchV1SessionRelayMessagesMessageIdDispatchPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r DispatchV1SessionRelayMessagesMessageIdDispatchPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type ResultV1SessionRelayMessagesMessageIdResultPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *map[string]bool
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r ResultV1SessionRelayMessagesMessageIdResultPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r ResultV1SessionRelayMessagesMessageIdResultPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type SummaryV1SessionRelayMessagesMessageIdSummaryPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *map[string]bool
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r SummaryV1SessionRelayMessagesMessageIdSummaryPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r SummaryV1SessionRelayMessagesMessageIdSummaryPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type WaitingV1SessionRelayMessagesMessageIdWaitingPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *map[string]bool
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r WaitingV1SessionRelayMessagesMessageIdWaitingPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r WaitingV1SessionRelayMessagesMessageIdWaitingPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type WorkingV1SessionRelayMessagesMessageIdWorkingPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *map[string]bool
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r WorkingV1SessionRelayMessagesMessageIdWorkingPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r WorkingV1SessionRelayMessagesMessageIdWorkingPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type OriginV1SessionRelayOriginPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *OriginView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r OriginV1SessionRelayOriginPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r OriginV1SessionRelayOriginPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type PollV1SessionRelayPollPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *RelayView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r PollV1SessionRelayPollPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r PollV1SessionRelayPollPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type ReadinessV1SessionRelayReadinessPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *RelayView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r ReadinessV1SessionRelayReadinessPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r ReadinessV1SessionRelayReadinessPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

type SummaryClaimV1SessionRelaySummariesClaimPostResponse struct {
	Body         []byte
	HTTPResponse *http.Response
	JSON200      *RelaySummaryClaimView
	JSON401      *ErrorBody
	JSON403      *ErrorBody
	JSON404      *ErrorBody
	JSON409      *ErrorBody
	JSON422      *ErrorBody
	JSON429      *ErrorBody
	JSON503      *ErrorBody
	JSON504      *ErrorBody
}

func (r SummaryClaimV1SessionRelaySummariesClaimPostResponse) Status() string {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.Status
	}
	return http.StatusText(0)
}

func (r SummaryClaimV1SessionRelaySummariesClaimPostResponse) StatusCode() int {
	if r.HTTPResponse != nil {
		return r.HTTPResponse.StatusCode
	}
	return 0
}

func (c *ClientWithResponses) StartV1AuthDevicesPostWithBodyWithResponse(ctx context.Context, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*StartV1AuthDevicesPostResponse, error) {
	rsp, err := c.StartV1AuthDevicesPostWithBody(ctx, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseStartV1AuthDevicesPostResponse(rsp)
}

func (c *ClientWithResponses) StartV1AuthDevicesPostWithResponse(ctx context.Context, body StartV1AuthDevicesPostJSONRequestBody, reqEditors ...RequestEditorFn) (*StartV1AuthDevicesPostResponse, error) {
	rsp, err := c.StartV1AuthDevicesPost(ctx, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseStartV1AuthDevicesPostResponse(rsp)
}

func (c *ClientWithResponses) ClaimV1AuthDevicesDeviceIdTokenPostWithBodyWithResponse(ctx context.Context, deviceId openapi_types.UUID, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*ClaimV1AuthDevicesDeviceIdTokenPostResponse, error) {
	rsp, err := c.ClaimV1AuthDevicesDeviceIdTokenPostWithBody(ctx, deviceId, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseClaimV1AuthDevicesDeviceIdTokenPostResponse(rsp)
}

func (c *ClientWithResponses) ClaimV1AuthDevicesDeviceIdTokenPostWithResponse(ctx context.Context, deviceId openapi_types.UUID, body ClaimV1AuthDevicesDeviceIdTokenPostJSONRequestBody, reqEditors ...RequestEditorFn) (*ClaimV1AuthDevicesDeviceIdTokenPostResponse, error) {
	rsp, err := c.ClaimV1AuthDevicesDeviceIdTokenPost(ctx, deviceId, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseClaimV1AuthDevicesDeviceIdTokenPostResponse(rsp)
}

func (c *ClientWithResponses) LogoutV1AuthSessionDeleteWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*LogoutV1AuthSessionDeleteResponse, error) {
	rsp, err := c.LogoutV1AuthSessionDelete(ctx, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseLogoutV1AuthSessionDeleteResponse(rsp)
}

func (c *ClientWithResponses) MeV1MeGetWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*MeV1MeGetResponse, error) {
	rsp, err := c.MeV1MeGet(ctx, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseMeV1MeGetResponse(rsp)
}

func (c *ClientWithResponses) StatusV1SessionRelayGetWithResponse(ctx context.Context, params *StatusV1SessionRelayGetParams, reqEditors ...RequestEditorFn) (*StatusV1SessionRelayGetResponse, error) {
	rsp, err := c.StatusV1SessionRelayGet(ctx, params, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseStatusV1SessionRelayGetResponse(rsp)
}

func (c *ClientWithResponses) AsyncAnswerV1SessionRelayAsyncAnswersPostWithBodyWithResponse(ctx context.Context, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*AsyncAnswerV1SessionRelayAsyncAnswersPostResponse, error) {
	rsp, err := c.AsyncAnswerV1SessionRelayAsyncAnswersPostWithBody(ctx, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseAsyncAnswerV1SessionRelayAsyncAnswersPostResponse(rsp)
}

func (c *ClientWithResponses) AsyncAnswerV1SessionRelayAsyncAnswersPostWithResponse(ctx context.Context, params *AsyncAnswerV1SessionRelayAsyncAnswersPostParams, body AsyncAnswerV1SessionRelayAsyncAnswersPostJSONRequestBody, reqEditors ...RequestEditorFn) (*AsyncAnswerV1SessionRelayAsyncAnswersPostResponse, error) {
	rsp, err := c.AsyncAnswerV1SessionRelayAsyncAnswersPost(ctx, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseAsyncAnswerV1SessionRelayAsyncAnswersPostResponse(rsp)
}

func (c *ClientWithResponses) AttachV1SessionRelayAttachPostWithBodyWithResponse(ctx context.Context, params *AttachV1SessionRelayAttachPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*AttachV1SessionRelayAttachPostResponse, error) {
	rsp, err := c.AttachV1SessionRelayAttachPostWithBody(ctx, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseAttachV1SessionRelayAttachPostResponse(rsp)
}

func (c *ClientWithResponses) AttachV1SessionRelayAttachPostWithResponse(ctx context.Context, params *AttachV1SessionRelayAttachPostParams, body AttachV1SessionRelayAttachPostJSONRequestBody, reqEditors ...RequestEditorFn) (*AttachV1SessionRelayAttachPostResponse, error) {
	rsp, err := c.AttachV1SessionRelayAttachPost(ctx, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseAttachV1SessionRelayAttachPostResponse(rsp)
}

func (c *ClientWithResponses) DetachV1SessionRelayDetachPostWithBodyWithResponse(ctx context.Context, params *DetachV1SessionRelayDetachPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*DetachV1SessionRelayDetachPostResponse, error) {
	rsp, err := c.DetachV1SessionRelayDetachPostWithBody(ctx, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseDetachV1SessionRelayDetachPostResponse(rsp)
}

func (c *ClientWithResponses) DetachV1SessionRelayDetachPostWithResponse(ctx context.Context, params *DetachV1SessionRelayDetachPostParams, body DetachV1SessionRelayDetachPostJSONRequestBody, reqEditors ...RequestEditorFn) (*DetachV1SessionRelayDetachPostResponse, error) {
	rsp, err := c.DetachV1SessionRelayDetachPost(ctx, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseDetachV1SessionRelayDetachPostResponse(rsp)
}

func (c *ClientWithResponses) InteractionCreateV1SessionRelayInteractionsPostWithBodyWithResponse(ctx context.Context, params *InteractionCreateV1SessionRelayInteractionsPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*InteractionCreateV1SessionRelayInteractionsPostResponse, error) {
	rsp, err := c.InteractionCreateV1SessionRelayInteractionsPostWithBody(ctx, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseInteractionCreateV1SessionRelayInteractionsPostResponse(rsp)
}

func (c *ClientWithResponses) InteractionCreateV1SessionRelayInteractionsPostWithResponse(ctx context.Context, params *InteractionCreateV1SessionRelayInteractionsPostParams, body InteractionCreateV1SessionRelayInteractionsPostJSONRequestBody, reqEditors ...RequestEditorFn) (*InteractionCreateV1SessionRelayInteractionsPostResponse, error) {
	rsp, err := c.InteractionCreateV1SessionRelayInteractionsPost(ctx, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseInteractionCreateV1SessionRelayInteractionsPostResponse(rsp)
}

func (c *ClientWithResponses) InteractionReadV1SessionRelayInteractionsIdentGetWithResponse(ctx context.Context, ident openapi_types.UUID, params *InteractionReadV1SessionRelayInteractionsIdentGetParams, reqEditors ...RequestEditorFn) (*InteractionReadV1SessionRelayInteractionsIdentGetResponse, error) {
	rsp, err := c.InteractionReadV1SessionRelayInteractionsIdentGet(ctx, ident, params, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseInteractionReadV1SessionRelayInteractionsIdentGetResponse(rsp)
}

func (c *ClientWithResponses) InteractionCloseV1SessionRelayInteractionsIdentClosePostWithBodyWithResponse(ctx context.Context, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*InteractionCloseV1SessionRelayInteractionsIdentClosePostResponse, error) {
	rsp, err := c.InteractionCloseV1SessionRelayInteractionsIdentClosePostWithBody(ctx, ident, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseInteractionCloseV1SessionRelayInteractionsIdentClosePostResponse(rsp)
}

func (c *ClientWithResponses) InteractionCloseV1SessionRelayInteractionsIdentClosePostWithResponse(ctx context.Context, ident openapi_types.UUID, params *InteractionCloseV1SessionRelayInteractionsIdentClosePostParams, body InteractionCloseV1SessionRelayInteractionsIdentClosePostJSONRequestBody, reqEditors ...RequestEditorFn) (*InteractionCloseV1SessionRelayInteractionsIdentClosePostResponse, error) {
	rsp, err := c.InteractionCloseV1SessionRelayInteractionsIdentClosePost(ctx, ident, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseInteractionCloseV1SessionRelayInteractionsIdentClosePostResponse(rsp)
}

func (c *ClientWithResponses) CancellationV1SessionRelayMessagesMessageIdCancellationGetWithResponse(ctx context.Context, messageId openapi_types.UUID, params *CancellationV1SessionRelayMessagesMessageIdCancellationGetParams, reqEditors ...RequestEditorFn) (*CancellationV1SessionRelayMessagesMessageIdCancellationGetResponse, error) {
	rsp, err := c.CancellationV1SessionRelayMessagesMessageIdCancellationGet(ctx, messageId, params, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseCancellationV1SessionRelayMessagesMessageIdCancellationGetResponse(rsp)
}

func (c *ClientWithResponses) CancelledV1SessionRelayMessagesMessageIdCancelledPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *CancelledV1SessionRelayMessagesMessageIdCancelledPostParams, reqEditors ...RequestEditorFn) (*CancelledV1SessionRelayMessagesMessageIdCancelledPostResponse, error) {
	rsp, err := c.CancelledV1SessionRelayMessagesMessageIdCancelledPost(ctx, messageId, params, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseCancelledV1SessionRelayMessagesMessageIdCancelledPostResponse(rsp)
}

func (c *ClientWithResponses) DispatchV1SessionRelayMessagesMessageIdDispatchPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*DispatchV1SessionRelayMessagesMessageIdDispatchPostResponse, error) {
	rsp, err := c.DispatchV1SessionRelayMessagesMessageIdDispatchPostWithBody(ctx, messageId, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseDispatchV1SessionRelayMessagesMessageIdDispatchPostResponse(rsp)
}

func (c *ClientWithResponses) DispatchV1SessionRelayMessagesMessageIdDispatchPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *DispatchV1SessionRelayMessagesMessageIdDispatchPostParams, body DispatchV1SessionRelayMessagesMessageIdDispatchPostJSONRequestBody, reqEditors ...RequestEditorFn) (*DispatchV1SessionRelayMessagesMessageIdDispatchPostResponse, error) {
	rsp, err := c.DispatchV1SessionRelayMessagesMessageIdDispatchPost(ctx, messageId, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseDispatchV1SessionRelayMessagesMessageIdDispatchPostResponse(rsp)
}

func (c *ClientWithResponses) ResultV1SessionRelayMessagesMessageIdResultPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*ResultV1SessionRelayMessagesMessageIdResultPostResponse, error) {
	rsp, err := c.ResultV1SessionRelayMessagesMessageIdResultPostWithBody(ctx, messageId, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseResultV1SessionRelayMessagesMessageIdResultPostResponse(rsp)
}

func (c *ClientWithResponses) ResultV1SessionRelayMessagesMessageIdResultPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *ResultV1SessionRelayMessagesMessageIdResultPostParams, body ResultV1SessionRelayMessagesMessageIdResultPostJSONRequestBody, reqEditors ...RequestEditorFn) (*ResultV1SessionRelayMessagesMessageIdResultPostResponse, error) {
	rsp, err := c.ResultV1SessionRelayMessagesMessageIdResultPost(ctx, messageId, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseResultV1SessionRelayMessagesMessageIdResultPostResponse(rsp)
}

func (c *ClientWithResponses) SummaryV1SessionRelayMessagesMessageIdSummaryPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*SummaryV1SessionRelayMessagesMessageIdSummaryPostResponse, error) {
	rsp, err := c.SummaryV1SessionRelayMessagesMessageIdSummaryPostWithBody(ctx, messageId, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseSummaryV1SessionRelayMessagesMessageIdSummaryPostResponse(rsp)
}

func (c *ClientWithResponses) SummaryV1SessionRelayMessagesMessageIdSummaryPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *SummaryV1SessionRelayMessagesMessageIdSummaryPostParams, body SummaryV1SessionRelayMessagesMessageIdSummaryPostJSONRequestBody, reqEditors ...RequestEditorFn) (*SummaryV1SessionRelayMessagesMessageIdSummaryPostResponse, error) {
	rsp, err := c.SummaryV1SessionRelayMessagesMessageIdSummaryPost(ctx, messageId, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseSummaryV1SessionRelayMessagesMessageIdSummaryPostResponse(rsp)
}

func (c *ClientWithResponses) WaitingV1SessionRelayMessagesMessageIdWaitingPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*WaitingV1SessionRelayMessagesMessageIdWaitingPostResponse, error) {
	rsp, err := c.WaitingV1SessionRelayMessagesMessageIdWaitingPostWithBody(ctx, messageId, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseWaitingV1SessionRelayMessagesMessageIdWaitingPostResponse(rsp)
}

func (c *ClientWithResponses) WaitingV1SessionRelayMessagesMessageIdWaitingPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *WaitingV1SessionRelayMessagesMessageIdWaitingPostParams, body WaitingV1SessionRelayMessagesMessageIdWaitingPostJSONRequestBody, reqEditors ...RequestEditorFn) (*WaitingV1SessionRelayMessagesMessageIdWaitingPostResponse, error) {
	rsp, err := c.WaitingV1SessionRelayMessagesMessageIdWaitingPost(ctx, messageId, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseWaitingV1SessionRelayMessagesMessageIdWaitingPostResponse(rsp)
}

func (c *ClientWithResponses) WorkingV1SessionRelayMessagesMessageIdWorkingPostWithBodyWithResponse(ctx context.Context, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*WorkingV1SessionRelayMessagesMessageIdWorkingPostResponse, error) {
	rsp, err := c.WorkingV1SessionRelayMessagesMessageIdWorkingPostWithBody(ctx, messageId, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseWorkingV1SessionRelayMessagesMessageIdWorkingPostResponse(rsp)
}

func (c *ClientWithResponses) WorkingV1SessionRelayMessagesMessageIdWorkingPostWithResponse(ctx context.Context, messageId openapi_types.UUID, params *WorkingV1SessionRelayMessagesMessageIdWorkingPostParams, body WorkingV1SessionRelayMessagesMessageIdWorkingPostJSONRequestBody, reqEditors ...RequestEditorFn) (*WorkingV1SessionRelayMessagesMessageIdWorkingPostResponse, error) {
	rsp, err := c.WorkingV1SessionRelayMessagesMessageIdWorkingPost(ctx, messageId, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseWorkingV1SessionRelayMessagesMessageIdWorkingPostResponse(rsp)
}

func (c *ClientWithResponses) OriginV1SessionRelayOriginPostWithBodyWithResponse(ctx context.Context, params *OriginV1SessionRelayOriginPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*OriginV1SessionRelayOriginPostResponse, error) {
	rsp, err := c.OriginV1SessionRelayOriginPostWithBody(ctx, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseOriginV1SessionRelayOriginPostResponse(rsp)
}

func (c *ClientWithResponses) OriginV1SessionRelayOriginPostWithResponse(ctx context.Context, params *OriginV1SessionRelayOriginPostParams, body OriginV1SessionRelayOriginPostJSONRequestBody, reqEditors ...RequestEditorFn) (*OriginV1SessionRelayOriginPostResponse, error) {
	rsp, err := c.OriginV1SessionRelayOriginPost(ctx, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseOriginV1SessionRelayOriginPostResponse(rsp)
}

func (c *ClientWithResponses) PollV1SessionRelayPollPostWithResponse(ctx context.Context, params *PollV1SessionRelayPollPostParams, reqEditors ...RequestEditorFn) (*PollV1SessionRelayPollPostResponse, error) {
	rsp, err := c.PollV1SessionRelayPollPost(ctx, params, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParsePollV1SessionRelayPollPostResponse(rsp)
}

func (c *ClientWithResponses) ReadinessV1SessionRelayReadinessPostWithBodyWithResponse(ctx context.Context, params *ReadinessV1SessionRelayReadinessPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*ReadinessV1SessionRelayReadinessPostResponse, error) {
	rsp, err := c.ReadinessV1SessionRelayReadinessPostWithBody(ctx, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseReadinessV1SessionRelayReadinessPostResponse(rsp)
}

func (c *ClientWithResponses) ReadinessV1SessionRelayReadinessPostWithResponse(ctx context.Context, params *ReadinessV1SessionRelayReadinessPostParams, body ReadinessV1SessionRelayReadinessPostJSONRequestBody, reqEditors ...RequestEditorFn) (*ReadinessV1SessionRelayReadinessPostResponse, error) {
	rsp, err := c.ReadinessV1SessionRelayReadinessPost(ctx, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseReadinessV1SessionRelayReadinessPostResponse(rsp)
}

func (c *ClientWithResponses) SummaryClaimV1SessionRelaySummariesClaimPostWithBodyWithResponse(ctx context.Context, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, contentType string, body io.Reader, reqEditors ...RequestEditorFn) (*SummaryClaimV1SessionRelaySummariesClaimPostResponse, error) {
	rsp, err := c.SummaryClaimV1SessionRelaySummariesClaimPostWithBody(ctx, params, contentType, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseSummaryClaimV1SessionRelaySummariesClaimPostResponse(rsp)
}

func (c *ClientWithResponses) SummaryClaimV1SessionRelaySummariesClaimPostWithResponse(ctx context.Context, params *SummaryClaimV1SessionRelaySummariesClaimPostParams, body SummaryClaimV1SessionRelaySummariesClaimPostJSONRequestBody, reqEditors ...RequestEditorFn) (*SummaryClaimV1SessionRelaySummariesClaimPostResponse, error) {
	rsp, err := c.SummaryClaimV1SessionRelaySummariesClaimPost(ctx, params, body, reqEditors...)
	if err != nil {
		return nil, err
	}
	return ParseSummaryClaimV1SessionRelaySummariesClaimPostResponse(rsp)
}

func ParseStartV1AuthDevicesPostResponse(rsp *http.Response) (*StartV1AuthDevicesPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &StartV1AuthDevicesPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 201:
		var dest DeviceCreated
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON201 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseClaimV1AuthDevicesDeviceIdTokenPostResponse(rsp *http.Response) (*ClaimV1AuthDevicesDeviceIdTokenPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &ClaimV1AuthDevicesDeviceIdTokenPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest DeviceToken
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseLogoutV1AuthSessionDeleteResponse(rsp *http.Response) (*LogoutV1AuthSessionDeleteResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &LogoutV1AuthSessionDeleteResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseMeV1MeGetResponse(rsp *http.Response) (*MeV1MeGetResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &MeV1MeGetResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest UserView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseStatusV1SessionRelayGetResponse(rsp *http.Response) (*StatusV1SessionRelayGetResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &StatusV1SessionRelayGetResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest RelayView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseAsyncAnswerV1SessionRelayAsyncAnswersPostResponse(rsp *http.Response) (*AsyncAnswerV1SessionRelayAsyncAnswersPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &AsyncAnswerV1SessionRelayAsyncAnswersPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest map[string]bool
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseAttachV1SessionRelayAttachPostResponse(rsp *http.Response) (*AttachV1SessionRelayAttachPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &AttachV1SessionRelayAttachPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest RelayView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseDetachV1SessionRelayDetachPostResponse(rsp *http.Response) (*DetachV1SessionRelayDetachPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &DetachV1SessionRelayDetachPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest RelayView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseInteractionCreateV1SessionRelayInteractionsPostResponse(rsp *http.Response) (*InteractionCreateV1SessionRelayInteractionsPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &InteractionCreateV1SessionRelayInteractionsPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest InteractionView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseInteractionReadV1SessionRelayInteractionsIdentGetResponse(rsp *http.Response) (*InteractionReadV1SessionRelayInteractionsIdentGetResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &InteractionReadV1SessionRelayInteractionsIdentGetResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest InteractionView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseInteractionCloseV1SessionRelayInteractionsIdentClosePostResponse(rsp *http.Response) (*InteractionCloseV1SessionRelayInteractionsIdentClosePostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &InteractionCloseV1SessionRelayInteractionsIdentClosePostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest InteractionView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseCancellationV1SessionRelayMessagesMessageIdCancellationGetResponse(rsp *http.Response) (*CancellationV1SessionRelayMessagesMessageIdCancellationGetResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &CancellationV1SessionRelayMessagesMessageIdCancellationGetResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest map[string]bool
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseCancelledV1SessionRelayMessagesMessageIdCancelledPostResponse(rsp *http.Response) (*CancelledV1SessionRelayMessagesMessageIdCancelledPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &CancelledV1SessionRelayMessagesMessageIdCancelledPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest map[string]bool
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseDispatchV1SessionRelayMessagesMessageIdDispatchPostResponse(rsp *http.Response) (*DispatchV1SessionRelayMessagesMessageIdDispatchPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &DispatchV1SessionRelayMessagesMessageIdDispatchPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest map[string]bool
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseResultV1SessionRelayMessagesMessageIdResultPostResponse(rsp *http.Response) (*ResultV1SessionRelayMessagesMessageIdResultPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &ResultV1SessionRelayMessagesMessageIdResultPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest map[string]bool
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseSummaryV1SessionRelayMessagesMessageIdSummaryPostResponse(rsp *http.Response) (*SummaryV1SessionRelayMessagesMessageIdSummaryPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &SummaryV1SessionRelayMessagesMessageIdSummaryPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest map[string]bool
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseWaitingV1SessionRelayMessagesMessageIdWaitingPostResponse(rsp *http.Response) (*WaitingV1SessionRelayMessagesMessageIdWaitingPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &WaitingV1SessionRelayMessagesMessageIdWaitingPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest map[string]bool
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseWorkingV1SessionRelayMessagesMessageIdWorkingPostResponse(rsp *http.Response) (*WorkingV1SessionRelayMessagesMessageIdWorkingPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &WorkingV1SessionRelayMessagesMessageIdWorkingPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest map[string]bool
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseOriginV1SessionRelayOriginPostResponse(rsp *http.Response) (*OriginV1SessionRelayOriginPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &OriginV1SessionRelayOriginPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest OriginView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParsePollV1SessionRelayPollPostResponse(rsp *http.Response) (*PollV1SessionRelayPollPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &PollV1SessionRelayPollPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest RelayView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseReadinessV1SessionRelayReadinessPostResponse(rsp *http.Response) (*ReadinessV1SessionRelayReadinessPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &ReadinessV1SessionRelayReadinessPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest RelayView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}

func ParseSummaryClaimV1SessionRelaySummariesClaimPostResponse(rsp *http.Response) (*SummaryClaimV1SessionRelaySummariesClaimPostResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	response := &SummaryClaimV1SessionRelaySummariesClaimPostResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest RelaySummaryClaimView
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 401:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON401 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 403:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON403 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 404:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON404 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 409:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON409 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 422:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON422 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 429:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON429 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 503:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON503 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 504:
		var dest ErrorBody
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON504 = &dest

	}

	return response, nil
}
