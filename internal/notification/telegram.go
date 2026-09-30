// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package notification delivers analysis results via Telegram.
package notification

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/olegiv/logwatch-ai-go/internal/ai"
	internalerrors "github.com/olegiv/logwatch-ai-go/internal/errors"
)

const (
	maxMessageLength = 4096
	// minMessageInterval is the minimum time between messages to the same channel
	// to avoid Telegram rate limits (L-01 fix)
	minMessageInterval = 1 * time.Second
	// maxRetries is the maximum number of retry attempts for sending messages
	maxRetries = 3
	// baseRetryDelay is the initial delay between retries (doubles each attempt)
	baseRetryDelay  = 2 * time.Second
	requestTimeout  = 30 * time.Second
	maxRetryAfter   = 30 * time.Second
	telegramAPIHost = "api.telegram.org"
)

type contextHTTPClient struct {
	ctx    context.Context
	client *http.Client
}

func (c *contextHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if err := validateTelegramRequest(req); err != nil {
		return nil, err
	}
	// #nosec G704 -- the request and redirects are restricted to Telegram's
	// fixed HTTPS API host immediately above and in CheckRedirect.
	return c.client.Do(req.WithContext(c.ctx))
}

func validateTelegramRequest(req *http.Request) error {
	if req.URL.Scheme != "https" || req.URL.Hostname() != telegramAPIHost {
		return fmt.Errorf("refusing non-Telegram API request")
	}
	return nil
}

// TelegramClient handles Telegram notifications
type TelegramClient struct {
	bot             *tgbotapi.BotAPI
	archiveChannel  int64
	alertsChannel   int64
	hostname        string
	lastMessageTime time.Time // tracks last message for rate limiting (L-01 fix)
	ctx             context.Context
}

// PartialDeliveryError reports that at least one durable Telegram side effect
// occurred before delivery failed. Callers must not rerun the full analysis,
// because doing so would duplicate messages, storage, and LLM cost.
type PartialDeliveryError struct {
	Destination    string
	DeliveredParts int
	TotalParts     int
	Err            error
}

func (e *PartialDeliveryError) Error() string {
	return fmt.Sprintf(
		"partial Telegram delivery to %s (%d/%d parts): %v",
		e.Destination,
		e.DeliveredParts,
		e.TotalParts,
		e.Err,
	)
}

func (e *PartialDeliveryError) Unwrap() error {
	return e.Err
}

// IsPartialDelivery reports whether err represents a non-retry-safe partial
// success rather than a total notification failure.
func IsPartialDelivery(err error) bool {
	var partial *PartialDeliveryError
	return errors.As(err, &partial)
}

// NewTelegramClient creates a new Telegram client
func NewTelegramClient(ctx context.Context, botToken string, archiveChannel, alertsChannel int64) (*TelegramClient, error) {
	return newTelegramClient(ctx, botToken, archiveChannel, alertsChannel, nil)
}

func newTelegramClient(
	ctx context.Context,
	botToken string,
	archiveChannel, alertsChannel int64,
	baseClient *http.Client,
) (*TelegramClient, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if baseClient == nil {
		baseClient = &http.Client{}
	}
	configuredClient := *baseClient
	configuredClient.Timeout = requestTimeout
	configuredClient.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		return validateTelegramRequest(req)
	}
	httpClient := &contextHTTPClient{
		ctx:    ctx,
		client: &configuredClient,
	}
	bot, err := tgbotapi.NewBotAPIWithClient(botToken, tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		// Sanitize error to prevent bot token from appearing in error messages (M-01 fix)
		return nil, internalerrors.Wrapf(err, "failed to create Telegram bot")
	}

	// Get hostname for reports
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	return &TelegramClient{
		bot:            bot,
		archiveChannel: archiveChannel,
		alertsChannel:  alertsChannel,
		hostname:       hostname,
		ctx:            ctx,
	}, nil
}

