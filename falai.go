package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FalAI model endpoints
const (
	FalColorize       = "fal-ai/ddcolor"
	FalClarityUpscale = "fal-ai/clarity-upscaler"
	FalESRGAN         = "fal-ai/esrgan"
	FalCreativeUp     = "fal-ai/creative-upscaler"
	FalRestore        = "fal-ai/image-apps-v2/photo-restoration"
	FalCodeFormer     = "fal-ai/codeformer"
	FalFlux2Edit      = "fal-ai/flux-2/turbo/edit"
	FalFlux2ProEdit   = "fal-ai/flux-2-pro/edit"
	FalFlux1DevEdit   = "fal-ai/flux/dev/image-to-image"
	FalVectorize      = "fal-ai/recraft/vectorize"
)

// FalClient handles fal.ai API communication
type FalClient struct {
	apiKey     string
	httpClient *http.Client
}

// NewFalClient creates a new fal.ai client
func NewFalClient(apiKey string) *FalClient {
	return &FalClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// FalImage represents an image in the response
type FalImage struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FileSize    int    `json:"file_size"`
}

// FalResponse is the common response structure
type FalResponse struct {
	Image   *FalImage  `json:"image,omitempty"`
	Images  []FalImage `json:"images,omitempty"`
	Message string     `json:"msg,omitempty"` // Error message from content checker or other issues
}

// ColorizeRequest for DDColor
type ColorizeRequest struct {
	ImageURL string `json:"image_url"`
}

// ClarityUpscaleRequest for Clarity Upscaler
type ClarityUpscaleRequest struct {
	ImageURL            string `json:"image_url"`
	Prompt              string `json:"prompt,omitempty"`
	NegativePrompt      string `json:"negative_prompt,omitempty"`
	EnableSafetyChecker bool   `json:"enable_safety_checker"`
}

// ESRGANRequest for ESRGAN upscaling
type ESRGANRequest struct {
	ImageURL string  `json:"image_url"`
	Scale    float64 `json:"scale,omitempty"`
	Model    string  `json:"model,omitempty"`
}

// RestoreRequest for Photo Restoration
type RestoreRequest struct {
	ImageURL            string `json:"image_url"`
	EnhanceResolution   bool   `json:"enhance_resolution"`
	FixColors           bool   `json:"fix_colors"`
	RemoveScratches     bool   `json:"remove_scratches"`
	EnableSafetyChecker bool   `json:"enable_safety_checker"`
}

// CodeFormerRequest for CodeFormer
type CodeFormerRequest struct {
	ImageURL            string `json:"image_url"`
	EnableSafetyChecker bool   `json:"enable_safety_checker"`
}

// Flux2EditRequest for FLUX.2 editing
type Flux2EditRequest struct {
	ImageURLs           []string `json:"image_urls"`
	Prompt              string   `json:"prompt"`
	GuidanceScale       float64  `json:"guidance_scale,omitempty"`
	EnableSafetyChecker bool     `json:"enable_safety_checker"`
	SafetyTolerance     int      `json:"safety_tolerance,omitempty"` // 1 (strict) to 6 (permissive)
}

// Flux1EditRequest for FLUX.1 editing
type Flux1EditRequest struct {
	ImageURL            string  `json:"image_url"`
	Prompt              string  `json:"prompt"`
	Strength            float64 `json:"strength,omitempty"`
	NumInferenceSteps   int     `json:"num_inference_steps,omitempty"`
	GuidanceScale       float64 `json:"guidance_scale,omitempty"`
	EnableSafetyChecker bool    `json:"enable_safety_checker"`
	SafetyTolerance     int     `json:"safety_tolerance,omitempty"` // 1 (strict) to 6 (permissive)
}

// VectorizeRequest for Recraft Vectorize
type VectorizeRequest struct {
	ImageURL string `json:"image_url"`
}

// callAPI makes a request to fal.ai (uses background context)
func (c *FalClient) callAPI(model string, payload interface{}) (*FalResponse, error) {
	return c.callAPIWithContext(context.Background(), model, payload)
}

// callAPIWithContext makes a request to fal.ai with cancellation support
func (c *FalClient) callAPIWithContext(ctx context.Context, model string, payload interface{}) (*FalResponse, error) {
	url := fmt.Sprintf("https://fal.run/%s", model)

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Key "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("request cancelled")
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result FalResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for error message in response (e.g., content checker)
	if result.Message != "" {
		return nil, fmt.Errorf("%s", result.Message)
	}

	return &result, nil
}

// Colorize colorizes a black & white image
func (c *FalClient) Colorize(imageDataURI string) (*FalImage, error) {
	return c.ColorizeWithContext(context.Background(), imageDataURI)
}

// ColorizeWithContext colorizes a black & white image with cancellation support
func (c *FalClient) ColorizeWithContext(ctx context.Context, imageDataURI string) (*FalImage, error) {
	req := ColorizeRequest{ImageURL: imageDataURI}
	resp, err := c.callAPIWithContext(ctx, FalColorize, req)
	if err != nil {
		return nil, err
	}
	return resp.Image, nil
}

// Upscale increases image resolution using the specified model
func (c *FalClient) Upscale(imageDataURI string, model string) (*FalImage, error) {
	return c.UpscaleWithContext(context.Background(), imageDataURI, model)
}

