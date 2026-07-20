package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

func TestDeleteImageRefusesWhileInUse(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	imagePath := filepath.Join(imageRoot(), "img-a")
	if err := os.MkdirAll(imagePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	image, err := db.CreateImage(ctx, domain.CreateImageRequest{Name: "img-a", Path: imagePath})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if _, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{
		Name:    "dev-1",
		ImageID: image.ID,
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}

	if _, err := db.DeleteImage(ctx, image.ID); !errors.Is(err, ErrImageInUse) {
		t.Fatalf("delete of an in-use image should fail with ErrImageInUse, got %v", err)
	}
	// The image must survive the refused delete.
	if _, err := db.GetImage(ctx, image.ID); err != nil {
		t.Fatalf("image was removed despite the refusal: %v", err)
	}
	// And so must its files — the caller keys off the returned path.
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("image files removed despite the refusal: %v", err)
	}
}

func TestDeleteImageReturnsPathWhenUnreferenced(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	imagePath := filepath.Join(imageRoot(), "img-b")
	if err := os.MkdirAll(imagePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	image, err := db.CreateImage(ctx, domain.CreateImageRequest{Name: "img-b", Path: imagePath})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}

	got, err := db.DeleteImage(ctx, image.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got != imagePath {
		t.Fatalf("returned path = %q, want %q", got, imagePath)
	}
	if _, err := db.GetImage(ctx, image.ID); err == nil {
		t.Fatal("image row still present after delete")
	}
}

func TestCountInstancesUsingImage(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	imagePath := filepath.Join(imageRoot(), "img-c")
	if err := os.MkdirAll(imagePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	image, err := db.CreateImage(ctx, domain.CreateImageRequest{Name: "img-c", Path: imagePath})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}

	if n, err := db.CountInstancesUsingImage(ctx, image.ID); err != nil || n != 0 {
		t.Fatalf("count = %d err=%v, want 0", n, err)
	}
	for _, name := range []string{"dev-1", "dev-2"} {
		if _, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{Name: name, ImageID: image.ID}); err != nil {
			t.Fatalf("create instance %s: %v", name, err)
		}
	}
	if n, err := db.CountInstancesUsingImage(ctx, image.ID); err != nil || n != 2 {
		t.Fatalf("count = %d err=%v, want 2", n, err)
	}
}