// SendAnalysisReport sends the analysis report to Telegram channels
// siteName is optional and used for multi-site Drupal deployments to identify the site in the report.
func (t *TelegramClient) SendAnalysisReport(analysis *ai.Analysis, stats *ai.Stats, logSourceType, siteName string) error {
	// Format message
	message := t.formatMessage(analysis, stats, logSourceType, siteName)
	parts := len(t.splitMessage(message))

	// Send to archive channel (always). Only a failure before any part was
	// delivered is fatal; a partial archive must not suppress the alert.
	archiveErr := t.sendToChannel(t.archiveChannel, message)
	if archiveErr != nil && !IsPartialDelivery(archiveErr) {
		return fmt.Errorf("failed to send to archive channel: %w", archiveErr)
	}

	// Send to alerts channel if configured and status warrants it
	sendAlerts := t.alertsChannel != 0 && ai.ShouldTriggerAlert(analysis.SystemStatus)
	var alertsErr error
	if sendAlerts {
		alertsErr = t.sendToChannel(t.alertsChannel, message)
	}
	if archiveErr == nil && alertsErr == nil {
		return nil
	}

	delivered, outcome := channelOutcome("archive", archiveErr, parts)
	total := parts
	var errs []error
	if archiveErr != nil {
		errs = append(errs, fmt.Errorf("failed to send to archive channel: %w", archiveErr))
	}
	if sendAlerts {
		alertsDelivered, alertsOutcome := channelOutcome("alerts", alertsErr, parts)
		delivered += alertsDelivered
		total += parts
		outcome += "; " + alertsOutcome
		if alertsErr != nil {
			errs = append(errs, fmt.Errorf("failed to send to alerts channel: %w", alertsErr))
		}
	}
	return &PartialDeliveryError{
		Destination:    "Telegram channels (" + outcome + ")",
		DeliveredParts: delivered,
		TotalParts:     total,
		Err:            errors.Join(errs...),
	}
}

// channelOutcome reports how many of a channel's message parts were delivered
// and describes the result for a PartialDeliveryError.
func channelOutcome(channel string, err error, parts int) (int, string) {
	if err == nil {
		return parts, channel + " delivered"
	}
	if partial, ok := errors.AsType[*PartialDeliveryError](err); ok {
		return partial.DeliveredParts, channel + " partially delivered"
	}
	return 0, channel + " failed"
}

// writeSection writes a section with a header and numbered items to the message builder.
// If showCount is true, the count is appended to the header.
func writeSection(msg *strings.Builder, emoji, title string, items []string, showCount bool) {
	if len(items) == 0 {
		return
	}
	if showCount {
		fmt.Fprintf(msg, "%s *%s* \\(%d\\)\n", emoji, title, len(items))
	} else {
		fmt.Fprintf(msg, "%s *%s*\n", emoji, title)
	}
	for i, item := range items {
		fmt.Fprintf(msg, "%d\\. %s\n", i+1, escapeMarkdown(item))
	}
	msg.WriteString("\n")
}

