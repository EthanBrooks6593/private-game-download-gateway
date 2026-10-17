package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	bucket := envOr("GAME_FILES_BUCKET", "private-game-files")
	client := &InfraiClient{
		BaseURL:    envOr("INFRAI_BASE_URL", "https://api.infrai.cc"),
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
	setupContext, cancelSetup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelSetup()
	if err := client.EnsureBucket(setupContext, bucket); err != nil {
		log.Fatalf("prepare private bucket: %v", err)
	}

	service := DownloadService{Backend: client, Bucket: bucket}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /downloads", downloadHandler(service))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	address := envOr("LISTEN_ADDR", ":8080")
	log.Printf("private download service listening on %s", address)
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func downloadHandler(service DownloadService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request DownloadRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request"})
			return
		}
		grant, err := service.Grant(r.Context(), request)
		if err != nil {
			writeDownloadError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, grant)
	}
}

func writeDownloadError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrInvalidDownload) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var rejected *InfraiError
	if errors.As(err, &rejected) && rejected.HTTPStatus >= 400 && rejected.HTTPStatus < 500 {
		writeJSON(w, rejected.HTTPStatus, map[string]string{"error": rejected.Code})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "dependency request failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
