package domain

import (
	"errors"
	"time"
)

type BroadcastTargetMode string

const (
	BroadcastTargetAll      BroadcastTargetMode = "all"
	BroadcastTargetSelected BroadcastTargetMode = "selected"
)

type BroadcastRecipientStatus string

const (
	BroadcastRecipientPending    BroadcastRecipientStatus = "pending"
	BroadcastRecipientProcessing BroadcastRecipientStatus = "processing"
	BroadcastRecipientSent       BroadcastRecipientStatus = "sent"
	BroadcastRecipientFailed     BroadcastRecipientStatus = "failed"
)

const (
	MaxBroadcastMessageBytes       = 4096
	MaxBroadcastSelectedRecipients = 200
	MaxBroadcastRecipientAttempts  = 5
)

type Broadcast struct {
	ID                int64
	Message           string
	TargetMode        BroadcastTargetMode
	TargetCount       int64
	MaterializedCount int64
	SentCount         int64
	FailedCount       int64
	EnumerationDone   bool
	CreatedBy         string
	CreatedAt         time.Time
}

var (
	ErrBroadcastInvalid          = errors.New("broadcast invalid")
	ErrBroadcastMessageEmpty     = errors.New("broadcast message is empty")
	ErrBroadcastMessageTooLong   = errors.New("broadcast message is too long")
	ErrBroadcastNoRecipients     = errors.New("broadcast has no recipients")
	ErrBroadcastRecipientInvalid = errors.New("broadcast recipient invalid")
	ErrBroadcastLeaseLost        = errors.New("broadcast recipient lease lost")
)
