//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func grokTaskVideosTestAccount() *Account {
	return &Account{
		ID:          81,
		Name:        "task-videos",
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":                           "sk-task-upstream",
			"base_url":                          "https://relay.example/v1",
			GrokVideoUpstreamStyleCredentialKey: GrokVideoUpstreamStyleTaskVideos,
		},
	}
}

func grokTaskVideosTestContext(method, target string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, bytes.NewReader(body))
	if len(body) > 0 {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	return c, recorder
}

func TestForwardGrokTaskVideoGenerationAdaptsURLAuthMappedModelAndPayload(t *testing.T) {
	body := []byte(`{
		"model":"client-video",
		"prompt":"cat",
		"size":"720",
		"aspect_ratio":"16:9",
		"image":"data:image/png;base64,one",
		"images":[{"image_url":"https://images.example/two.png"}],
		"reference_images":{"url":"https://images.example/three.png"},
		"ignored":"drop-me"
	}`)
	c, recorder := grokTaskVideosTestContext(http.MethodPost, "/v1/videos/generations", body)
	account := grokTaskVideosTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"client-video": "grok-1.5-video-10s"}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-generated","status":"processing"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, account,
		GrokMediaEndpointVideosGenerations, "", body, "application/json",
	)

	require.NoError(t, err)
	require.Equal(t, "https://relay.example/v1/videos", upstream.lastReq.URL.String())
	require.Equal(t, http.MethodPost, upstream.lastReq.Method)
	require.Equal(t, "sk-task-upstream", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Content-Type"))
	require.JSONEq(t, `{
		"model":"grok-1.5-video-10s",
		"prompt":"cat",
		"seconds":"10",
		"aspect_ratio":"16:9",
		"size":"720P",
		"images":[
			"data:image/png;base64,one",
			"https://images.example/two.png",
			"https://images.example/three.png"
		]
	}`, string(upstream.lastBody))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "task-generated", result.ResponseID)
	require.Equal(t, "client-video", result.Model)
	require.Equal(t, "grok-1.5-video-10s", result.UpstreamModel)
	require.Equal(t, "/v1/videos", result.UpstreamEndpoint)
	require.Equal(t, "/v1/videos", GetActualOpenAIUpstreamEndpoint(c))
	require.Equal(t, 10, result.VideoDurationSeconds)
	require.Equal(t, VideoBillingResolution720P, result.VideoResolution)
}

func TestGrokTaskVideoPublicAliasesDefaultToVariableDurationModel(t *testing.T) {
	account := grokTaskVideosTestAccount()
	account.Credentials["model_mapping"] = map[string]any{
		"grok-imagine-video": "grok-imagine-video",
	}

	for _, model := range []string{"grok-imagine-video", "grok-imagine-video-1.5"} {
		require.Equal(t, "grok-video-3", grokMediaMappedModel(account, model))
		eligible, reason := GrokMediaAccountSupportsRoutingModel(account, model)
		require.True(t, eligible)
		require.Equal(t, "task_videos_supported", reason)
	}

	account.Credentials["model_mapping"] = map[string]any{
		"grok-imagine-video": "grok-1.5-video-10s",
	}
	require.Equal(t, "grok-1.5-video-10s", grokMediaMappedModel(account, "grok-imagine-video"))
}

func TestForwardGrokTaskVideoPublicAliasUsesVariableDurationModel(t *testing.T) {
	body := []byte(`{
		"model":"grok-imagine-video",
		"prompt":"cat walks forward",
		"resolution":"720p",
		"duration":8,
		"image":{"image_url":"https://images.example/cat.jpeg"}
	}`)
	c, _ := grokTaskVideosTestContext(http.MethodPost, "/v1/videos/generations", body)
	account := grokTaskVideosTestAccount()
	account.Credentials["model_mapping"] = map[string]any{
		"grok-imagine-video": "grok-imagine-video",
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-alias","status":"processing"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, account,
		GrokMediaEndpointVideosGenerations, "", body, "application/json",
	)

	require.NoError(t, err)
	require.Equal(t, "https://relay.example/v1/videos", upstream.lastReq.URL.String())
	require.Equal(t, "sk-task-upstream", upstream.lastReq.Header.Get("Authorization"))
	require.JSONEq(t, `{
		"model":"grok-video-3",
		"prompt":"cat walks forward",
		"seconds":"8",
		"resolution":"720P",
		"images":["https://images.example/cat.jpeg"]
	}`, string(upstream.lastBody))
	require.Equal(t, "grok-video-3", result.UpstreamModel)
	require.Equal(t, "/v1/videos", result.UpstreamEndpoint)
}

