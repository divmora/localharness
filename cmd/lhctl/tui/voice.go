package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// VoiceManager handles audio recording (dictation), speech-to-text transcription,
// and text-to-speech response playback.
type VoiceManager struct {
	mu             sync.Mutex
	isRecording    bool
	recordingStart time.Time
	recordCmd      *exec.Cmd
	recordFile     string
	isSpeaking     bool
	speakCmd       *exec.Cmd
	autoSpeak      bool
	openAIKey      string
	openAIBaseURL  string
}

// NewVoiceManager creates a new voice manager.
func NewVoiceManager() *VoiceManager {
	vm := &VoiceManager{
		openAIKey:     os.Getenv("OPENAI_API_KEY"),
		openAIBaseURL: "https://api.openai.com/v1",
	}
	if groqKey := os.Getenv("GROQ_API_KEY"); groqKey != "" && vm.openAIKey == "" {
		vm.openAIKey = groqKey
		vm.openAIBaseURL = "https://api.groq.com/openai/v1"
	}

	// Check if global litellm.json is configured
	if home, err := os.UserHomeDir(); err == nil {
		litellmPath := filepath.Join(home, ".divmora", "config", "litellm.json")
		if data, err := os.ReadFile(litellmPath); err == nil {
			var cfg struct {
				DefaultEndpoint string `json:"defaultEndpoint"`
				Endpoints       map[string]struct {
					BaseURL string `json:"baseUrl"`
					APIKey  string `json:"apiKey"`
				} `json:"endpoints"`
			}
			if err := json.Unmarshal(data, &cfg); err == nil {
				if ep, ok := cfg.Endpoints[cfg.DefaultEndpoint]; ok && ep.APIKey != "" {
					if vm.openAIKey == "" {
						vm.openAIKey = ep.APIKey
						vm.openAIBaseURL = strings.TrimRight(ep.BaseURL, "/") + "/v1"
					}
				}
			}
		}
	}

	return vm
}

// IsRecording returns true if audio recording is in progress.
func (vm *VoiceManager) IsRecording() bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.isRecording
}

// RecordingDuration returns the elapsed duration since recording began.
func (vm *VoiceManager) RecordingDuration() time.Duration {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if !vm.isRecording {
		return 0
	}
	return time.Since(vm.recordingStart)
}

// IsSpeaking returns true if text-to-speech audio is currently playing.
func (vm *VoiceManager) IsSpeaking() bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.isSpeaking
}

// AutoSpeakEnabled returns true if responses should automatically be spoken aloud.
func (vm *VoiceManager) AutoSpeakEnabled() bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.autoSpeak
}

// SetAutoSpeak sets the auto-speech preference.
func (vm *VoiceManager) SetAutoSpeak(enabled bool) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.autoSpeak = enabled
}

// ToggleAutoSpeak toggles the auto-speech preference and returns the new state.
func (vm *VoiceManager) ToggleAutoSpeak() bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.autoSpeak = !vm.autoSpeak
	return vm.autoSpeak
}

// StartRecording begins capturing microphone audio.
func (vm *VoiceManager) StartRecording() error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.isRecording {
		return nil
	}

	// Stop any active speech output
	if vm.speakCmd != nil && vm.speakCmd.Process != nil {
		_ = vm.speakCmd.Process.Kill()
		vm.isSpeaking = false
		vm.speakCmd = nil
	}

	tempDir := os.TempDir()
	now := time.Now().UnixNano()

	switch runtime.GOOS {
	case "darwin":
		recFile := filepath.Join(tempDir, fmt.Sprintf("lh_voice_%d.m4a", now))
		cmd, err := vm.startDarwinRecording(recFile)
		if err != nil {
			return err
		}
		vm.recordCmd = cmd
		vm.recordFile = recFile
		vm.isRecording = true
		vm.recordingStart = time.Now()
		return nil

	default:
		// Linux / other: check for rec (sox), arecord, or ffmpeg
		recFile := filepath.Join(tempDir, fmt.Sprintf("lh_voice_%d.wav", now))
		var cmd *exec.Cmd
		if _, err := exec.LookPath("rec"); err == nil {
			cmd = exec.Command("rec", "-q", recFile, "rate", "16k", "channels", "1")
		} else if _, err := exec.LookPath("arecord"); err == nil {
			cmd = exec.Command("arecord", "-q", "-f", "cd", "-t", "wav", recFile)
		} else if _, err := exec.LookPath("ffmpeg"); err == nil {
			cmd = exec.Command("ffmpeg", "-y", "-f", "alsa", "-i", "default", "-ar", "16000", "-ac", "1", recFile)
		} else {
			return fmt.Errorf("no audio recording utility found (install 'sox', 'arecord', or 'ffmpeg')")
		}

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting audio recording: %w", err)
		}
		vm.recordCmd = cmd
		vm.recordFile = recFile
		vm.isRecording = true
		vm.recordingStart = time.Now()
		return nil
	}
}

