package service

import "strings"

const (
	GrokVideoUpstreamStyleCredentialKey = "grok_video_upstream_style"
	GrokVideoUpstreamStyleXAI           = "xai"
	GrokVideoUpstreamStyleTaskVideos    = "task_videos"
)

// GrokVideoUpstreamStyle returns the account-level Grok video protocol.
// Only Grok API-key accounts may opt into the task_videos protocol; every
// other account or malformed value keeps the existing xAI behavior.
func (a *Account) GrokVideoUpstreamStyle() string {
	if a == nil || !a.IsGrok() || a.Type != AccountTypeAPIKey {
		return GrokVideoUpstreamStyleXAI
	}

	if strings.TrimSpace(a.GetCredential(GrokVideoUpstreamStyleCredentialKey)) == GrokVideoUpstreamStyleTaskVideos {
		return GrokVideoUpstreamStyleTaskVideos
	}
	return GrokVideoUpstreamStyleXAI
}

func (a *Account) UsesTaskVideosUpstream() bool {
	return a.GrokVideoUpstreamStyle() == GrokVideoUpstreamStyleTaskVideos
}