func TestAdaptGrokTaskVideoGenerationBodySecondsRules(t *testing.T) {
	t.Run("grok video 3 accepts seconds string", func(t *testing.T) {
		out, err := adaptGrokTaskVideoGenerationBody(
			[]byte(`{"model":"grok-video-3","prompt":"cat","seconds":"15","resolution":"480p"}`),
			"grok-video-3",
		)
		require.NoError(t, err)
		require.Equal(t, "15", gjson.GetBytes(out, "seconds").String())
		require.Equal(t, "480P", gjson.GetBytes(out, "resolution").String())
	})

	t.Run("grok video 3 accepts numeric duration", func(t *testing.T) {
		out, err := adaptGrokTaskVideoGenerationBody(
			[]byte(`{"model":"grok-video-3","prompt":"cat","duration":9,"size":"720P"}`),
			"grok-video-3",
		)
		require.NoError(t, err)
		require.Equal(t, "9", gjson.GetBytes(out, "seconds").String())
		require.Equal(t, "720P", gjson.GetBytes(out, "size").String())
		require.False(t, gjson.GetBytes(out, "resolution").Exists())
	})

	t.Run("fixed model derives seconds", func(t *testing.T) {
		out, err := adaptGrokTaskVideoGenerationBody(
			[]byte(`{"model":"grok-1.5-video-6s","prompt":"cat"}`),
			"grok-1.5-video-6s",
		)
		require.NoError(t, err)
		require.Equal(t, "6", gjson.GetBytes(out, "seconds").String())
	})

	t.Run("fixed model rejects conflicting duration", func(t *testing.T) {
		_, err := adaptGrokTaskVideoGenerationBody(
			[]byte(`{"model":"grok-1.5-video-15s","prompt":"cat","duration":10}`),
			"grok-1.5-video-15s",
		)
		require.ErrorContains(t, err, "requires seconds=15")
	})

	t.Run("rejects out of range seconds", func(t *testing.T) {
		_, err := adaptGrokTaskVideoGenerationBody(
			[]byte(`{"model":"grok-video-3","prompt":"cat","seconds":16}`),
			"grok-video-3",
		)
		require.ErrorContains(t, err, "between 1 and 15")
	})
}

func TestForwardGrokTaskVideoStatusUsesRawAuthAndRewritesTaskURLs(t *testing.T) {
	c, recorder := grokTaskVideosTestContext(http.MethodGet, "/v1/videos/task-1", nil)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"task-1",
			"status":"completed",
			"video_url":"https://storage.deepwl.cn/task-1.mp4",
			"metadata":{"url":"https://storage.deepwl.cn/task-1.mp4"}
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, grokTaskVideosTestAccount(),
		GrokMediaEndpointVideoStatus, "task-1", nil, "",
	)

	require.NoError(t, err)
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, "task-1", result.ResponseID)
	require.Equal(t, grokTaskVideosDefaultModel, result.BillingModel)
	require.Equal(t, "https://relay.example/v1/videos/task-1", upstream.lastReq.URL.String())
	require.Equal(t, "sk-task-upstream", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "/v1/videos/task-1/content", gjson.Get(recorder.Body.String(), "video_url").String())
	require.Equal(t, "/v1/videos/task-1/content", gjson.Get(recorder.Body.String(), "metadata.url").String())
}

func TestForwardGrokTaskVideoContentUsesAllowedSignedURLWithoutCredential(t *testing.T) {
	account := grokTaskVideosTestAccount()
	account.Credentials[credKeyHeaderOverrideEnabled] = true
	account.Credentials[credKeyHeaderOverrides] = map[string]any{"user-agent": "private-agent"}
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(`{"status":"completed","metadata":{"url":"https://storage.deepwl.cn/video/task-1.mp4?signature=abc"}}`),
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("video-payload")),
		},
	}}
	svc := &OpenAIGatewayService{
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:       true,
			UpstreamHosts: []string{"relay.example", "storage.deepwl.cn"},
		}}},
	}
	c, recorder := grokTaskVideosTestContext(http.MethodGet, "/v1/videos/task-1/content", nil)

	_, err := svc.ForwardGrokMedia(
		context.Background(), c, account,
		GrokMediaEndpointVideoContent, "task-1", nil, "",
	)

	require.NoError(t, err)
	require.Equal(t, "video-payload", recorder.Body.String())
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "sk-task-upstream", upstream.requests[0].Header.Get("Authorization"))
	require.Equal(t, "private-agent", upstream.requests[0].Header.Get("User-Agent"))
	require.Equal(t, "https://storage.deepwl.cn/video/task-1.mp4?signature=abc", upstream.requests[1].URL.String())
	require.Empty(t, upstream.requests[1].Header.Get("Authorization"))
	require.Empty(t, upstream.requests[1].Header.Get("User-Agent"))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[1].Context()))
}

