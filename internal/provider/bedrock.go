package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Bedrock is the AWS Bedrock adapter.
//
// Status: SCAFFOLD. Configuration and credential resolution are implemented and
// testable; request signing and body mapping are deferred and Generate returns
// an error wrapping ErrNotImplemented.
//
// TODO(bedrock): implement Generate against
// POST https://bedrock-runtime.{region}.amazonaws.com/model/{modelId}/converse
// using the Converse API so one body shape covers every model family.
// TODO(bedrock): implement pure-Go SigV4 request signing (no CGO, standard
// library crypto only). TODO(bedrock): implement Models via the
// bedrock.{region}.amazonaws.com ListFoundationModels call.
type Bedrock struct {
	region       string
	modelID      string
	profile      string
	accessKeyID  string
	secretKey    string
	sessionToken string
}

// NewBedrock builds the Bedrock adapter, validating the region/model and
// resolving AWS credentials from the environment.
func NewBedrock(cfg Config) (*Bedrock, error) {
	region := firstNonEmpty(cfg.Bedrock.Region, os.Getenv(EnvAWSRegion))
	if region == "" {
		return nil, fmt.Errorf("bedrock provider requires bedrock.region or %s", EnvAWSRegion)
	}
	accessKeyID := strings.TrimSpace(os.Getenv(EnvAWSAccessKeyID))
	secretKey := strings.TrimSpace(os.Getenv(EnvAWSSecretAccessKey))
	profile := strings.TrimSpace(cfg.Bedrock.Profile)
	if profile == "" && (accessKeyID == "" || secretKey == "") {
		return nil, fmt.Errorf("bedrock provider requires %s and %s, or bedrock.profile", EnvAWSAccessKeyID, EnvAWSSecretAccessKey)
	}
	return &Bedrock{
		region:       region,
		modelID:      firstNonEmpty(cfg.Bedrock.ModelID, cfg.DefaultModel),
		profile:      profile,
		accessKeyID:  accessKeyID,
		secretKey:    secretKey,
		sessionToken: strings.TrimSpace(os.Getenv(EnvAWSSessionToken)),
	}, nil
}

// Name identifies the adapter.
func (b *Bedrock) Name() string { return string(KindBedrock) }

// ConverseURL is the endpoint Generate will target once implemented. It exists
// so wiring and configuration can be verified today.
func (b *Bedrock) ConverseURL(model string) string {
	return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/converse", b.region, firstNonEmpty(model, b.modelID))
}

// Generate is not implemented yet.
func (b *Bedrock) Generate(_ context.Context, req Request) (Response, error) {
	model := firstNonEmpty(req.Model, b.modelID)
	return Response{}, fmt.Errorf("bedrock generate (region %s, model %s): %w", b.region, model, ErrNotImplemented)
}

// Models is not implemented yet.
func (b *Bedrock) Models(_ context.Context) ([]Model, error) {
	return nil, fmt.Errorf("bedrock models (region %s): %w", b.region, ErrNotImplemented)
}
