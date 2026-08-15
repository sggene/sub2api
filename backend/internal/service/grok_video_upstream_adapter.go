package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	grokTaskVideoMinSeconds = 1
	grokTaskVideoMaxSeconds = 15
)

type grokTaskVideoGenerationPayload struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	Seconds     string   `json:"seconds"`
	AspectRatio string   `json:"aspect_ratio,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	Size        string   `json:"size,omitempty"`
	Images      []string `json:"images,omitempty"`
}

func isGrokTaskVideosEndpoint(account *Account, endpoint GrokMediaEndpoint) bool {
	if account == nil || !account.UsesTaskVideosUpstream() {
		return false
	}
	switch endpoint {
	case GrokMediaEndpointVideosGenerations, GrokMediaEndpointVideoStatus, GrokMediaEndpointVideoContent:
		return true
	default:
		return false
	}
}

// GrokMediaAccountSupportsRequest keeps native xAI and task-style video
// accounts from being mixed by the scheduler. Account-level model mapping is
// resolved first so administrators can explicitly bridge a public alias to the
// protocol-specific upstream model.
func GrokMediaAccountSupportsRequest(account *Account, endpoint GrokMediaEndpoint, routingModel string) (bool, string) {
	if account == nil {
		return false, "missing_account"
	}
	if endpoint.IsVideoLookupRequest() {
		return true, "video_lookup_bound_account"
	}

	if account.UsesTaskVideosUpstream() {
		if endpoint != GrokMediaEndpointVideosGenerations {
			return false, "task_videos_endpoint_unsupported"
		}
	}
	return GrokMediaAccountSupportsRoutingModel(account, routingModel)
}

// GrokMediaAccountSupportsRoutingModel is the protocol-only eligibility check
// used by account scheduling before a concrete media endpoint is selected.
func GrokMediaAccountSupportsRoutingModel(account *Account, routingModel string) (bool, string) {
	if account == nil {
		return false, "missing_account"
	}
	upstreamModel := grokMediaMappedModel(account, routingModel)
	taskModel := isGrokTaskVideoModel(upstreamModel)
	if account.UsesTaskVideosUpstream() {
		if !taskModel {
			return false, "task_videos_model_unsupported"
		}
		return true, "task_videos_supported"
	}
	if taskModel {
		return false, "xai_task_video_model_unsupported"
	}
	return true, "xai_supported"
}

// grokMediaMappedModel applies the account mapping and then bridges xAI's
// public video aliases to the task-style upstream's variable-duration model.
// An explicit mapping to one of the four task models always wins.
func grokMediaMappedModel(account *Account, routingModel string) string {
	if account == nil {
		return strings.TrimSpace(routingModel)
	}
	routingModel = strings.TrimSpace(routingModel)
	upstreamModel := strings.TrimSpace(account.GetMappedModel(routingModel))
	if !account.UsesTaskVideosUpstream() || isGrokTaskVideoModel(upstreamModel) {
		return upstreamModel
	}

	// Empty mappings and the UI's identity mappings both retain the public
	// model name. Treat those as aliases; a distinct unsupported target still
	// fails closed during protocol eligibility checks.
	if upstreamModel != "" && !strings.EqualFold(upstreamModel, routingModel) {
		return upstreamModel
	}
	switch strings.ToLower(routingModel) {
	case "grok-imagine-video", "grok-imagine-video-1.5":
		return "grok-video-3"
	default:
		return upstreamModel
	}
}

func isGrokTaskVideoModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "grok-video-3", "grok-1.5-video-6s", "grok-1.5-video-10s", "grok-1.5-video-15s":
		return true
	default:
		return false
	}
}

func grokMediaAuthorizationValue(account *Account, endpoint GrokMediaEndpoint, token string) string {
	if isGrokTaskVideosEndpoint(account, endpoint) {
		return token
	}
	return "Bearer " + token
}

func adaptGrokTaskVideoGenerationBody(body []byte, upstreamModel string) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("grok task video generation requires a JSON request body")
	}

	model := strings.TrimSpace(upstreamModel)
	if model == "" {
		model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	}
	if model == "" {
		return nil, fmt.Errorf("grok task video model is required")
	}
	prompt := strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	if prompt == "" {
		return nil, fmt.Errorf("grok task video prompt is required")
	}

	seconds, provided, err := parseGrokTaskVideoSeconds(body)
	if err != nil {
		return nil, err
	}
	if fixedSeconds, fixed := grokTaskVideoFixedSeconds(model); fixed {
		if provided && seconds != fixedSeconds {
			return nil, fmt.Errorf("grok task video model %s requires seconds=%d", model, fixedSeconds)
		}
		seconds = fixedSeconds
	} else if !provided {
		seconds = VideoBillingDefaultDurationSeconds
	}
	if seconds < grokTaskVideoMinSeconds || seconds > grokTaskVideoMaxSeconds {
		return nil, fmt.Errorf("grok task video seconds must be between %d and %d", grokTaskVideoMinSeconds, grokTaskVideoMaxSeconds)
	}

	resolution, useSizeField, err := grokTaskVideoResolution(body)
	if err != nil {
		return nil, err
	}
	payload := grokTaskVideoGenerationPayload{
		Model:       model,
		Prompt:      prompt,
		Seconds:     strconv.Itoa(seconds),
		AspectRatio: strings.TrimSpace(gjson.GetBytes(body, "aspect_ratio").String()),
		Images:      grokTaskVideoImages(body),
	}
	if useSizeField {
		payload.Size = resolution
	} else {
		payload.Resolution = resolution
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode grok task video request: %w", err)
	}
	return out, nil
}

func parseGrokTaskVideoSeconds(body []byte) (int, bool, error) {
	for _, field := range []string{"seconds", "duration"} {
		value := gjson.GetBytes(body, field)
		if !value.Exists() {
			continue
		}
		raw := strings.TrimSpace(value.String())
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return 0, true, fmt.Errorf("grok task video %s must be an integer", field)
		}
		return seconds, true, nil
	}
	return 0, false, nil
}

func grokTaskVideoFixedSeconds(model string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "grok-1.5-video-6s":
		return 6, true
	case "grok-1.5-video-10s":
		return 10, true
	case "grok-1.5-video-15s":
		return 15, true
	default:
		return 0, false
	}
}

func grokTaskVideoResolution(body []byte) (string, bool, error) {
	raw := strings.TrimSpace(gjson.GetBytes(body, "resolution").String())
	useSizeField := false
	if raw == "" {
		raw = strings.TrimSpace(gjson.GetBytes(body, "size").String())
		useSizeField = raw != ""
	}
	if raw == "" {
		return "", false, nil
	}
	switch strings.ToUpper(raw) {
	case "480", "480P":
		return "480P", useSizeField, nil
	case "720", "720P":
		return "720P", useSizeField, nil
	default:
		return "", false, fmt.Errorf("grok task video resolution must be 480P or 720P")
	}
}

func grokTaskVideoImages(body []byte) []string {
	images := make([]string, 0)
	appendValue := func(value gjson.Result) {
		if !value.Exists() {
			return
		}
		if value.IsArray() {
			for _, item := range value.Array() {
				if imageURL := grokTaskVideoImageURL(item); imageURL != "" {
					images = append(images, imageURL)
				}
			}
			return
		}
		if imageURL := grokTaskVideoImageURL(value); imageURL != "" {
			images = append(images, imageURL)
		}
	}
	appendValue(gjson.GetBytes(body, "image"))
	appendValue(gjson.GetBytes(body, "images"))
	appendValue(gjson.GetBytes(body, "reference_images"))
	return images
}

func grokTaskVideoImageURL(value gjson.Result) string {
	if value.Type == gjson.String {
		return strings.TrimSpace(value.String())
	}
	return extractGrokMediaImageURL(value)
}
