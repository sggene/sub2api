//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountGrokVideoUpstreamStyle(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    string
		uses    bool
	}{
		{
			name: "nil account uses xai",
			want: GrokVideoUpstreamStyleXAI,
		},
		{
			name:    "legacy grok api key without credentials uses xai",
			account: &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey},
			want:    GrokVideoUpstreamStyleXAI,
		},
		{
			name: "legacy grok api key without style uses xai",
			account: &Account{
				Platform:    PlatformGrok,
				Type:        AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "sk-test"},
			},
			want: GrokVideoUpstreamStyleXAI,
		},
		{
			name: "grok oauth cannot enable task videos",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					GrokVideoUpstreamStyleCredentialKey: GrokVideoUpstreamStyleTaskVideos,
				},
			},
			want: GrokVideoUpstreamStyleXAI,
		},
		{
			name: "grok api key enables task videos",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					GrokVideoUpstreamStyleCredentialKey: GrokVideoUpstreamStyleTaskVideos,
				},
			},
			want: GrokVideoUpstreamStyleTaskVideos,
			uses: true,
		},
		{
			name: "explicit xai remains xai",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					GrokVideoUpstreamStyleCredentialKey: GrokVideoUpstreamStyleXAI,
				},
			},
			want: GrokVideoUpstreamStyleXAI,
		},
		{
			name: "unknown style falls back to xai",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					GrokVideoUpstreamStyleCredentialKey: "unknown",
				},
			},
			want: GrokVideoUpstreamStyleXAI,
		},
		{
			name: "malformed style falls back to xai",
			account: &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					GrokVideoUpstreamStyleCredentialKey: true,
				},
			},
			want: GrokVideoUpstreamStyleXAI,
		},
		{
			name: "non grok api key cannot enable task videos",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					GrokVideoUpstreamStyleCredentialKey: GrokVideoUpstreamStyleTaskVideos,
				},
			},
			want: GrokVideoUpstreamStyleXAI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.account.GrokVideoUpstreamStyle())
			require.Equal(t, tt.uses, tt.account.UsesTaskVideosUpstream())
		})
	}
}

func TestTaskVideosAccountOnlyAdvertisesMediaCapability(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			GrokVideoUpstreamStyleCredentialKey: GrokVideoUpstreamStyleTaskVideos,
		},
	}

	require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGrokMediaGeneration))
}
