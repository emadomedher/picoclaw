package channels

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
)

type MatrixChannel struct {
	*BaseChannel
	client       *mautrix.Client
	matrixConfig config.MatrixConfig
	syncer       *mautrix.DefaultSyncer
	stopSyncer   context.CancelFunc
	roomNames    sync.Map // roomID -> room name
}

func NewMatrixChannel(matrixCfg config.MatrixConfig, bus *bus.MessageBus) (*MatrixChannel, error) {
	// Create Matrix client
	client, err := mautrix.NewClient(matrixCfg.Homeserver, id.UserID(matrixCfg.UserID), matrixCfg.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create matrix client: %w", err)
	}

	// Set device ID if provided
	if matrixCfg.DeviceID != "" {
		client.DeviceID = id.DeviceID(matrixCfg.DeviceID)
	}

	base := NewBaseChannel("matrix", matrixCfg, bus, matrixCfg.AllowFrom)

	syncer := client.Syncer.(*mautrix.DefaultSyncer)

	return &MatrixChannel{
		BaseChannel:  base,
		client:       client,
		matrixConfig: matrixCfg,
		syncer:       syncer,
		roomNames:    sync.Map{},
	}, nil
}

func (c *MatrixChannel) Start(ctx context.Context) error {
	logger.InfoC("matrix", "Starting Matrix client...")

	// Set up event handlers
	c.syncer.OnEventType(event.EventMessage, c.handleMessage)
	c.syncer.OnEventType(event.StateMember, c.handleMemberEvent)

	// Create a cancellable context for the syncer
	syncCtx, cancel := context.WithCancel(ctx)
	c.stopSyncer = cancel

	// Start syncing in background
	go func() {
		err := c.client.SyncWithContext(syncCtx)
		if err != nil && syncCtx.Err() == nil {
			logger.ErrorCF("matrix", "Sync error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	c.setRunning(true)
	logger.InfoC("matrix", "Matrix client started successfully")
	return nil
}

func (c *MatrixChannel) Stop(ctx context.Context) error {
	logger.InfoC("matrix", "Stopping Matrix client...")

	if c.stopSyncer != nil {
		c.stopSyncer()
	}

	c.setRunning(false)
	logger.InfoC("matrix", "Matrix client stopped")
	return nil
}

func (c *MatrixChannel) handleMemberEvent(ctx context.Context, evt *event.Event) {
	memberEvt := evt.Content.AsMember()
	
	// Auto-join rooms if invited and JoinOnInvite is enabled
	if memberEvt.Membership == event.MembershipInvite && 
	   evt.GetStateKey() == string(c.client.UserID) &&
	   c.matrixConfig.JoinOnInvite {
		
		roomID := evt.RoomID
		logger.InfoCF("matrix", "Auto-joining room after invite", map[string]interface{}{
			"room_id": roomID.String(),
		})
		
		_, err := c.client.JoinRoomByID(ctx, roomID)
		if err != nil {
			logger.ErrorCF("matrix", "Failed to join room", map[string]interface{}{
				"room_id": roomID.String(),
				"error":   err.Error(),
			})
		} else {
			logger.InfoCF("matrix", "Successfully joined room", map[string]interface{}{
				"room_id": roomID.String(),
			})
		}
	}
}

func (c *MatrixChannel) handleMessage(ctx context.Context, evt *event.Event) {
	// Ignore our own messages
	if evt.Sender == c.client.UserID {
		return
	}

	// Only handle text messages
	msgEvt := evt.Content.AsMessage()
	if msgEvt.MsgType != event.MsgText {
		return
	}

	roomID := evt.RoomID.String()
	senderID := evt.Sender.String()

	// Check if sender is allowed
	if !c.IsAllowed(senderID) {
		logger.WarnCF("matrix", "Ignoring message from unauthorized user", map[string]interface{}{
			"sender_id": senderID,
		})
		return
	}

	// Get or cache room name
	roomName := c.getRoomName(ctx, evt.RoomID)

	// Get sender display name
	senderName := c.getUserDisplayName(ctx, evt.RoomID, evt.Sender)

	messageText := msgEvt.Body

	logger.InfoCF("matrix", "Received message", map[string]interface{}{
		"sender":   senderName,
		"room":     roomName,
		"content":  messageText,
	})

	// Check if it's a group chat
	memberCount := c.getRoomMemberCount(ctx, evt.RoomID)
	isGroup := memberCount > 2

	logger.InfoCF("matrix", "Message context", map[string]interface{}{
		"room":          roomName,
		"member_count":  memberCount,
		"is_group_chat": isGroup,
	})

	// In group chats, check mention requirement
	if isGroup && c.matrixConfig.RequireMentionInGroup {
		mentioned := c.isBotMentioned(msgEvt, c.client.UserID)
		if !mentioned {
			logger.InfoCF("matrix", "Ignoring group message (not mentioned)", map[string]interface{}{
				"room":   roomName,
				"sender": senderName,
			})
			return
		}
		logger.InfoCF("matrix", "Bot mentioned in group chat", map[string]interface{}{
			"room":   roomName,
			"sender": senderName,
		})
		// Remove the mention from the message text
		messageText = c.removeMention(messageText, c.client.UserID)
	}

	// Prepare metadata
	metadata := map[string]string{
		"sender_name":  senderName,
		"room_name":    roomName,
		"timestamp":    fmt.Sprintf("%d", evt.Timestamp),
	}

	if isGroup {
		metadata["is_group_chat"] = "true"
	}

	// Check for reply-to
	replyToID := c.getReplyToID(msgEvt)
	if replyToID != "" {
		metadata["reply_to_msg_id"] = replyToID
	}

	// Handle the message through base channel
	c.HandleMessage(senderID, roomID, messageText, []string{}, metadata)
}

func (c *MatrixChannel) getRoomName(ctx context.Context, roomID id.RoomID) string {
	// Check cache first
	if cached, ok := c.roomNames.Load(roomID.String()); ok {
		return cached.(string)
	}

	// Fetch room name from state event
	var nameEvt event.RoomNameEventContent
	err := c.client.StateEvent(ctx, roomID, event.StateRoomName, "", &nameEvt)
	if err == nil && nameEvt.Name != "" {
		c.roomNames.Store(roomID.String(), nameEvt.Name)
		return nameEvt.Name
	}

	// Fallback to room ID
	roomName := roomID.String()
	c.roomNames.Store(roomID.String(), roomName)
	return roomName
}

func (c *MatrixChannel) getUserDisplayName(ctx context.Context, roomID id.RoomID, userID id.UserID) string {
	resp, err := c.client.GetDisplayName(ctx, userID)
	if err == nil && resp.DisplayName != "" {
		return resp.DisplayName
	}
	return userID.String()
}

func (c *MatrixChannel) getRoomMemberCount(ctx context.Context, roomID id.RoomID) int {
	// Get joined members count
	resp, err := c.client.JoinedMembers(ctx, roomID)
	if err != nil {
		return 0
	}
	return len(resp.Joined)
}

func (c *MatrixChannel) isGroupChat(ctx context.Context, roomID id.RoomID) bool {
	return c.getRoomMemberCount(ctx, roomID) > 2
}

func (c *MatrixChannel) getReplyToID(msgEvt *event.MessageEventContent) string {
	if msgEvt.RelatesTo != nil && msgEvt.RelatesTo.InReplyTo != nil {
		return msgEvt.RelatesTo.InReplyTo.EventID.String()
	}
	return ""
}

func (c *MatrixChannel) isBotMentioned(msgEvt *event.MessageEventContent, botUserID id.UserID) bool {
	// Check plain text body for mention
	if strings.Contains(msgEvt.Body, botUserID.String()) {
		return true
	}

	// Check formatted body (HTML) for mention
	if msgEvt.Format == event.FormatHTML && strings.Contains(msgEvt.FormattedBody, botUserID.String()) {
		return true
	}

	// Check for displayname mention (e.g., "wanda" or "Wanda")
	// This is less reliable but common in Matrix clients
	displayName := strings.TrimPrefix(botUserID.String(), "@")
	displayName = strings.Split(displayName, ":")[0] // Get localpart only
	lowerBody := strings.ToLower(msgEvt.Body)
	if strings.Contains(lowerBody, strings.ToLower(displayName)) {
		return true
	}

	return false
}

func (c *MatrixChannel) removeMention(text string, botUserID id.UserID) string {
	// Remove @user:homeserver mentions
	text = strings.ReplaceAll(text, botUserID.String(), "")
	
	// Remove localpart mentions (e.g., "wanda")
	displayName := strings.TrimPrefix(botUserID.String(), "@")
	displayName = strings.Split(displayName, ":")[0]
	
	// Remove with @ prefix
	text = strings.ReplaceAll(text, "@"+displayName, "")
	
	// Remove standalone displayname at start/end
	text = strings.TrimPrefix(text, displayName)
	text = strings.TrimSuffix(text, displayName)
	
	// Clean up extra whitespace
	text = strings.TrimSpace(text)
	
	return text
}

func (c *MatrixChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	roomID := id.RoomID(msg.ChatID)

	// Prepare message content
	content := &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    msg.Content,
	}

	// Handle Markdown formatting
	if strings.Contains(msg.Content, "**") || strings.Contains(msg.Content, "_") || 
	   strings.Contains(msg.Content, "`") || strings.Contains(msg.Content, "#") {
		content.Format = event.FormatHTML
		content.FormattedBody = c.markdownToHTML(msg.Content)
	}

	// Send the message
	_, err := c.client.SendMessageEvent(ctx, roomID, event.EventMessage, content)
	if err != nil {
		return fmt.Errorf("failed to send matrix message: %w", err)
	}

	logger.InfoCF("matrix", "Sent message to room", map[string]interface{}{
		"chat_id": msg.ChatID,
	})
	return nil
}

// Simple markdown to HTML converter for Matrix
func (c *MatrixChannel) markdownToHTML(text string) string {
	html := text
	
	// Bold: **text** -> <strong>text</strong>
	html = strings.ReplaceAll(html, "**", "<strong>")
	// Count replacements and close tags
	count := strings.Count(text, "**")
	for i := 0; i < count/2; i++ {
		html = strings.Replace(html, "<strong>", "<strong>", 1)
		html = strings.Replace(html, "<strong>", "</strong>", 1)
	}
	
	// Italic: _text_ -> <em>text</em>
	// Code: `text` -> <code>text</code>
	// Simple replacements for now
	
	return html
}
