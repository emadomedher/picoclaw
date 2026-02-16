// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package channels

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/utils"
)

type MatrixChannel struct {
	*BaseChannel
	client       *mautrix.Client
	config       config.MatrixConfig
	ctx          context.Context
	stopSync     context.CancelFunc
	syncerWg     sync.WaitGroup
	joinedRooms  map[id.RoomID]bool
	roomsMutex   sync.RWMutex
}

func NewMatrixChannel(cfg config.MatrixConfig, msgBus *bus.MessageBus) (*MatrixChannel, error) {
	client, err := mautrix.NewClient(cfg.Homeserver, id.UserID(cfg.UserID), cfg.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create matrix client: %w", err)
	}

	// Set device ID if provided
	if cfg.DeviceID != "" {
		client.DeviceID = id.DeviceID(cfg.DeviceID)
	}

	base := NewBaseChannel("matrix", cfg, msgBus, cfg.AllowFrom)

	return &MatrixChannel{
		BaseChannel: base,
		client:      client,
		config:      cfg,
		joinedRooms: make(map[id.RoomID]bool),
	}, nil
}

func (c *MatrixChannel) Start(ctx context.Context) error {
	logger.InfoC("matrix", "Starting Matrix client")

	c.ctx, c.stopSync = context.WithCancel(ctx)

	// Set up event handlers
	syncer := c.client.Syncer.(*mautrix.DefaultSyncer)

	// Handle incoming messages
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		c.handleMessage(evt)
	})

	// Handle room invites
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		c.handleMemberEvent(evt)
	})

	// Start syncing in background
	c.syncerWg.Add(1)
	go func() {
		defer c.syncerWg.Done()
		if err := c.client.SyncWithContext(c.ctx); err != nil && c.ctx.Err() == nil {
			logger.ErrorCF("matrix", "Sync error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// Wait a moment for initial sync
	time.Sleep(500 * time.Millisecond)

	c.setRunning(true)
	logger.InfoCF("matrix", "Matrix client connected", map[string]interface{}{
		"user_id":    c.config.UserID,
		"homeserver": c.config.Homeserver,
	})

	return nil
}

func (c *MatrixChannel) Stop(ctx context.Context) error {
	logger.InfoC("matrix", "Stopping Matrix client")
	c.setRunning(false)

	if c.stopSync != nil {
		c.stopSync()
	}

	// Wait for syncer to stop (with timeout)
	done := make(chan struct{})
	go func() {
		c.syncerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.InfoC("matrix", "Matrix client stopped gracefully")
	case <-time.After(5 * time.Second):
		logger.WarnC("matrix", "Matrix client stop timeout")
	}

	return nil
}

func (c *MatrixChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("matrix client not running")
	}

	roomID := id.RoomID(msg.ChatID)
	if roomID == "" {
		return fmt.Errorf("room ID is empty")
	}

	// Split long messages if needed (Matrix limit is ~65KB, we'll use 60KB to be safe)
	chunks := c.splitMessage(msg.Content, 60000)

	for _, chunk := range chunks {
		content := &event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    chunk,
		}

		// Send formatted HTML if content has markdown-like formatting
		if c.hasFormatting(chunk) {
			content.Format = event.FormatHTML
			content.FormattedBody = c.convertToHTML(chunk)
		}

		_, err := c.client.SendMessageEvent(ctx, roomID, event.EventMessage, content)
		if err != nil {
			return fmt.Errorf("failed to send message to %s: %w", roomID, err)
		}

		// Small delay between chunks to avoid rate limiting
		if len(chunks) > 1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

func (c *MatrixChannel) handleMessage(evt *event.Event) {
	// Ignore our own messages
	if evt.Sender == id.UserID(c.config.UserID) {
		return
	}

	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		return
	}

	// Only handle text messages for now
	if content.MsgType != event.MsgText && content.MsgType != event.MsgNotice {
		return
	}

	senderID := string(evt.Sender)
	roomID := string(evt.RoomID)

	// Check allowlist
	if !c.IsAllowed(senderID) {
		logger.DebugCF("matrix", "Message from unauthorized user", map[string]interface{}{
			"sender": senderID,
			"room":   roomID,
		})
		return
	}

	// Extract message text (prefer formatted body, fallback to plain)
	messageText := content.Body

	logger.DebugCF("matrix", "Received message", map[string]interface{}{
		"sender": senderID,
		"room":   roomID,
		"text":   utils.Truncate(messageText, 100),
	})

	// Send to bus
	c.HandleMessage(senderID, roomID, messageText, nil, map[string]string{
		"event_id": string(evt.ID),
	})
}

func (c *MatrixChannel) handleMemberEvent(evt *event.Event) {
	// Only handle invites if auto-join is enabled
	if !c.config.JoinOnInvite {
		return
	}

	content, ok := evt.Content.Parsed.(*event.MemberEventContent)
	if !ok {
		return
	}

	// Check if this is an invite for us
	if *evt.StateKey != c.config.UserID || content.Membership != event.MembershipInvite {
		return
	}

	logger.InfoCF("matrix", "Received room invite", map[string]interface{}{
		"room":   string(evt.RoomID),
		"sender": string(evt.Sender),
	})

	// Check if inviter is allowed
	if !c.IsAllowed(string(evt.Sender)) {
		logger.WarnCF("matrix", "Rejected invite from unauthorized user", map[string]interface{}{
			"sender": string(evt.Sender),
		})
		return
	}

	// Join the room
	if _, err := c.client.JoinRoomByID(context.Background(), evt.RoomID); err != nil {
		logger.ErrorCF("matrix", "Failed to join room", map[string]interface{}{
			"room":  string(evt.RoomID),
			"error": err.Error(),
		})
		return
	}

	c.roomsMutex.Lock()
	c.joinedRooms[evt.RoomID] = true
	c.roomsMutex.Unlock()

	logger.InfoCF("matrix", "Joined room", map[string]interface{}{
		"room": string(evt.RoomID),
	})
}

// splitMessage splits long messages into chunks
func (c *MatrixChannel) splitMessage(content string, maxLength int) []string {
	if len(content) <= maxLength {
		return []string{content}
	}

	var chunks []string
	for len(content) > 0 {
		if len(content) <= maxLength {
			chunks = append(chunks, content)
			break
		}

		// Try to split at newline
		splitPoint := maxLength
		lastNewline := strings.LastIndex(content[:maxLength], "\n")
		if lastNewline > maxLength/2 {
			splitPoint = lastNewline + 1
		}

		chunks = append(chunks, content[:splitPoint])
		content = content[splitPoint:]
	}

	return chunks
}

// hasFormatting checks if content has markdown-like formatting
func (c *MatrixChannel) hasFormatting(text string) bool {
	return strings.Contains(text, "**") ||
		strings.Contains(text, "```") ||
		strings.Contains(text, "`") ||
		strings.Contains(text, "_")
}

// convertToHTML converts markdown-like text to HTML
// This is a simple implementation, can be enhanced later
func (c *MatrixChannel) convertToHTML(text string) string {
	html := text

	// Code blocks
	html = strings.ReplaceAll(html, "```", "<pre><code>")
	// Inline code
	parts := strings.Split(html, "`")
	for i := 1; i < len(parts); i += 2 {
		parts[i] = "<code>" + parts[i] + "</code>"
	}
	html = strings.Join(parts, "")

	// Bold
	html = strings.ReplaceAll(html, "**", "<strong>")
	// Italic
	html = strings.ReplaceAll(html, "_", "<em>")

	return html
}