// startDarwinRecording starts recording audio on macOS using native AVFoundation or command tools.
func (vm *VoiceManager) startDarwinRecording(recFile string) (*exec.Cmd, error) {
	// 1. Check for cached native Swift recorder binary
	binDir := getVoiceBinDir()
	recBin := filepath.Join(binDir, "lh-rec")
	if _, err := os.Stat(recBin); err == nil {
		cmd := exec.Command(recBin, recFile)
		if err := cmd.Start(); err == nil {
			return cmd, nil
		}
	}

	// 2. Check if swiftc is available to compile helper once
	if swiftc, err := exec.LookPath("swiftc"); err == nil {
		_ = os.MkdirAll(binDir, 0755)
		swiftSrc := `
import AVFoundation
import Foundation

guard CommandLine.arguments.count > 1 else { exit(1) }
let url = URL(fileURLWithPath: CommandLine.arguments[1])
let settings: [String: Any] = [
    AVFormatIDKey: Int(kAudioFormatMPEG4AAC),
    AVSampleRateKey: 16000.0,
    AVNumberOfChannelsKey: 1,
    AVEncoderAudioQualityKey: AVAudioQuality.high.rawValue
]
guard let recorder = try? AVAudioRecorder(url: url, settings: settings) else { exit(1) }
recorder.prepareToRecord()
guard recorder.record() else { exit(1) }
signal(SIGINT) { _ in exit(0) }
signal(SIGTERM) { _ in exit(0) }
RunLoop.main.run()
`
		tmpSwift := filepath.Join(binDir, "rec.swift")
		if err := os.WriteFile(tmpSwift, []byte(swiftSrc), 0644); err == nil {
			buildCmd := exec.Command(swiftc, "-O", tmpSwift, "-o", recBin)
			if err := buildCmd.Run(); err == nil {
				_ = os.Remove(tmpSwift)
				cmd := exec.Command(recBin, recFile)
				if err := cmd.Start(); err == nil {
					return cmd, nil
				}
			}
		}
	}

	// 3. Fallback: check for rec or ffmpeg
	if _, err := exec.LookPath("rec"); err == nil {
		cmd := exec.Command("rec", "-q", recFile)
		if err := cmd.Start(); err == nil {
			return cmd, nil
		}
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		cmd := exec.Command("ffmpeg", "-y", "-f", "avfoundation", "-i", ":0", "-ar", "16000", "-ac", "1", recFile)
		if err := cmd.Start(); err == nil {
			return cmd, nil
		}
	}

	return nil, fmt.Errorf("macOS microphone capture requires swiftc (Xcode Command Line Tools) or sox/ffmpeg")
}

// StopRecording halts recording and returns the recorded audio file path.
func (vm *VoiceManager) StopRecording() (string, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.isRecording || vm.recordCmd == nil {
		return "", fmt.Errorf("not recording")
	}

	recFile := vm.recordFile
	cmd := vm.recordCmd

	vm.isRecording = false
	vm.recordCmd = nil
	vm.recordFile = ""

	if cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		select {
		case <-done:
		case <-time.After(1 * time.Second):
			_ = cmd.Process.Kill()
		}
	}

	// Verify file exists and has size
	fi, err := os.Stat(recFile)
	if err != nil || fi.Size() < 500 {
		_ = os.Remove(recFile)
		return "", fmt.Errorf("no audio captured")
	}

	return recFile, nil
}

