package channels

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
)

// EmailChannel polls an IMAP inbox and delivers new messages into the agent bus.
// Outbound replies are a no-op — agents respond via their primary channel (e.g. Matrix).
type EmailChannel struct {
	*BaseChannel
	emailConfig config.EmailConfig
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

func NewEmailChannel(cfg config.EmailConfig, bus *bus.MessageBus) (*EmailChannel, error) {
	if cfg.IMAPHost == "" {
		return nil, fmt.Errorf("email channel: imap_host is required")
	}
	if cfg.Username == "" || cfg.Password == "" {
		return nil, fmt.Errorf("email channel: username and password are required")
	}

	base := NewBaseChannel("email", cfg, bus, cfg.AllowFrom)

	return &EmailChannel{
		BaseChannel: base,
		emailConfig: cfg,
		stopCh:      make(chan struct{}),
	}, nil
}

func (c *EmailChannel) Start(ctx context.Context) error {
	logger.InfoCF("email", "Starting email channel", map[string]interface{}{
		"host":          c.emailConfig.IMAPHost,
		"port":          c.emailConfig.IMAPPort,
		"username":      c.emailConfig.Username,
		"poll_interval": c.emailConfig.PollInterval,
	})

	c.setRunning(true)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.pollLoop(ctx)
	}()

	return nil
}

func (c *EmailChannel) Stop(_ context.Context) error {
	logger.InfoC("email", "Stopping email channel")
	close(c.stopCh)
	c.wg.Wait()
	c.setRunning(false)
	return nil
}

// Send is a no-op — agents receive emails and reply via their primary channel.
func (c *EmailChannel) Send(_ context.Context, msg bus.OutboundMessage) error {
	logger.WarnCF("email", "Outbound email not implemented — agent replies via primary channel", map[string]interface{}{
		"chat_id": msg.ChatID,
	})
	return nil
}

// pollLoop runs at the configured interval and fetches UNSEEN messages.
func (c *EmailChannel) pollLoop(ctx context.Context) {
	interval := time.Duration(c.emailConfig.PollInterval) * time.Second
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}

	// Immediate poll on start
	c.fetchUnseen()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.fetchUnseen()
		}
	}
}

// connect opens and authenticates an IMAP connection.
func (c *EmailChannel) connect() (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", c.emailConfig.IMAPHost, c.emailConfig.IMAPPort)

	var (
		client *imapclient.Client
		err    error
	)

	if c.emailConfig.TLS {
		tlsCfg := &tls.Config{
			ServerName: c.emailConfig.IMAPHost,
			MinVersion: tls.VersionTLS12,
		}
		client, err = imapclient.DialTLS(addr, &imapclient.Options{TLSConfig: tlsCfg})
	} else {
		client, err = imapclient.DialInsecure(addr, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("imap dial: %w", err)
	}

	if err := client.Login(c.emailConfig.Username, c.emailConfig.Password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("imap login: %w", err)
	}

	return client, nil
}

// fetchUnseen connects, fetches all UNSEEN messages, publishes them, then marks as SEEN.
func (c *EmailChannel) fetchUnseen() {
	client, err := c.connect()
	if err != nil {
		logger.ErrorCF("email", "IMAP connection failed", map[string]interface{}{"error": err.Error()})
		return
	}
	defer client.Logout()

	if _, err = client.Select("INBOX", nil).Wait(); err != nil {
		logger.ErrorCF("email", "Failed to select INBOX", map[string]interface{}{"error": err.Error()})
		return
	}

	searchData, err := client.Search(&imap.SearchCriteria{
		NotFlag: []imap.Flag{imap.FlagSeen},
	}, nil).Wait()
	if err != nil {
		logger.ErrorCF("email", "IMAP search failed", map[string]interface{}{"error": err.Error()})
		return
	}

	seqNums := searchData.AllSeqNums()
	if len(seqNums) == 0 {
		return
	}

	seqSet := imap.SeqSetNum(seqNums...)

	fetchOptions := &imap.FetchOptions{
		Envelope: true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierText},
		},
	}

	messages, err := client.Fetch(seqSet, fetchOptions).Collect()
	if err != nil {
		logger.ErrorCF("email", "IMAP fetch failed", map[string]interface{}{"error": err.Error()})
		return
	}

	for _, msg := range messages {
		c.processMessage(msg)
	}

	// Mark all fetched messages as SEEN
	if err := client.Store(seqSet, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagSeen},
	}, nil).Close(); err != nil {
		logger.WarnCF("email", "Failed to mark messages as seen", map[string]interface{}{"error": err.Error()})
	}
}

// processMessage parses a buffered IMAP message and publishes it to the bus.
func (c *EmailChannel) processMessage(msg *imapclient.FetchMessageBuffer) {
	env := msg.Envelope
	if env == nil {
		return
	}

	// Extract sender address
	senderEmail := ""
	if len(env.From) > 0 {
		addr := env.From[0]
		if addr.Host != "" {
			senderEmail = fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
		} else {
			senderEmail = addr.Mailbox
		}
	}

	subject := env.Subject
	body := c.extractBody(msg)

	content := fmt.Sprintf("📧 **Email from:** %s\n**Subject:** %s\n\n%s",
		senderEmail, subject, strings.TrimSpace(body))

	logger.InfoCF("email", "Received email", map[string]interface{}{
		"from":    senderEmail,
		"subject": subject,
	})

	chatID := senderEmail
	if chatID == "" {
		chatID = "unknown-sender"
	}

	c.HandleMessage(senderEmail, chatID, content, nil, map[string]string{
		"subject": subject,
		"from":    senderEmail,
	})
}

// extractBody returns the plaintext body from a buffered IMAP message.
func (c *EmailChannel) extractBody(msg *imapclient.FetchMessageBuffer) string {
	for _, section := range msg.BodySection {
		if len(section.Bytes) == 0 {
			continue
		}

		mr, err := mail.CreateReader(strings.NewReader(string(section.Bytes)))
		if err != nil {
			// Not a MIME message — return raw bytes
			return strings.TrimSpace(string(section.Bytes))
		}

		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}

			ct := part.Header.Get("Content-Type")
			if strings.HasPrefix(ct, "text/plain") {
				b, err := io.ReadAll(part.Body)
				if err == nil {
					return strings.TrimSpace(string(b))
				}
			}
		}
	}

	return ""
}

// compile-time interface check
var _ Channel = (*EmailChannel)(nil)