// formatMessage formats the analysis into a Telegram message
func (t *TelegramClient) formatMessage(analysis *ai.Analysis, stats *ai.Stats, logSourceType, siteName string) string {
	var msg strings.Builder

	// Header with log source type and optional site name
	sourceDisplayName := getLogSourceDisplayName(logSourceType)
	if siteName != "" {
		fmt.Fprintf(&msg, "🔍 *%s Report* \\- %s\n", sourceDisplayName, escapeMarkdown(siteName))
	} else {
		fmt.Fprintf(&msg, "🔍 *%s Report*\n", sourceDisplayName)
	}
	fmt.Fprintf(&msg, "🖥 Host\\: %s\n", escapeMarkdown(t.hostname))
	fmt.Fprintf(&msg, "📅 Date\\: %s\n", escapeMarkdown(time.Now().Format("2006-01-02 15:04:05")))
	fmt.Fprintf(&msg, "🌍 Timezone\\: %s\n", escapeMarkdown(time.Now().Location().String()))
	fmt.Fprintf(&msg, "%s *Status\\:* %s\n\n", ai.GetStatusEmoji(analysis.SystemStatus), analysis.SystemStatus)

	// Execution Stats
	msg.WriteString("📋 *Execution Stats*\n")
	fmt.Fprintf(&msg, "• LLM\\: %s \\(%s\\)\n", escapeMarkdown(stats.Model), escapeMarkdown(stats.Provider))
	fmt.Fprintf(&msg, "• Critical Issues\\: %d\n", len(analysis.CriticalIssues))
	fmt.Fprintf(&msg, "• Warnings\\: %d\n", len(analysis.Warnings))
	fmt.Fprintf(&msg, "• Recommendations\\: %d\n", len(analysis.Recommendations))
	fmt.Fprintf(&msg, "• Cost\\: %s\n", escapeMarkdown(fmt.Sprintf("$%.4f", stats.CostUSD)))
	fmt.Fprintf(&msg, "• Duration\\: %s\n", escapeMarkdown(fmt.Sprintf("%.2fs", stats.DurationSeconds)))

	// Token usage details
	if stats.CacheReadTokens > 0 || stats.CacheCreationTokens > 0 {
		fmt.Fprintf(&msg, "• Cache Read\\: %d tokens\n", stats.CacheReadTokens)
	}
	msg.WriteString("\n")

	// Summary
	msg.WriteString("📊 *Summary*\n")
	msg.WriteString(escapeMarkdown(analysis.Summary))
	msg.WriteString("\n\n")

	// Critical Issues, Warnings, Recommendations
	writeSection(&msg, "🔴", "Critical Issues", analysis.CriticalIssues, true)
	writeSection(&msg, "⚡", "Warnings", analysis.Warnings, true)
	writeSection(&msg, "💡", "Recommendations", analysis.Recommendations, false)

	// Key Metrics
	if len(analysis.Metrics) > 0 {
		msg.WriteString("📈 *Key Metrics*\n")
		for key, value := range analysis.Metrics {
			valueStr := fmt.Sprintf("%v", value)
			fmt.Fprintf(&msg, "• %s\\: %s\n", escapeMarkdown(key), escapeMarkdown(valueStr))
		}
	}

	return msg.String()
}

// sendToChannel sends a message to a Telegram channel with rate limiting (L-01 fix)
func (t *TelegramClient) sendToChannel(channelID int64, message string) error {
	// Split message if it exceeds Telegram's limit
	messages := t.splitMessage(message)

	for i, msg := range messages {
		// Apply rate limiting before sending (L-01 fix)
		if err := t.waitForRateLimit(); err != nil {
			return channelDeliveryError(channelID, i, len(messages), err)
		}

		msgConfig := tgbotapi.NewMessage(channelID, msg)
		msgConfig.ParseMode = "MarkdownV2"

		// Send with exponential backoff retry
		if err := t.sendWithRetry(msgConfig); err != nil {
			return channelDeliveryError(channelID, i, len(messages), err)
		}

		// Update last message time for rate limiting
		t.lastMessageTime = time.Now()
	}

	return nil
}

func channelDeliveryError(channelID int64, deliveredParts, totalParts int, err error) error {
	if deliveredParts == 0 {
		return err
	}
	return &PartialDeliveryError{
		Destination:    fmt.Sprintf("channel %d", channelID),
		DeliveredParts: deliveredParts,
		TotalParts:     totalParts,
		Err:            err,
	}
}

// waitForRateLimit ensures minimum interval between messages (L-01 fix)
func (t *TelegramClient) waitForRateLimit() error {
	if t.lastMessageTime.IsZero() {
		return nil
	}

	elapsed := time.Since(t.lastMessageTime)
	if elapsed < minMessageInterval {
		return waitForContext(t.requestContext(), minMessageInterval-elapsed)
	}
	return nil
}