// CancelRecording cancels the active recording and removes any temporary audio files.
func (vm *VoiceManager) CancelRecording() {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if !vm.isRecording {
		return
	}

	if vm.recordCmd != nil && vm.recordCmd.Process != nil {
		_ = vm.recordCmd.Process.Kill()
	}
	if vm.recordFile != "" {
		_ = os.Remove(vm.recordFile)
	}

	vm.isRecording = false
	vm.recordCmd = nil
	vm.recordFile = ""
}

// TranscribeCmd produces a Bubbletea Cmd that stops recording and transcribes in the background.
func (vm *VoiceManager) TranscribeCmd() tea.Cmd {
	return func() tea.Msg {
		recFile, err := vm.StopRecording()
		if err != nil {
			return VoiceTranscriptionMsg{Err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		text, err := vm.Transcribe(ctx, recFile)
		return VoiceTranscriptionMsg{Text: text, Err: err}
	}
}

// Transcribe converts recorded audio to text.
func (vm *VoiceManager) Transcribe(ctx context.Context, audioPath string) (string, error) {
	defer os.Remove(audioPath)

	// 1. Try Cloud / LiteLLM Whisper API if key is available
	if vm.openAIKey != "" {
		text, err := vm.transcribeCloudWhisper(ctx, audioPath)
		if err == nil && text != "" {
			return text, nil
		}
	}

	// 2. On macOS: Try native offline SFSpeechRecognizer
	if runtime.GOOS == "darwin" {
		text, err := vm.transcribeDarwinOffline(audioPath)
		if err == nil && text != "" {
			return text, nil
		}
	}

	// 3. Fallback: local whisper CLI
	if _, err := exec.LookPath("whisper"); err == nil {
		cmd := exec.CommandContext(ctx, "whisper", audioPath, "--output_format", "txt", "--model", "base.en")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			txt := strings.TrimSpace(out.String())
			if txt != "" {
				return txt, nil
			}
		}
	}

	return "", fmt.Errorf("transcription failed: set OPENAI_API_KEY or configure litellm.json for cloud Whisper, or install whisper CLI")
}

func (vm *VoiceManager) transcribeCloudWhisper(ctx context.Context, audioPath string) (string, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}

	modelName := "whisper-1"
	if strings.Contains(vm.openAIBaseURL, "groq.com") {
		modelName = "whisper-large-v3-turbo"
	}
	_ = writer.WriteField("model", modelName)
	_ = writer.WriteField("response_format", "json")
	_ = writer.Close()

	endpoint := strings.TrimRight(vm.openAIBaseURL, "/") + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+vm.openAIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("whisper API error (%d): %s", resp.StatusCode, string(respBytes))
	}

	var result struct {
		Text  string `json:"text"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}
	if result.Error.Message != "" {
		return "", fmt.Errorf("%s", result.Error.Message)
	}

	return strings.TrimSpace(result.Text), nil
}

func (vm *VoiceManager) transcribeDarwinOffline(audioPath string) (string, error) {
	binDir := getVoiceBinDir()
	scriptFile := filepath.Join(binDir, "transcribe.swift")

	if _, err := os.Stat(scriptFile); os.IsNotExist(err) {
		_ = os.MkdirAll(binDir, 0755)
		swiftSrc := `
import Foundation
import Speech

guard CommandLine.arguments.count > 1 else { exit(1) }
let audioURL = URL(fileURLWithPath: CommandLine.arguments[1])
guard let recognizer = SFSpeechRecognizer(locale: Locale(identifier: "en-US")), recognizer.isAvailable else {
    exit(1)
}

let request = SFSpeechURLRecognitionRequest(url: audioURL)
let semaphore = DispatchSemaphore(value: 0)

recognizer.recognitionTask(with: request) { result, error in
    if error != nil {
        semaphore.signal()
        return
    }
    if let result = result, result.isFinal {
        print(result.bestTranscription.formattedString)
        semaphore.signal()
    }
}
_ = semaphore.wait(timeout: .now() + 15.0)
`
		_ = os.WriteFile(scriptFile, []byte(swiftSrc), 0644)
	}

	cmd := exec.Command("swift", scriptFile, audioPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return strings.TrimSpace(out.String()), nil
}

// Speak converts text to audible speech using native platform TTS.
func (vm *VoiceManager) Speak(text string) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Stop any existing speech
	if vm.speakCmd != nil && vm.speakCmd.Process != nil {
		_ = vm.speakCmd.Process.Kill()
		vm.isSpeaking = false
		vm.speakCmd = nil
	}

	cleaned := CleanTextForTTS(text)
	if cleaned == "" {
		return nil
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("/usr/bin/say", cleaned)
	case "windows":
		psScript := "Add-Type -AssemblyName System.Speech; (New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak([Console]::In.ReadToEnd())"
		cmd = exec.Command("powershell", "-NoProfile", "-Command", psScript)
		cmd.Stdin = strings.NewReader(cleaned)
	default:
		if _, err := exec.LookPath("spd-say"); err == nil {
			cmd = exec.Command("spd-say", cleaned)
		} else if _, err := exec.LookPath("espeak"); err == nil {
			cmd = exec.Command("espeak", cleaned)
		} else {
			return fmt.Errorf("no TTS engine found (install spd-say or espeak)")
		}
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting speech synthesis: %w", err)
	}

	vm.speakCmd = cmd
	vm.isSpeaking = true

	go func() {
		_ = cmd.Wait()
		vm.mu.Lock()
		if vm.speakCmd == cmd {
			vm.isSpeaking = false
			vm.speakCmd = nil
		}
		vm.mu.Unlock()
	}()

	return nil
}

// StopSpeaking halts any active speech output.
func (vm *VoiceManager) StopSpeaking() {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.speakCmd != nil && vm.speakCmd.Process != nil {
		_ = vm.speakCmd.Process.Kill()
		vm.isSpeaking = false
		vm.speakCmd = nil
	}
	if runtime.GOOS == "darwin" {
		_ = exec.Command("/usr/bin/killall", "say").Run()
	}
}

// VoiceTranscriptionMsg carries speech-to-text results into Bubbletea.
type VoiceTranscriptionMsg struct {
	Text string
	Err  error
}

// CleanTextForTTS strips markdown, code blocks, diffs, and formatting tokens
// so spoken audio sounds natural.
func CleanTextForTTS(text string) string {
	t := text
	// Remove code blocks
	reCode := regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*\\n.*?```")
	t = reCode.ReplaceAllString(t, "code omitted")

	// Remove inline code
	reInlineCode := regexp.MustCompile("`([^`]+)`")
	t = reInlineCode.ReplaceAllString(t, "$1")

	// Remove markdown links [label](url) -> label
	reLink := regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`)
	t = reLink.ReplaceAllString(t, "$1")

	// Remove bold and italic formatting
	reBold := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	t = reBold.ReplaceAllString(t, "$1")
	reItalic := regexp.MustCompile(`\*([^*]+)\*`)
	t = reItalic.ReplaceAllString(t, "$1")

	// Remove markdown headers
	reHeader := regexp.MustCompile(`(?m)^#{1,6}\s*`)
	t = reHeader.ReplaceAllString(t, "")

	// Remove bullet point markers
	reBullet := regexp.MustCompile(`(?m)^[\s]*[-*+]\s*`)
	t = reBullet.ReplaceAllString(t, "")

	// Collapse multiple whitespaces and newlines
	reSpace := regexp.MustCompile(`\s+`)
	t = reSpace.ReplaceAllString(t, " ")

	return strings.TrimSpace(t)
}

func getVoiceBinDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".divmora", "localharness", "bin")
	}
	return filepath.Join(os.TempDir(), "localharness_bin")
}
