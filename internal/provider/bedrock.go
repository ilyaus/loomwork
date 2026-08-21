package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// Bedrock is the AWS Bedrock adapter. It talks to the Converse API so a single
// body shape covers every model family, and delegates SigV4 signing to the AWS
// SDK rather than hand-rolling it.
type Bedrock struct {
	region       string
	modelID      string
	profile      string
	accessKeyID  string
	secretKey    string
	sessionToken string
	// endpoint overrides the resolved AWS endpoint (VPC endpoints, tests).
	endpoint string
	timeout  time.Duration

	mu       sync.Mutex
	awsCfg   *aws.Config
	runtime  *bedrockruntime.Client
	controlP *bedrock.Client
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
		endpoint:     strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		timeout:      cfg.Timeout(),
	}, nil
}

// Name identifies the adapter.
func (b *Bedrock) Name() string { return string(KindBedrock) }

// ConverseURL is the runtime endpoint the SDK targets for a model.
func (b *Bedrock) ConverseURL(model string) string {
	base := b.endpoint
	if base == "" {
		base = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", b.region)
	}
	return fmt.Sprintf("%s/model/%s/converse", base, firstNonEmpty(model, b.modelID))
}

// Generate performs a Converse call. Temperature, TopP, MaxOutputTokens, and
// Stop are the only normalized parameters Converse accepts portably; TopK,
// RepeatPenalty, ContextWindow, and Seed are model-family specific and are
// dropped rather than guessed at — pass them through Params.Extra, which becomes
// additionalModelRequestFields.
func (b *Bedrock) Generate(ctx context.Context, req Request) (Response, error) {
	req.Model = firstNonEmpty(req.Model, b.modelID)
	if err := req.Validate(); err != nil {
		return Response{}, fmt.Errorf("bedrock: %w", err)
	}
	client, err := b.runtimeClient(ctx)
	if err != nil {
		return Response{}, fmt.Errorf("bedrock generate (region %s, model %s): %w", b.region, req.Model, err)
	}

	output, err := client.Converse(ctx, bedrockConverseInput(req))
	if err != nil {
		return Response{}, fmt.Errorf("bedrock generate (region %s, model %s): %w", b.region, req.Model, err)
	}
	text := converseText(output.Output)
	if strings.TrimSpace(text) == "" {
		return Response{}, fmt.Errorf("bedrock generate (region %s, model %s): empty completion", b.region, req.Model)
	}

	return Response{
		Text:         text,
		Model:        req.Model,
		FinishReason: string(output.StopReason),
		Usage:        converseUsage(output.Usage),
		Raw:          map[string]string{"provider": b.Name(), "region": b.region},
	}, nil
}

// Models lists the foundation models the account can call in the region.
func (b *Bedrock) Models(ctx context.Context) ([]Model, error) {
	client, err := b.controlClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("bedrock models (region %s): %w", b.region, err)
	}
	output, err := client.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return nil, fmt.Errorf("bedrock models (region %s): %w", b.region, err)
	}
	models := make([]Model, 0, len(output.ModelSummaries))
	for _, summary := range output.ModelSummaries {
		models = append(models, Model{
			ID:          aws.ToString(summary.ModelId),
			Description: strings.TrimSpace(fmt.Sprintf("%s %s", aws.ToString(summary.ProviderName), aws.ToString(summary.ModelName))),
		})
	}
	return models, nil
}

// bedrockConverseInput maps a normalized request onto the Converse schema.
func bedrockConverseInput(req Request) *bedrockruntime.ConverseInput {
	messages := make([]types.Message, 0, 2)
	if rendered := RenderContext(req.Context); rendered != "" {
		messages = append(messages, userMessage(rendered))
	}
	messages = append(messages, userMessage(req.Prompt))

	input := &bedrockruntime.ConverseInput{
		ModelId:  aws.String(req.Model),
		Messages: messages,
	}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		input.System = []types.SystemContentBlock{&types.SystemContentBlockMemberText{Value: req.SystemPrompt}}
	}
	inference := &types.InferenceConfiguration{}
	set := false
	if req.Params.Temperature != nil {
		inference.Temperature = aws.Float32(float32(*req.Params.Temperature))
		set = true
	}
	if req.Params.TopP != nil {
		inference.TopP = aws.Float32(float32(*req.Params.TopP))
		set = true
	}
	if req.Params.MaxOutputTokens != nil {
		inference.MaxTokens = aws.Int32(int32(*req.Params.MaxOutputTokens))
		set = true
	}
	if len(req.Params.Stop) > 0 {
		inference.StopSequences = append([]string(nil), req.Params.Stop...)
		set = true
	}
	if set {
		input.InferenceConfig = inference
	}
	if len(req.Params.Extra) > 0 {
		input.AdditionalModelRequestFields = document.NewLazyDocument(req.Params.Extra)
	}
	return input
}

func userMessage(text string) types.Message {
	return types.Message{
		Role:    types.ConversationRoleUser,
		Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: text}},
	}
}

// converseText concatenates the text blocks of an assistant message.
func converseText(output types.ConverseOutput) string {
	message, ok := output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return ""
	}
	var builder strings.Builder
	for _, block := range message.Value.Content {
		if text, ok := block.(*types.ContentBlockMemberText); ok {
			builder.WriteString(text.Value)
		}
	}
	return builder.String()
}

func converseUsage(usage *types.TokenUsage) Usage {
	if usage == nil {
		return Usage{}
	}
	return Usage{
		PromptTokens:     int(aws.ToInt32(usage.InputTokens)),
		CompletionTokens: int(aws.ToInt32(usage.OutputTokens)),
		TotalTokens:      int(aws.ToInt32(usage.TotalTokens)),
	}
}

func (b *Bedrock) runtimeClient(ctx context.Context) (*bedrockruntime.Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.runtime != nil {
		return b.runtime, nil
	}
	cfg, err := b.loadAWSConfig(ctx)
	if err != nil {
		return nil, err
	}
	b.runtime = bedrockruntime.NewFromConfig(*cfg, func(options *bedrockruntime.Options) {
		if b.endpoint != "" {
			options.BaseEndpoint = aws.String(b.endpoint)
		}
	})
	return b.runtime, nil
}

func (b *Bedrock) controlClient(ctx context.Context) (*bedrock.Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.controlP != nil {
		return b.controlP, nil
	}
	cfg, err := b.loadAWSConfig(ctx)
	if err != nil {
		return nil, err
	}
	b.controlP = bedrock.NewFromConfig(*cfg, func(options *bedrock.Options) {
		if b.endpoint != "" {
			options.BaseEndpoint = aws.String(b.endpoint)
		}
	})
	return b.controlP, nil
}

// loadAWSConfig resolves credentials once: static keys from the environment when
// present, otherwise the named shared-config profile. Credential values are
// never logged.
func (b *Bedrock) loadAWSConfig(ctx context.Context) (*aws.Config, error) {
	if b.awsCfg != nil {
		return b.awsCfg, nil
	}
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(b.region),
		awsconfig.WithHTTPClient(newHTTPClient(b.timeout)),
	}
	switch {
	case b.accessKeyID != "" && b.secretKey != "":
		options = append(options, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(b.accessKeyID, b.secretKey, b.sessionToken)))
	case b.profile != "":
		options = append(options, awsconfig.WithSharedConfigProfile(b.profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load aws configuration: %w", err)
	}
	b.awsCfg = &cfg
	return b.awsCfg, nil
}