// sendWithRetry sends a message with exponential backoff retry (L-01 fix)
func (t *TelegramClient) sendWithRetry(msgConfig tgbotapi.MessageConfig) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := t.requestContext().Err(); err != nil {
			return err
		}
		_, err := t.bot.Send(msgConfig)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if this is a rate limit error (429)
		if isRateLimitError(err) && attempt < maxRetries {
			// Wait longer for rate limit errors
			retryAfter := extractRetryAfter(err)
			if retryAfter > 0 {
				if err := waitForContext(t.requestContext(), time.Duration(retryAfter)*time.Second); err != nil {
					return err
				}
				continue
			}
		}

		// Exponential backoff for other errors
		if attempt < maxRetries {
			delay := baseRetryDelay * time.Duration(1<<(attempt-1)) // 2s, 4s, 8s...
			if err := waitForContext(t.requestContext(), delay); err != nil {
				return err
			}
		}
	}

	// Sanitize error to prevent credentials from appearing in error messages (M-01 fix)
	return internalerrors.Wrapf(lastErr, "failed to send message after %d retries", maxRetries)
}

func (t *TelegramClient) requestContext() context.Context {
	if t.ctx == nil {
		return context.Background()
	}
	return t.ctx
}

func waitForContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// isRateLimitError checks if the error is a Telegram rate limit error (429)
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	if telegramErr, ok := errors.AsType[*tgbotapi.Error](err); ok {
		return telegramErr.Code == http.StatusTooManyRequests
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "429") || strings.Contains(errStr, "too many requests")
}

// extractRetryAfter extracts the retry_after value from a rate limit error
func extractRetryAfter(err error) int {
	if err == nil {
		return 0
	}
	if telegramErr, ok := errors.AsType[*tgbotapi.Error](err); ok && telegramErr.RetryAfter > 0 {
		return capRetryAfter(telegramErr.RetryAfter)
	}

	// Telegram API errors typically include retry_after in the message
	// Example: "Too Many Requests: retry after 30"
	errStr := err.Error()

	// Simple extraction - look for "retry after X" pattern
	if idx := strings.Index(strings.ToLower(errStr), "retry after "); idx != -1 {
		remaining := errStr[idx+len("retry after "):]
		var seconds int
		if _, err := fmt.Sscanf(remaining, "%d", &seconds); err == nil {
			return capRetryAfter(seconds)
		}
	}

	// Default to a conservative wait time if we can't extract the value
	return 30
}

func capRetryAfter(seconds int) int {
	maxSeconds := int(maxRetryAfter / time.Second)
	if seconds > maxSeconds {
		return maxSeconds
	}
	return seconds
}

// splitMessage splits a long message into multiple messages
func (t *TelegramClient) splitMessage(message string) []string {
	if len(message) <= maxMessageLength {
		return []string{message}
	}

	var messages []string
	lines := strings.Split(message, "\n")
	var currentMsg strings.Builder

	for _, line := range lines {
		// If adding this line would exceed the limit
		if currentMsg.Len()+len(line)+1 > maxMessageLength {
			// Save current message
			if currentMsg.Len() > 0 {
				messages = append(messages, currentMsg.String())
				currentMsg.Reset()
			}

			// If a single line is too long, split it on rune boundaries
			// so we never emit a message that ends mid-UTF-8-sequence
			// (invalid UTF-8 breaks Telegram MarkdownV2 parsing).
			if len(line) >= maxMessageLength {
				var chunk strings.Builder
				chunk.Grow(maxMessageLength)
				for _, r := range line {
					if chunk.Len()+utf8.RuneLen(r) > maxMessageLength {
						flushed := chunk.String()
						chunk.Reset()
						// Never end a chunk on an unpaired trailing backslash:
						// escapeMarkdown emits backslashes only in pairs, so an
						// odd trailing run means this boundary would split an
						// escape sequence — the chunk would end with a dangling
						// `\` and the next would start with an unescaped special
						// character, and Telegram rejects both with 400 "can't
						// parse entities". Carry the dangling backslash over.
						if countTrailingBackslashes(flushed)%2 == 1 {
							flushed = flushed[:len(flushed)-1]
							chunk.WriteByte('\\')
						}
						if flushed != "" {
							messages = append(messages, flushed)
						}
					}
					chunk.WriteRune(r)
				}
				if chunk.Len() > 0 {
					messages = append(messages, chunk.String())
				}
				continue
			}
		}

		currentMsg.WriteString(line)
		currentMsg.WriteString("\n")
	}

	// Add remaining content
	if currentMsg.Len() > 0 {
		messages = append(messages, currentMsg.String())
	}

	return messages
}

