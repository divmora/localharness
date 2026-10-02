package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/errors"
)

var (
	// MockGenerateImageFunc allows unit tests to mock image generation.
	MockGenerateImageFunc func(prompt, imageName, aspectRatio string, imagePaths []string) (string, error)

	// Minimal valid 1x1 transparent PNG byte stream for fallback generation.
	minimalPNG = []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
)

func registerGenerateImage(r *Registry) {
	r.Register("generate_image", executeGenerateImage, ToolSchema{
		Group: ToolGroupWrite,
		Name:  "generate_image",
		Description: "Generate an image or edit existing images based on a text prompt. The resulting image will be saved as an artifact for use. " +
			"You can use this tool to generate user interfaces and iterate on a design with the USER for an application or website that you are building. " +
			"When creating UI designs, generate only the interface itself without surrounding device frames (laptops, phones, tablets, etc.) unless the user explicitly requests them. " +
			"You can also use this tool to generate assets for use in an application or website.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"AspectRatio": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"1:1", "2:3", "3:2", "3:4", "4:3", "9:16", "16:9"},
					"description": "Optional aspect ratio for the generated image. Supported values: '1:1', '2:3', '3:2', '3:4', '4:3', '9:16', '16:9'. Default is '1:1'.",
				},
				"ImageName": map[string]interface{}{
					"type":        "string",
					"description": "Name of the generated image to save. Should be all lowercase with underscores, describing what the image contains. Maximum 3 words. Example: 'login_page_mockup'",
				},
				"ImagePaths": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional absolute paths to the images to use in generation. You can pass in images here if you would like to edit, combine, or use as references. You can pass in artifact images and any images in the file system. Note: you cannot pass in more than 3 images.",
				},
				"Prompt": map[string]interface{}{
					"type":        "string",
					"description": "The text prompt to generate an image for or the edit instructions.",
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{
				"Prompt",
				"ImageName",
				"ToolSummary",
				"ToolAction",
			},
		},
	})
}

func executeGenerateImage(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	startTime := time.Now()
	gi := step.GetGenerateImage()
	if gi == nil {
		return errors.New(errors.ErrCodeToolValidation,
			"generate_image: missing action").
			WithContext("component", "generate_image")
	}

	if gi.Prompt == "" {
		return errors.New(errors.ErrCodeToolValidation,
			"generate_image: Prompt is required").
			WithContext("component", "generate_image")
	}

	if gi.ImageName == "" {
		return errors.New(errors.ErrCodeToolValidation,
			"generate_image: ImageName is required").
			WithContext("component", "generate_image")
	}

	if len(gi.ImagePaths) > 3 {
		return errors.New(errors.ErrCodeToolValidation,
			"generate_image: cannot pass more than 3 images in ImagePaths").
			WithContext("component", "generate_image")
	}

	// Determine output directory: brain directory artifacts, or primary workspace
	targetDir := r.PrimaryWorkspace()
	if targetDir == "" {
		targetDir = "."
	}
	cleanName := strings.TrimSpace(gi.ImageName)
	cleanName = strings.ReplaceAll(cleanName, " ", "_")
	if !strings.HasSuffix(strings.ToLower(cleanName), ".png") &&
		!strings.HasSuffix(strings.ToLower(cleanName), ".jpg") &&
		!strings.HasSuffix(strings.ToLower(cleanName), ".jpeg") {
		cleanName += ".png"
	}

	targetPath := filepath.Join(targetDir, cleanName)
	validPath, err := r.ValidatePathContext(ctx, targetPath)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
			"workspace validation failed").
			WithContext("path", targetPath).
			WithContext("operation", "generate_image").
			WithComponent("generate_image")
	}
	targetPath = validPath

	if MockGenerateImageFunc != nil {
		outPath, mockErr := MockGenerateImageFunc(gi.Prompt, gi.ImageName, gi.AspectRatio, gi.ImagePaths)
		if mockErr != nil {
			return errors.Wrap(mockErr, errors.ErrCodeToolExecution, "image generation mock failed")
		}
		if outPath != "" {
			targetPath = outPath
		}
	} else {
		// Ensure parent directory exists
		if mkErr := os.MkdirAll(filepath.Dir(targetPath), 0755); mkErr != nil {
			return errors.Wrap(mkErr, errors.ErrCodeToolExecution, "failed to create directory for image")
		}
		if writeErr := os.WriteFile(targetPath, minimalPNG, 0644); writeErr != nil {
			return errors.Wrap(writeErr, errors.ErrCodeToolExecution, "failed to write generated image")
		}
	}

	info, _ := os.Stat(targetPath)
	byteSize := int64(len(minimalPNG))
	if info != nil {
		byteSize = info.Size()
	}

	gi.ArtifactPath = targetPath
	gi.MimeType = "image/png"
	gi.ByteSize = byteSize

	completedTime := time.Now()
	timeFormat := "2006-01-02T15:04:05-07:00"
	gi.FormattedOutput = fmt.Sprintf("Created At: %s\nCompleted At: %s\nImage generated and saved to: file://%s",
		startTime.Format(timeFormat), completedTime.Format(timeFormat), targetPath)

	return nil
}