// UpscaleWithContext increases image resolution with cancellation support
func (c *FalClient) UpscaleWithContext(ctx context.Context, imageDataURI string, model string) (*FalImage, error) {
	var resp *FalResponse
	var err error

	switch model {
	case FalClarityUpscale, "":
		req := ClarityUpscaleRequest{
			ImageURL:       imageDataURI,
			Prompt:         "masterpiece, best quality, highres",
			NegativePrompt: "(worst quality, low quality, normal quality:2)",
		}
		resp, err = c.callAPIWithContext(ctx, FalClarityUpscale, req)
	case FalESRGAN:
		req := ESRGANRequest{
			ImageURL: imageDataURI,
			Scale:    4,
			Model:    "RealESRGAN_x4plus",
		}
		resp, err = c.callAPIWithContext(ctx, FalESRGAN, req)
	case FalCreativeUp:
		req := ClarityUpscaleRequest{
			ImageURL: imageDataURI,
		}
		resp, err = c.callAPIWithContext(ctx, FalCreativeUp, req)
	default:
		return nil, fmt.Errorf("unknown upscale model: %s", model)
	}

	if err != nil {
		return nil, err
	}
	return resp.Image, nil
}

// Restore cleans and enhances degraded images
func (c *FalClient) Restore(imageDataURI string, model string, fixColors, removeScratches bool) (*FalImage, error) {
	return c.RestoreWithContext(context.Background(), imageDataURI, model, fixColors, removeScratches)
}

// RestoreWithContext cleans and enhances degraded images with cancellation support
func (c *FalClient) RestoreWithContext(ctx context.Context, imageDataURI string, model string, fixColors, removeScratches bool) (*FalImage, error) {
	var resp *FalResponse
	var err error

	switch model {
	case FalRestore, "":
		req := RestoreRequest{
			ImageURL:          imageDataURI,
			EnhanceResolution: true,
			FixColors:         fixColors,
			RemoveScratches:   removeScratches,
		}
		resp, err = c.callAPIWithContext(ctx, FalRestore, req)
	case FalCodeFormer:
		req := CodeFormerRequest{
			ImageURL: imageDataURI,
		}
		resp, err = c.callAPIWithContext(ctx, FalCodeFormer, req)
	default:
		return nil, fmt.Errorf("unknown restore model: %s", model)
	}

	if err != nil {
		return nil, err
	}

	if len(resp.Images) > 0 {
		return &resp.Images[0], nil
	}
	return resp.Image, nil
}

// Edit modifies image based on text prompt
func (c *FalClient) Edit(imageDataURI string, model string, prompt string, strength float64) (*FalImage, error) {
	return c.EditWithContext(context.Background(), imageDataURI, model, prompt, strength)
}

// EditWithContext modifies image based on text prompt with cancellation support
func (c *FalClient) EditWithContext(ctx context.Context, imageDataURI string, model string, prompt string, strength float64) (*FalImage, error) {
	var resp *FalResponse
	var err error

	switch model {
	case FalFlux2Edit, "":
		req := Flux2EditRequest{
			ImageURLs:       []string{imageDataURI},
			Prompt:          prompt,
			GuidanceScale:   2.5,
			SafetyTolerance: 6, // Most permissive
		}
		resp, err = c.callAPIWithContext(ctx, FalFlux2Edit, req)
	case FalFlux2ProEdit:
		req := Flux2EditRequest{
			ImageURLs:       []string{imageDataURI},
			Prompt:          prompt,
			GuidanceScale:   2.5,
			SafetyTolerance: 6, // Most permissive
		}
		resp, err = c.callAPIWithContext(ctx, FalFlux2ProEdit, req)
	case FalFlux1DevEdit:
		if strength == 0 {
			strength = 0.75
		}
		req := Flux1EditRequest{
			ImageURL:          imageDataURI,
			Prompt:            prompt,
			Strength:          strength,
			NumInferenceSteps: 40,
			GuidanceScale:     3.5,
			SafetyTolerance:   6, // Most permissive
		}
		resp, err = c.callAPIWithContext(ctx, FalFlux1DevEdit, req)
	default:
		return nil, fmt.Errorf("unknown edit model: %s", model)
	}

	if err != nil {
		return nil, err
	}

	if len(resp.Images) > 0 {
		return &resp.Images[0], nil
	}
	return resp.Image, nil
}

// Vectorize converts an image to SVG vector format
func (c *FalClient) Vectorize(imageDataURI string) (*FalImage, error) {
	return c.VectorizeWithContext(context.Background(), imageDataURI)
}

// VectorizeWithContext converts an image to SVG vector format with cancellation support
func (c *FalClient) VectorizeWithContext(ctx context.Context, imageDataURI string) (*FalImage, error) {
	req := VectorizeRequest{ImageURL: imageDataURI}
	resp, err := c.callAPIWithContext(ctx, FalVectorize, req)
	if err != nil {
		return nil, err
	}
	return resp.Image, nil
}

// DownloadImage fetches an image from URL and returns the bytes and content type
func (c *FalClient) DownloadImage(url string) ([]byte, string, error) {
	return c.DownloadImageWithContext(context.Background(), url)
}

// DownloadImageWithContext fetches an image from URL with cancellation support
func (c *FalClient) DownloadImageWithContext(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return nil, "", fmt.Errorf("download cancelled")
		}
		return nil, "", fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read image: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}

	return data, contentType, nil
}