// getLogSourceDisplayName returns a human-readable display name for log source types
func getLogSourceDisplayName(logSourceType string) string {
	switch logSourceType {
	case "logwatch":
		return "Logwatch"
	case "drupal_watchdog":
		return "Drupal Watchdog"
	case "ocms":
		return "OCMS"
	default:
		return "Log"
	}
}

// countTrailingBackslashes returns the number of consecutive backslash bytes
// at the end of s. Safe on UTF-8 input: '\\' (0x5C) is ASCII and never
// appears as a continuation byte of a multi-byte rune.
func countTrailingBackslashes(s string) int {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n
}

// escapeMarkdown escapes special characters for Telegram MarkdownV2
func escapeMarkdown(text string) string {
	// Characters that need to be escaped in MarkdownV2
	// See: https://core.telegram.org/bots/api#markdownv2-style
	// IMPORTANT: Backslash MUST be first to prevent double-escaping issues.
	// If text contains `\.` and we escape `.` first, we get `\\.` which means
	// "escaped backslash + unescaped dot". By escaping `\` first, we get `\\\.`
	// which correctly means "escaped backslash + escaped dot".
	specialChars := []string{
		"\\", // Must be first!
		"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!", ":",
	}

	result := text
	for _, char := range specialChars {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}

	return result
}

// SendNoEntriesReport sends an informational message when no log entries were found.
// This is used for Drupal watchdog when there are no entries for the analyzed time period.
// siteName is optional and used for multi-site Drupal deployments.
func (t *TelegramClient) SendNoEntriesReport(logSourceType, siteName string) error {
	var msg strings.Builder

	// Header with log source type and optional site name
	sourceDisplayName := getLogSourceDisplayName(logSourceType)
	if siteName != "" {
		fmt.Fprintf(&msg, "ℹ️ *%s Report* \\- %s\n", sourceDisplayName, escapeMarkdown(siteName))
	} else {
		fmt.Fprintf(&msg, "ℹ️ *%s Report*\n", sourceDisplayName)
	}
	fmt.Fprintf(&msg, "🖥 Host\\: %s\n", escapeMarkdown(t.hostname))
	fmt.Fprintf(&msg, "📅 Date\\: %s\n", escapeMarkdown(time.Now().Format("2006-01-02 15:04:05")))
	fmt.Fprintf(&msg, "🌍 Timezone\\: %s\n\n", escapeMarkdown(time.Now().Location().String()))

	msg.WriteString("📭 *No Entries Found*\n\n")
	msg.WriteString("No log entries were found for the analyzed time period \\(yesterday\\)\\.\n")
	msg.WriteString("This is normal if no events occurred during this period\\.\n\n")
	msg.WriteString("_No AI analysis was performed\\._")

	// Send to archive channel only (not alerts - this is not an alert condition)
	if err := t.sendToChannel(t.archiveChannel, msg.String()); err != nil {
		return fmt.Errorf("failed to send no-entries report to archive channel: %w", err)
	}

	return nil
}

// GetBotInfo returns information about the bot
func (t *TelegramClient) GetBotInfo() map[string]any {
	return map[string]any{
		"username":        t.bot.Self.UserName,
		"archive_channel": t.archiveChannel,
		"alerts_channel":  t.alertsChannel,
		"hostname":        t.hostname,
	}
}

// Close closes the Telegram client
func (t *TelegramClient) Close() error {
	t.bot.StopReceivingUpdates()
	return nil
}
