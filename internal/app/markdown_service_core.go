package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"time"
)

type MarkdownCachedImageResult struct {
	Hit         bool   `json:"hit"`
	ContentType string `json:"content_type,omitempty"`
	Data        string `json:"data,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	DecodedSize int64  `json:"decoded_size,omitempty"`
}

func (a *App) InitMarkdownImages(dataDir string) error {
	cache, err := newMarkdownImageCache(
		filepath.Join(dataDir, "markdown_image_cache"),
		markdownImageCacheMaxBytes,
		time.Now,
	)
	if err != nil {
		return err
	}
	a.markdownCache = cache
	a.markdownLoader = newMarkdownRemoteImageLoader(cache, func(progress MarkdownImageProgress) {
		a.emitEvent("markdown:image-progress", progress)
	}, nil, false)
	return nil
}

func (a *App) LoadRemoteMarkdownImage(requestID, rawURL string) (MarkdownRemoteImageResult, error) {
	if a.markdownLoader == nil {
		return MarkdownRemoteImageResult{}, fmt.Errorf("Markdown image loader is not initialized")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.markdownLoader.Load(ctx, requestID, rawURL)
}

func (a *App) CancelRemoteMarkdownImage(requestID string) bool {
	return a.markdownLoader != nil && a.markdownLoader.Cancel(requestID)
}

func (a *App) GetCachedMarkdownImage(rawURL string) (MarkdownCachedImageResult, error) {
	if a.markdownCache == nil {
		return MarkdownCachedImageResult{}, fmt.Errorf("Markdown image cache is not initialized")
	}
	entry, hit, err := a.markdownCache.Get(rawURL)
	if err != nil || !hit {
		return MarkdownCachedImageResult{Hit: false}, err
	}
	contentType, width, height, decodedSize, err := validateMarkdownImage(entry.Data, entry.ContentType)
	if err != nil {
		return MarkdownCachedImageResult{Hit: false}, nil
	}
	return MarkdownCachedImageResult{
		Hit: true, ContentType: contentType, Data: base64.StdEncoding.EncodeToString(entry.Data),
		Size: int64(len(entry.Data)), Width: width, Height: height, DecodedSize: decodedSize,
	}, nil
}

func (a *App) ValidateEmbeddedMarkdownImage(dataBase64, contentType string) (MarkdownImageData, error) {
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return MarkdownImageData{}, fmt.Errorf("decode embedded image: %w", err)
	}
	validatedType, width, height, decodedSize, err := validateMarkdownImage(data, contentType)
	if err != nil {
		return MarkdownImageData{}, err
	}
	return MarkdownImageData{
		ContentType: validatedType, Data: dataBase64, Size: int64(len(data)), Width: width, Height: height, DecodedSize: decodedSize,
	}, nil
}

func (a *App) GetMarkdownImageCacheStats() (MarkdownImageCacheStats, error) {
	if a.markdownCache == nil {
		return MarkdownImageCacheStats{}, fmt.Errorf("Markdown image cache is not initialized")
	}
	return a.markdownCache.Stats()
}

func (a *App) ClearMarkdownImageCache() error {
	if a.markdownLoader == nil {
		return fmt.Errorf("Markdown image loader is not initialized")
	}
	if err := a.markdownLoader.ClearCache(); err != nil {
		return err
	}
	a.emitEvent("markdown:image-cache-cleared")
	return nil
}
