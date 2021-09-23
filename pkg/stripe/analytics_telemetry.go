package stripe

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/stripe/stripe-cli/pkg/useragent"
	"github.com/stripe/stripe-cli/pkg/version"
)

//
// Public types
//
type TelemetryContextKey struct{}
type TelemetryClientKey struct{}

// DefaultTelemetryEndpoint is the default URL for the telemetry destination
const DefaultTelemetryEndpoint = "https://r.stripe.com/0"

// CLIAnalyticsEventContext is the structure that holds telemetry data context that is ultimately sent to the Stripe Analytics Service.
type CLIAnalyticsEventContext struct {
	InvocationID      string `url:"invocation_id"`      // The invocation id is unique to each context object and represents all events coming from one command / gRPC method call
	UserAgent         string `url:"user_agent"`         // the application that is used to create this request
	CommandPath       string `url:"command_path"`       // the command or gRPC method that initiated this request
	Merchant          string `url:"merchant"`           // the merchant ID: ex. acct_xxxx
	CLIVersion        string `url:"cli_version"`        // the version of the CLI
	OS                string `url:"os"`                 // the OS of the system
	GeneratedResource bool   `url:"generated_resource"` // whether or not this was a generated resource
}

// TelemetryClient is an interface that can send two types of events: an API request, and just general events.
type TelemetryClient interface {
	SendAPIRequestEvent(ctx context.Context, requestID string, livemode bool) (*http.Response, error)
	SendEvent(ctx context.Context, eventName string, eventValue string) (*http.Response, error)
}

// AnalyticsTelemetryClient sends event information to r.stripe.com
type AnalyticsTelemetryClient struct {
	BaseURL    *url.URL
	WG         *sync.WaitGroup
	HttpClient *http.Client
}

//
// Public functions
//
func InitContext() *CLIAnalyticsEventContext {
	// if the get config errors, don't fail running the command
	return &CLIAnalyticsEventContext{
		InvocationID: uuid.NewString(),
		UserAgent:    useragent.GetEncodedUserAgent(),
		CLIVersion:   version.Version,
		OS:           runtime.GOOS,
	}
}

// SetCobraCommandContext sets the telemetry values for the command being executed.
// Needs to come from the gRPC method name.
func (e *CLIAnalyticsEventContext) SetCobraCommandContext(cmd *cobra.Command) {
	e.CommandPath = cmd.CommandPath()
	e.GeneratedResource = false

	for _, value := range cmd.Annotations {
		// Generated commands have an annotation called "operation", we can
		// search for that to let us know it's generated
		if value == "operation" {
			e.GeneratedResource = true
		}
	}
}

func (e *CLIAnalyticsEventContext) SetMerchant(merchant string) {
	e.Merchant = merchant
}

// special function for API requests
func (a *AnalyticsTelemetryClient) SendAPIRequestEvent(ctx context.Context, requestID string, livemode bool) (*http.Response, error) {
	a.WG.Add(1)
	defer a.WG.Done()
	if (ctx.Value(TelemetryContextKey{}) != nil) {
		data, _ := query.Values(ctx.Value(TelemetryContextKey{}))

		data.Set("client_id", "stripe-cli")
		data.Set("request_id", requestID)
		data.Set("livemode", strconv.FormatBool(livemode))
		data.Set("event_id", uuid.NewString())
		data.Set("event_name", "API Request")
		data.Set("event_value", "")
		data.Set("created", fmt.Sprint((time.Now().Unix())))

		return a.sendData(ctx, data)
	}
	return nil, nil
}

// SendEvent sends a telemetry event to r.stripe.com
func (a *AnalyticsTelemetryClient) SendEvent(ctx context.Context, eventName string, eventValue string) (*http.Response, error) {
	a.WG.Add(1)
	defer a.WG.Done()
	if (ctx.Value(TelemetryContextKey{}) != nil) {
		data, _ := query.Values(ctx.Value(TelemetryContextKey{}))

		data.Set("client_id", "stripe-cli")
		data.Set("event_id", uuid.NewString())
		data.Set("event_name", eventName)
		data.Set("event_value", eventValue)
		data.Set("created", fmt.Sprint((time.Now().Unix())))

		return a.sendData(ctx, data)
	}
	return nil, nil
}

func (a *AnalyticsTelemetryClient) sendData(ctx context.Context, data url.Values) (*http.Response, error) {
	if telemetryOptedOut(os.Getenv("STRIPE_CLI_TELEMETRY_OPTOUT")) {
		return nil, nil
	}

	if a.BaseURL == nil {
		analyticsURL, err := url.Parse(DefaultTelemetryEndpoint)
		if err != nil {
			return nil, err
		}
		a.BaseURL = analyticsURL
	}

	req, err := http.NewRequest(http.MethodPost, a.BaseURL.String(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("origin", "stripe-cli")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if ctx != nil {
		req = req.WithContext(ctx)
	}

	resp, err := a.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func telemetryOptedOut(optoutVar string) bool {
	optoutVar = strings.ToLower(optoutVar)

	return optoutVar == "1" || optoutVar == "true"
}