func TestForwardGrokTaskVideoAuthenticatedContentUsesRawAuth(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(`{"status":"completed"}`),
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("video-payload")),
		},
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := grokTaskVideosTestContext(http.MethodGet, "/v1/videos/task-1/content", nil)

	_, err := svc.ForwardGrokMedia(
		context.Background(), c, grokTaskVideosTestAccount(),
		GrokMediaEndpointVideoContent, "task-1", nil, "",
	)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "sk-task-upstream", upstream.requests[0].Header.Get("Authorization"))
	require.Equal(t, "sk-task-upstream", upstream.requests[1].Header.Get("Authorization"))
	require.Equal(t, "https://relay.example/v1/videos/task-1/content", upstream.requests[1].URL.String())
}

func TestGrokTaskVideoSignedURLRequiresConfiguredHostWhenAllowlistEnabled(t *testing.T) {
	_, err := grokMediaSignedVideoContentURL(
		[]byte(`{"video_url":"https://attacker.invalid/video.mp4"}`),
		"task-1",
		grokTaskVideosTestAccount(),
		&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:       true,
			UpstreamHosts: []string{"relay.example", "storage.deepwl.cn"},
		}}},
	)

	require.ErrorContains(t, err, "unsupported video content URL")
}

func TestGrokTaskVideoStyleDoesNotChangeMutationEndpoints(t *testing.T) {
	body := []byte(`{"model":"grok-video-3","prompt":"cat"}`)
	c, _ := grokTaskVideosTestContext(http.MethodPost, "/v1/videos/edits", body)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"request_id":"edit-1"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}

	_, err := svc.ForwardGrokMedia(
		context.Background(), c, grokTaskVideosTestAccount(),
		GrokMediaEndpointVideosEdits, "", body, "application/json",
	)

	require.NoError(t, err)
	require.Equal(t, "https://relay.example/v1/videos/edits", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-task-upstream", upstream.lastReq.Header.Get("Authorization"))
	require.JSONEq(t, string(body), string(upstream.lastBody))
}

func TestGrokMediaAccountSupportsRequestMatchesProtocolAndMappedModel(t *testing.T) {
	taskAccount := grokTaskVideosTestAccount()
	nativeAccount := grokMediaContentTestAccount()

	eligible, reason := GrokMediaAccountSupportsRequest(
		taskAccount,
		GrokMediaEndpointVideosGenerations,
		"grok-video-3",
	)
	require.True(t, eligible)
	require.Equal(t, "task_videos_supported", reason)

	eligible, reason = GrokMediaAccountSupportsRequest(
		taskAccount,
		GrokMediaEndpointImagesGenerations,
		"grok-video-3",
	)
	require.False(t, eligible)
	require.Equal(t, "task_videos_endpoint_unsupported", reason)

	eligible, reason = GrokMediaAccountSupportsRequest(
		taskAccount,
		GrokMediaEndpointVideosGenerations,
		"grok-imagine-video",
	)
	require.True(t, eligible)
	require.Equal(t, "task_videos_supported", reason)

	taskAccount.Credentials["model_mapping"] = map[string]any{
		"public-video": "grok-1.5-video-10s",
	}
	eligible, _ = GrokMediaAccountSupportsRequest(
		taskAccount,
		GrokMediaEndpointVideosGenerations,
		"public-video",
	)
	require.True(t, eligible)

	eligible, reason = GrokMediaAccountSupportsRequest(
		nativeAccount,
		GrokMediaEndpointVideosGenerations,
		"grok-video-3",
	)
	require.False(t, eligible)
	require.Equal(t, "xai_task_video_model_unsupported", reason)

	nativeAccount.Credentials["model_mapping"] = map[string]any{
		"grok-video-3": "grok-imagine-video",
	}
	eligible, _ = GrokMediaAccountSupportsRequest(
		nativeAccount,
		GrokMediaEndpointVideosGenerations,
		"grok-video-3",
	)
	require.True(t, eligible)

	eligible, reason = GrokMediaAccountSupportsRequest(
		taskAccount,
		GrokMediaEndpointVideoStatus,
		"",
	)
	require.True(t, eligible)
	require.Equal(t, "video_lookup_bound_account", reason)
}
