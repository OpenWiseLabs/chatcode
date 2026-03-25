package discord

import (
	"context"
	"testing"

	"github.com/bwmarrin/discordgo"

	"chatcode/internal/domain"
)

func TestIsThread(t *testing.T) {
	cases := []struct {
		t    discordgo.ChannelType
		want bool
	}{
		{discordgo.ChannelTypeGuildPublicThread, true},
		{discordgo.ChannelTypeGuildPrivateThread, true},
		{discordgo.ChannelTypeGuildNewsThread, true},
		{discordgo.ChannelTypeGuildText, false},
		{discordgo.ChannelTypeDM, false},
	}
	for _, tc := range cases {
		got := isThread(tc.t)
		if got != tc.want {
			t.Errorf("isThread(%v) = %v, want %v", tc.t, got, tc.want)
		}
	}
}

func TestBotName(t *testing.T) {
	b := New("token", nil)
	if b.Name() != "discord" {
		t.Errorf("Name() = %q, want %q", b.Name(), "discord")
	}
}

func TestSendWithoutSession(t *testing.T) {
	b := New("token", nil)
	// session is nil before Start — Send must return an error.
	err := b.Send(context.Background(), domain.OutboundMessage{
		SessionKey: domain.SessionKey{Platform: domain.PlatformDiscord, ChatID: "123"},
		Text:       "hello",
	})
	if err == nil {
		t.Error("expected error when session is nil")
	}
}

func TestNewAllowedChannelIDs(t *testing.T) {
	b := New("token", []string{"111", "222", ""})
	if _, ok := b.allowedChannelIDs["111"]; !ok {
		t.Error("expected 111 in allowedChannelIDs")
	}
	if _, ok := b.allowedChannelIDs["222"]; !ok {
		t.Error("expected 222 in allowedChannelIDs")
	}
	// empty string should be skipped
	if len(b.allowedChannelIDs) != 2 {
		t.Errorf("expected 2 entries, got %d", len(b.allowedChannelIDs))
	}
}

func TestNewEmptyAllowedChannelIDs(t *testing.T) {
	b := New("token", nil)
	if len(b.allowedChannelIDs) != 0 {
		t.Errorf("expected empty map, got %v", b.allowedChannelIDs)
	}
}

func TestHtmlToDiscordMarkdown(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			"<b>tool_name</b>",
			"**tool_name**",
		},
		{
			"<code>input</code>",
			"`input`",
		},
		{
			"<b>tool</b>: <code>arg</code>",
			"**tool**: `arg`",
		},
		{
			"<pre><code>some code</code></pre>",
			"```\nsome code\n```",
		},
		{
			"<pre>plain block</pre>",
			"```\nplain block\n```",
		},
		{
			"plain text",
			"plain text",
		},
		{
			"&amp; &lt;test&gt;",
			"& <test>",
		},
		{
			"<i>italic</i>",
			"*italic*",
		},
	}
	for _, tc := range cases {
		got := htmlToDiscordMarkdown(tc.input)
		if got != tc.want {
			t.Errorf("htmlToDiscordMarkdown(%q)\n got  %q\n want %q", tc.input, got, tc.want)
		}
	}
}

func TestDiscordCommandsCount(t *testing.T) {
	cmds := discordCommands()
	// 9 commands: new, cd, list, codex, claude, mode, status, reset, stop
	const want = 9
	if len(cmds) != want {
		t.Errorf("discordCommands() returned %d commands, want %d", len(cmds), want)
	}
}

func TestDiscordCommandNames(t *testing.T) {
	expected := map[string]bool{
		"new": true, "cd": true, "list": true, "codex": true,
		"claude": true, "mode": true, "status": true, "reset": true, "stop": true,
	}
	for _, cmd := range discordCommands() {
		if !expected[cmd.Name] {
			t.Errorf("unexpected command: %q", cmd.Name)
		}
		delete(expected, cmd.Name)
	}
	for name := range expected {
		t.Errorf("missing command: %q", name)
	}
}
