package discord

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"chatcode/internal/domain"
)

// Bot implements domain.Transport for Discord.
type Bot struct {
	token             string
	allowedChannelIDs map[string]struct{} // empty = allow all
	session           *discordgo.Session
}

func New(token string, allowedChannelIDs []string) *Bot {
	m := make(map[string]struct{}, len(allowedChannelIDs))
	for _, id := range allowedChannelIDs {
		if id != "" {
			m[id] = struct{}{}
		}
	}
	return &Bot{
		token:             token,
		allowedChannelIDs: m,
	}
}

func (b *Bot) Name() string { return "discord" }

func (b *Bot) Start(ctx context.Context, handler domain.MessageHandler) error {
	dg, err := discordgo.New("Bot " + b.token)
	if err != nil {
		return fmt.Errorf("discordgo.New: %w", err)
	}
	b.session = dg
	dg.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentMessageContent

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil || m.Author.Bot {
			return
		}
		channelID := m.ChannelID

		// Resolve chatID, handling threads.
		chatID := channelID
		threadID := ""
		ch, err := s.Channel(channelID)
		if err != nil {
			slog.Warn("discord: failed to resolve channel", "channelID", channelID, "err", err)
		} else if isThread(ch.Type) {
			threadID = channelID
			chatID = ch.ParentID
		}

		// Filter by allowed channel IDs (using parent chatID).
		if len(b.allowedChannelIDs) > 0 {
			if _, ok := b.allowedChannelIDs[chatID]; !ok {
				return
			}
		}

		msg := domain.Message{
			SessionKey: domain.SessionKey{
				Platform: domain.PlatformDiscord,
				ChatID:   chatID,
				ThreadID: threadID,
			},
			SenderID: m.Author.ID,
			Text:     strings.TrimSpace(m.Content),
			At:       time.Now(),
		}
		if err := handler(ctx, msg); err != nil {
			slog.Warn("discord: handler error", "err", err)
		}
	})

	dg.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		data := i.ApplicationCommandData()

		// Build text as "/command arg" mirroring Telegram slash command format.
		parts := []string{"/" + data.Name}
		for _, opt := range data.Options {
			switch opt.Type {
			case discordgo.ApplicationCommandOptionString:
				if v := opt.StringValue(); v != "" {
					parts = append(parts, v)
				}
			}
		}
		text := strings.Join(parts, " ")

		// Immediately ack to prevent Discord's 3-second timeout error,
		// then edit the deferred response to show the command text as a visual anchor.
		// The actual reply is sent as a normal channel message following it.
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		}); err != nil {
			slog.Warn("discord: interaction ack failed", "err", err)
		} else {
			go func() {
				time.Sleep(200 * time.Millisecond)
				content := "`" + text + "`"
				if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Content: &content,
				}); err != nil {
					slog.Warn("discord: edit interaction response failed", "err", err)
				}
			}()
		}

		channelID := i.ChannelID
		chatID := channelID
		threadID := ""
		ch, err := s.Channel(channelID)
		if err != nil {
			slog.Warn("discord: failed to resolve interaction channel", "channelID", channelID, "err", err)
		} else if isThread(ch.Type) {
			threadID = channelID
			chatID = ch.ParentID
		}

		senderID := ""
		if i.Member != nil && i.Member.User != nil {
			senderID = i.Member.User.ID
		} else if i.User != nil {
			senderID = i.User.ID
		}

		msg := domain.Message{
			SessionKey: domain.SessionKey{
				Platform: domain.PlatformDiscord,
				ChatID:   chatID,
				ThreadID: threadID,
			},
			SenderID: senderID,
			Text:     text,
			At:       time.Now(),
		}
		if err := handler(ctx, msg); err != nil {
			slog.Warn("discord: interaction handler error", "err", err)
		}
	})

	if err := dg.Open(); err != nil {
		return fmt.Errorf("discord open: %w", err)
	}
	slog.Info("discord bot connected")

	b.registerCommands(dg)

	<-ctx.Done()
	if err := dg.Close(); err != nil {
		slog.Warn("discord close", "err", err)
	}
	return nil
}

