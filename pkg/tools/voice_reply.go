package tools

import (
	"context"
	"fmt"
)

// VoiceSendCallback synthesizes text to audio and delivers it to the given channel/chat.
// The implementation owns the full lifecycle: synthesis, upload, and cleanup.
type VoiceSendCallback func(ctx context.Context, channel, chatID, text string) error

// VoiceReplyTool lets the agent choose to reply with a spoken voice message.
// Use this when the user sent a voice message, explicitly asked for audio,
// or when a voice response is contextually more appropriate than text.
type VoiceReplyTool struct {
	sendCallback   VoiceSendCallback
	defaultChannel string
	defaultChatID  string
	sentInRound    bool
}

func NewVoiceReplyTool() *VoiceReplyTool {
	return &VoiceReplyTool{}
}

func (t *VoiceReplyTool) Name() string {
	return "voice_reply"
}

func (t *VoiceReplyTool) Description() string {
	return `Send a voice message (audio) to the user instead of text.
Use this when:
- The user sent you a voice/audio message
- The user explicitly asks you to reply with voice
- A spoken response is more natural for the context (e.g., storytelling, greetings)
Do NOT use this for every reply — default to text unless voice is clearly appropriate.`
}

func (t *VoiceReplyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"description": "The text to speak aloud. Write naturally for listening, not reading — avoid markdown, bullet points, or code blocks.",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target channel override",
			},
			"chat_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target chat ID override",
			},
		},
		"required": []string{"text"},
	}
}

func (t *VoiceReplyTool) SetContext(channel, chatID string) {
	t.defaultChannel = channel
	t.defaultChatID = chatID
	t.sentInRound = false
}

func (t *VoiceReplyTool) HasSentInRound() bool {
	return t.sentInRound
}

func (t *VoiceReplyTool) SetSendCallback(cb VoiceSendCallback) {
	t.sendCallback = cb
}

func (t *VoiceReplyTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	text, ok := args["text"].(string)
	if !ok || text == "" {
		return &ToolResult{ForLLM: "text is required", IsError: true}
	}

	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)

	if channel == "" {
		channel = t.defaultChannel
	}
	if chatID == "" {
		chatID = t.defaultChatID
	}

	if channel == "" || chatID == "" {
		return &ToolResult{ForLLM: "No target channel/chat — set context first", IsError: true}
	}

	if t.sendCallback == nil {
		return &ToolResult{ForLLM: "Voice reply not configured (no TTS available)", IsError: true}
	}

	if err := t.sendCallback(ctx, channel, chatID, text); err != nil {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("voice reply failed: %v", err),
			IsError: true,
			Err:     err,
		}
	}

	t.sentInRound = true
	return &ToolResult{
		ForLLM: fmt.Sprintf("Voice message sent to %s:%s", channel, chatID),
		Silent: true,
	}
}
