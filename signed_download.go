package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
)

type DownloadBackend interface {
	VerifySession(context.Context, string) error
	PresignDownload(context.Context, string, string, int, string, string) (string, error)
}

type DownloadRequest struct {
	SessionID string `json:"session_id"`
	AssetType string `json:"asset_type"`
	AssetID   string `json:"asset_id"`
}

type DownloadGrant struct {
	URL            string `json:"url"`
	Method         string `json:"method"`
	ExpiresSeconds int    `json:"expires_seconds"`
}

type assetPolicy struct {
	prefix      string
	expires     int
	disposition string
}

var assetPolicies = map[string]assetPolicy{
	"player_asset":        {prefix: "players", expires: 300, disposition: "attachment"},
	"live_event_replay":   {prefix: "live-events", expires: 90, disposition: "attachment"},
	"moderation_evidence": {prefix: "moderation", expires: 60, disposition: "attachment"},
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type DownloadService struct {
	Backend DownloadBackend
	Bucket  string
}

func (s DownloadService) Grant(ctx context.Context, request DownloadRequest) (DownloadGrant, error) {
	if request.SessionID == "" || !safeID.MatchString(request.AssetID) {
		return DownloadGrant{}, ErrInvalidDownload
	}
	policy, ok := assetPolicies[request.AssetType]
	if !ok {
		return DownloadGrant{}, ErrInvalidDownload
	}
	if err := s.Backend.VerifySession(ctx, request.SessionID); err != nil {
		return DownloadGrant{}, err
	}

	objectKey := fmt.Sprintf("%s/%s", policy.prefix, request.AssetID)
	digest := sha256.Sum256([]byte(request.SessionID + "\x00" + objectKey))
	idempotencyKey := "download-" + hex.EncodeToString(digest[:16])
	url, err := s.Backend.PresignDownload(ctx, s.Bucket, objectKey, policy.expires, policy.disposition, idempotencyKey)
	if err != nil {
		return DownloadGrant{}, err
	}
	return DownloadGrant{URL: url, Method: "GET", ExpiresSeconds: policy.expires}, nil
}

var ErrInvalidDownload = errors.New("invalid download request")
