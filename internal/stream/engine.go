package stream

import (
	"context"
	"io"
	"time"

	"ds2api/internal/sse"
)

type StopReason string

const (
	StopReasonNone              StopReason = ""
	StopReasonContextCancelled  StopReason = "context_cancelled"
	StopReasonNoContentTimeout  StopReason = "no_content_timeout"
	StopReasonIdleTimeout       StopReason = "idle_timeout"
	StopReasonMaxDuration       StopReason = "max_duration"
	StopReasonUpstreamCompleted StopReason = "upstream_completed"
	StopReasonHandlerRequested  StopReason = "handler_requested"
)

type ConsumeConfig struct {
	Context             context.Context
	Body                io.Reader
	ThinkingEnabled     bool
	InitialType         string
	KeepAliveInterval   time.Duration
	IdleTimeout         time.Duration
	MaxDuration         time.Duration
	MaxKeepAliveNoInput int
}

type ParsedDecision struct {
	Stop        bool
	StopReason  StopReason
	ContentSeen bool
}

type ConsumeHooks struct {
	OnParsed      func(parsed sse.LineResult) ParsedDecision
	OnKeepAlive   func()
	OnFinalize    func(reason StopReason, scannerErr error)
	OnContextDone func()
}

func ConsumeSSE(cfg ConsumeConfig, hooks ConsumeHooks) {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	initialType := cfg.InitialType
	if initialType == "" {
		if cfg.ThinkingEnabled {
			initialType = "thinking"
		} else {
			initialType = "text"
		}
	}
	parsedLines, done := sse.StartParsedLinePump(cfg.Context, cfg.Body, cfg.ThinkingEnabled, initialType)

	var ticker *time.Ticker
	if cfg.KeepAliveInterval > 0 {
		ticker = time.NewTicker(cfg.KeepAliveInterval)
		defer ticker.Stop()
	}
	var maxTimer *time.Timer
	var maxDurationCh <-chan time.Time
	if cfg.MaxDuration > 0 {
		maxTimer = time.NewTimer(cfg.MaxDuration)
		maxDurationCh = maxTimer.C
		defer maxTimer.Stop()
	}

	hasContent := false
	lastContent := time.Now()
	keepaliveCount := 0

	finalize := func(reason StopReason, scannerErr error) {
		if hooks.OnFinalize != nil {
			hooks.OnFinalize(reason, scannerErr)
		}
	}

	for {
		select {
		case <-cfg.Context.Done():
			if hooks.OnContextDone != nil {
				hooks.OnContextDone()
			}
			return
		case <-maxDurationCh:
			finalize(StopReasonMaxDuration, nil)
			return
		case <-tickCh(ticker):
			if !hasContent {
				keepaliveCount++
				if cfg.MaxKeepAliveNoInput > 0 && keepaliveCount >= cfg.MaxKeepAliveNoInput {
					finalize(StopReasonNoContentTimeout, nil)
					return
				}
			}
			if hasContent && cfg.IdleTimeout > 0 && time.Since(lastContent) > cfg.IdleTimeout {
				finalize(StopReasonIdleTimeout, nil)
				return
			}
			if hooks.OnKeepAlive != nil {
				hooks.OnKeepAlive()
			}
		case parsed, ok := <-parsedLines:
			if !ok {
				finalize(StopReasonUpstreamCompleted, <-done)
				return
			}
			if hooks.OnParsed == nil {
				continue
			}
			decision := hooks.OnParsed(parsed)
			if decision.ContentSeen {
				hasContent = true
				lastContent = time.Now()
				keepaliveCount = 0
			}
			if decision.Stop {
				reason := decision.StopReason
				if reason == StopReasonNone {
					reason = StopReasonHandlerRequested
				}
				finalize(reason, nil)
				return
			}
		}
	}
}

func tickCh(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}
