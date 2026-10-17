package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type recordedBackend struct {
	verifyErr    error
	verified     string
	presignCalls int
	key          string
	expires      int
	disposition  string
	idempotency  string
}

func (b *recordedBackend) VerifySession(_ context.Context, sessionID string) error {
	b.verified = sessionID
	return b.verifyErr
}

func (b *recordedBackend) PresignDownload(_ context.Context, bucket, key string, expires int, disposition, idempotency string) (string, error) {
	b.presignCalls++
	b.key = key
	b.expires = expires
	b.disposition = disposition
	b.idempotency = idempotency
	return fmt.Sprintf("https://downloads.example/%s/%s", bucket, key), nil
}

func TestGrantAppliesAssetPolicyAfterSessionVerification(t *testing.T) {
	tests := []struct {
		name      string
		assetType string
		wantKey   string
		wantTTL   int
	}{
		{name: "player generated asset", assetType: "player_asset", wantKey: "players/avatar-42.png", wantTTL: 300},
		{name: "live event replay", assetType: "live_event_replay", wantKey: "live-events/avatar-42.png", wantTTL: 90},
		{name: "moderation queue evidence", assetType: "moderation_evidence", wantKey: "moderation/avatar-42.png", wantTTL: 60},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &recordedBackend{}
			service := DownloadService{Backend: backend, Bucket: "game-files"}
			grant, err := service.Grant(context.Background(), DownloadRequest{
				SessionID: "session-7",
				AssetType: test.assetType,
				AssetID:   "avatar-42.png",
			})
			if err != nil {
				t.Fatalf("Grant() error = %v", err)
			}
			if backend.verified != "session-7" || backend.presignCalls != 1 {
				t.Fatalf("verification = %q, presign calls = %d", backend.verified, backend.presignCalls)
			}
			if backend.key != test.wantKey || backend.expires != test.wantTTL {
				t.Fatalf("policy key/ttl = %q/%d, want %q/%d", backend.key, backend.expires, test.wantKey, test.wantTTL)
			}
			if backend.disposition != "attachment" || backend.idempotency == "" {
				t.Fatalf("disposition/idempotency = %q/%q", backend.disposition, backend.idempotency)
			}
			if grant.Method != "GET" || grant.ExpiresSeconds != test.wantTTL {
				t.Fatalf("grant = %#v", grant)
			}
		})
	}
}

func TestGrantDoesNotPresignWhenSessionIsRejected(t *testing.T) {
	backend := &recordedBackend{verifyErr: errors.New("session rejected")}
	service := DownloadService{Backend: backend, Bucket: "game-files"}
	_, err := service.Grant(context.Background(), DownloadRequest{
		SessionID: "session-7",
		AssetType: "player_asset",
		AssetID:   "avatar-42.png",
	})
	if err == nil {
		t.Fatal("Grant() error = nil")
	}
	if backend.presignCalls != 0 {
		t.Fatalf("presign calls = %d, want 0", backend.presignCalls)
	}
}
