package media

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/models"
)

func TestCaptureImageVariantsLifecycle(t *testing.T) {
	for _, failUpload := range []bool{false, true} {
		name := "complete"
		if failUpload {
			name = "failed variant rolls back upload"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			objects := map[string][]byte{}
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Header.Get("AccessKey") != "test-key" {
					http.Error(w, "unauthorized", 401)
					return
				}
				switch r.Method {
				case http.MethodPut:
					if failUpload && strings.HasSuffix(r.URL.Path, "/popup.jpg") {
						http.Error(w, "failed", 500)
						return
					}
					objects[r.URL.Path], _ = io.ReadAll(r.Body)
				case http.MethodGet, http.MethodHead:
					data, ok := objects[r.URL.Path]
					if !ok {
						http.NotFound(w, r)
						return
					}
					w.Header().Set("Content-Type", "image/jpeg")
					_, _ = w.Write(data)
				case http.MethodDelete:
					delete(objects, r.URL.Path)
				default:
					http.Error(w, "unexpected method", 405)
				}
			}))
			t.Cleanup(origin.Close)
			storage := &BunnyStorage{
				host: strings.TrimPrefix(origin.URL, "https://"), httpClient: origin.Client(),
				privateZone: "photos", privateKey: "test-key", publicZone: "photos", publicKey: "test-key",
				publicBaseURL: "https://photos.example.test", imageVariantsEnabled: true,
			}
			var encoded bytes.Buffer
			if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 600, 1000)), nil); err != nil {
				t.Fatal(err)
			}
			asset := &NormalizedAsset{Bytes: encoded.Bytes(), ContentType: "image/jpeg", Extension: ".jpg", Width: 600, Height: 1000}
			key, err := storage.StorePrivateCapture(context.Background(), 1, "sample", time.Now(), asset)
			if failUpload {
				if err == nil {
					t.Fatal("expected failed upload")
				}
				mu.Lock()
				defer mu.Unlock()
				if len(objects) != 0 {
					t.Fatalf("failed upload left %d objects", len(objects))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			if !bytes.Equal(objects["/photos/"+key], asset.Bytes) {
				t.Error("original changed")
			}
			for _, profile := range captureImageProfiles {
				data := objects["/photos/"+captureImageVariantKey(key, profile.name)]
				cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
				if err != nil {
					t.Errorf("invalid %s: %v", profile.name, err)
					continue
				}
				if cfg.Width > profile.maxDimension || cfg.Height > profile.maxDimension || cfg.Width > 600 || cfg.Height > 1000 {
					t.Errorf("wrong dimensions: %+v", cfg)
				}
			}
			mu.Unlock()
			capture := &models.Capture{PrivateStorageKey: key, PublicStorageKey: key, Status: "published"}
			urls := storage.CaptureImageVariantURLs(capture)
			if len(urls) != 4 || strings.Contains(urls["thumb"], "?") {
				t.Fatalf("invalid static image URLs: %v", urls)
			}
			if err := storage.BackfillCaptureImageVariants(context.Background(), capture); err != nil {
				t.Fatal(err)
			}
			if err := storage.DeletePrivate(context.Background(), key); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(objects) != 0 {
				t.Fatalf("delete left %d objects", len(objects))
			}
		})
	}
}
