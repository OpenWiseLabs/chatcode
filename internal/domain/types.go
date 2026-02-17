package domain

import (
	"context"
	"time"
)

type Platform string

const (
	PlatformTelegram Platform = "telegram"
	PlatformWhatsApp Platform = "whatsapp"
)

type SessionKey struct {
	Platform Platform
	ChatID   string
	ThreadID string
}

func (k SessionKey) String() string {
	if k.ThreadID == "" {
		return string(k.Platform) + ":" + k.ChatID
	}
	return string(k.Platform) + ":" + k.ChatID + ":" + k.ThreadID
}

type Message struct {
	SessionKey SessionKey
	SenderID   string
	Text       string
	Meta       InboundMessageMeta
	At         time.Time
}

type InboundMessageMeta struct {
	ReplyToMessageID string
	Raw              map[string]string
}

type OutboundMessage struct {
	SessionKey SessionKey
	Text       string
	Format     string
	Meta       map[string]string
}

type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
	JobStopped JobStatus = "stopped"
)

type Job struct {
	ID           string
	SessionKey   SessionKey
	Executor     string
	Session      string
	Prompt       string
	Workdir      string
	Status       JobStatus
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	ErrorMessage string
}

type StreamEvent struct {
	JobID    string
	Seq      int64
	Chunk    string
	IsFinal  bool
	TS       time.Time
	Stream   string
	ExitCode *int
}

type Transport interface {
	Name() string
	Start(context.Context, MessageHandler) error
	Send(context.Context, OutboundMessage) error
}

type MessageHandler func(context.Context, Message) error