func (b *Bot) registerCommands(s *discordgo.Session) {
	cmds := discordCommands()
	if _, err := s.ApplicationCommandBulkOverwrite(s.State.User.ID, "", cmds); err != nil {
		slog.Error("discord: failed to register slash commands", "err", err)
		return
	}
	slog.Info("discord slash commands registered", "count", len(cmds))
}

func discordCommands() []*discordgo.ApplicationCommand {
	str := discordgo.ApplicationCommandOptionString
	return []*discordgo.ApplicationCommand{
		{
			Name:        "new",
			Description: "Create and switch workdir",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: str, Name: "project_dir", Description: "Project directory path", Required: true},
			},
		},
		{
			Name:        "cd",
			Description: "Set workdir",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: str, Name: "project_dir", Description: "Project directory path (empty = project root)", Required: false},
			},
		},
		{
			Name:        "list",
			Description: "List projects under project root",
		},
		{
			Name:        "codex",
			Description: "Use codex executor or run once",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: str, Name: "prompt", Description: "Optional prompt", Required: false},
			},
		},
		{
			Name:        "claude",
			Description: "Use claude executor or run once",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: str, Name: "prompt", Description: "Optional prompt", Required: false},
			},
		},
		{
			Name:        "mode",
			Description: "Set permission mode",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        str,
					Name:        "mode",
					Description: "sandbox or full-access",
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "sandbox", Value: "sandbox"},
						{Name: "full-access", Value: "full-access"},
					},
				},
			},
		},
		{
			Name:        "status",
			Description: "Show current session status",
		},
		{
			Name:        "reset",
			Description: "Reset current session",
		},
		{
			Name:        "stop",
			Description: "Stop running job",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: str, Name: "job_id", Description: "Job ID to stop", Required: true},
			},
		},
	}
}

const discordMaxChars = 1900

func (b *Bot) Send(_ context.Context, msg domain.OutboundMessage) error {
	if b.session == nil {
		return fmt.Errorf("discord: not connected")
	}
	target := msg.SessionKey.ChatID
	if msg.SessionKey.ThreadID != "" {
		target = msg.SessionKey.ThreadID
	}

	text := msg.Text
	if msg.Format == "html" {
		text = htmlToDiscordMarkdown(text)
	}

	// Split long messages at the character limit.
	for len(text) > discordMaxChars {
		if _, err := b.session.ChannelMessageSend(target, text[:discordMaxChars]); err != nil {
			return err
		}
		text = text[discordMaxChars:]
	}
	if text != "" {
		if _, err := b.session.ChannelMessageSend(target, text); err != nil {
			return err
		}
	}
	return nil
}

// htmlToDiscordMarkdown converts HTML-formatted text to Discord Markdown.
// It handles block-level <pre> first, then inline tags.
func htmlToDiscordMarkdown(s string) string {
	// <pre>...</pre> → ```\n...\n```
	s = rePreTag.ReplaceAllStringFunc(s, func(match string) string {
		inner := rePreTag.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		content := inner[1]
		// strip inner <code> tags if present
		content = reCodeTag.ReplaceAllString(content, "$1")
		return "```\n" + strings.TrimSpace(content) + "\n```"
	})
	// <code>...</code> → `...`
	s = reCodeTag.ReplaceAllString(s, "`$1`")
	// <b>...</b> → **...**
	s = reBoldTag.ReplaceAllString(s, "**$1**")
	// <i>...</i> → *...*
	s = reItalicTag.ReplaceAllString(s, "*$1*")
	// strip remaining tags
	s = reAnyTag.ReplaceAllString(s, "")
	// unescape HTML entities
	s = html.UnescapeString(s)
	return s
}

var (
	rePreTag    = regexp.MustCompile(`(?is)<pre>(.*?)</pre>`)
	reCodeTag   = regexp.MustCompile(`(?is)<code>(.*?)</code>`)
	reBoldTag   = regexp.MustCompile(`(?is)<b>(.*?)</b>`)
	reItalicTag = regexp.MustCompile(`(?is)<i>(.*?)</i>`)
	reAnyTag    = regexp.MustCompile(`<[^>]+>`)
)

func isThread(t discordgo.ChannelType) bool {
	return t == discordgo.ChannelTypeGuildPublicThread ||
		t == discordgo.ChannelTypeGuildPrivateThread ||
		t == discordgo.ChannelTypeGuildNewsThread
}
