package notification

import (
	"fmt"
	"os"
	"strings"
	"time"

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
	baseRetryDelay = 2 * time.Second
)

// TelegramClient handles Telegram notifications
type TelegramClient struct {
	bot             *tgbotapi.BotAPI
	archiveChannel  int64
	alertsChannel   int64
	hostname        string
	lastMessageTime time.Time // tracks last message for rate limiting (L-01 fix)
}

// NewTelegramClient creates a new Telegram client
func NewTelegramClient(botToken string, archiveChannel, alertsChannel int64) (*TelegramClient, error) {
	bot, err := tgbotapi.NewBotAPI(botToken)
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
	}, nil
}

// SendAnalysisReport sends the analysis report to Telegram channels
func (t *TelegramClient) SendAnalysisReport(analysis *ai.Analysis, stats *ai.Stats) error {
	// Format message
	message := t.formatMessage(analysis, stats)

	// Send to archive channel (always)
	if err := t.sendToChannel(t.archiveChannel, message); err != nil {
		return fmt.Errorf("failed to send to archive channel: %w", err)
	}

	// Send to alerts channel if configured and status warrants it
	if t.alertsChannel != 0 && ai.ShouldTriggerAlert(analysis.SystemStatus) {
		if err := t.sendToChannel(t.alertsChannel, message); err != nil {
			// Don't fail the whole operation if alerts channel fails
			return fmt.Errorf("failed to send to alerts channel: %w", err)
		}
	}

	return nil
}

// formatMessage formats the analysis into a Telegram message
func (t *TelegramClient) formatMessage(analysis *ai.Analysis, stats *ai.Stats) string {

	const formattedListTemplate = "%d\\. %s\n"

	var msg strings.Builder

	// Header
	msg.WriteString("🔍 *Logwatch Analysis Report*\n")
	msg.WriteString(fmt.Sprintf("🖥 Host\\: %s\n", escapeMarkdown(t.hostname)))
	msg.WriteString(fmt.Sprintf("📅 Date\\: %s\n", escapeMarkdown(time.Now().Format("2006-01-02 15:04:05"))))
	msg.WriteString(fmt.Sprintf("🌍 Timezone\\: %s\n", escapeMarkdown(time.Now().Location().String())))
	msg.WriteString(fmt.Sprintf("%s *Status\\:* %s\n\n", ai.GetStatusEmoji(analysis.SystemStatus), analysis.SystemStatus))

	// Execution Stats
	msg.WriteString("📋 *Execution Stats*\n")
	msg.WriteString(fmt.Sprintf("• Critical Issues\\: %d\n", len(analysis.CriticalIssues)))
	msg.WriteString(fmt.Sprintf("• Warnings\\: %d\n", len(analysis.Warnings)))
	msg.WriteString(fmt.Sprintf("• Recommendations\\: %d\n", len(analysis.Recommendations)))
	msg.WriteString(fmt.Sprintf("• Cost\\: %s\n", escapeMarkdown(fmt.Sprintf("$%.4f", stats.CostUSD))))
	msg.WriteString(fmt.Sprintf("• Duration\\: %s\n", escapeMarkdown(fmt.Sprintf("%.2fs", stats.DurationSeconds))))

	// Token usage details
	if stats.CacheReadTokens > 0 || stats.CacheCreationTokens > 0 {
		msg.WriteString(fmt.Sprintf("• Cache Read\\: %d tokens\n", stats.CacheReadTokens))
	}
	msg.WriteString("\n")

	// Summary
	msg.WriteString("📊 *Summary*\n")
	msg.WriteString(escapeMarkdown(analysis.Summary))
	msg.WriteString("\n\n")

	// Critical Issues
	if len(analysis.CriticalIssues) > 0 {
		msg.WriteString(fmt.Sprintf("🔴 *Critical Issues* \\(%d\\)\n", len(analysis.CriticalIssues)))
		for i, issue := range analysis.CriticalIssues {
			msg.WriteString(fmt.Sprintf(formattedListTemplate, i+1, escapeMarkdown(issue)))
		}
		msg.WriteString("\n")
	}

	// Warnings
	if len(analysis.Warnings) > 0 {
		msg.WriteString(fmt.Sprintf("⚡ *Warnings* \\(%d\\)\n", len(analysis.Warnings)))
		for i, warning := range analysis.Warnings {
			msg.WriteString(fmt.Sprintf(formattedListTemplate, i+1, escapeMarkdown(warning)))
		}
		msg.WriteString("\n")
	}

	// Recommendations
	if len(analysis.Recommendations) > 0 {
		msg.WriteString("💡 *Recommendations*\n")
		for i, rec := range analysis.Recommendations {
			msg.WriteString(fmt.Sprintf(formattedListTemplate, i+1, escapeMarkdown(rec)))
		}
		msg.WriteString("\n")
	}

	// Key Metrics
	if len(analysis.Metrics) > 0 {
		msg.WriteString("📈 *Key Metrics*\n")
		for key, value := range analysis.Metrics {
			valueStr := fmt.Sprintf("%v", value)
			msg.WriteString(fmt.Sprintf("• %s\\: %s\n", escapeMarkdown(key), escapeMarkdown(valueStr)))
		}
	}

	return msg.String()
}

// sendToChannel sends a message to a Telegram channel with rate limiting (L-01 fix)
func (t *TelegramClient) sendToChannel(channelID int64, message string) error {
	// Split message if it exceeds Telegram's limit
	messages := t.splitMessage(message)

	for _, msg := range messages {
		// Apply rate limiting before sending (L-01 fix)
		t.waitForRateLimit()

		msgConfig := tgbotapi.NewMessage(channelID, msg)
		msgConfig.ParseMode = "MarkdownV2"

		// Send with exponential backoff retry
		if err := t.sendWithRetry(msgConfig); err != nil {
			return err
		}

		// Update last message time for rate limiting
		t.lastMessageTime = time.Now()
	}

	return nil
}

// waitForRateLimit ensures minimum interval between messages (L-01 fix)
func (t *TelegramClient) waitForRateLimit() {
	if t.lastMessageTime.IsZero() {
		return
	}

	elapsed := time.Since(t.lastMessageTime)
	if elapsed < minMessageInterval {
		time.Sleep(minMessageInterval - elapsed)
	}
}

// sendWithRetry sends a message with exponential backoff retry (L-01 fix)
func (t *TelegramClient) sendWithRetry(msgConfig tgbotapi.MessageConfig) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		_, err := t.bot.Send(msgConfig)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if this is a rate limit error (429)
		if isRateLimitError(err) {
			// Wait longer for rate limit errors
			retryAfter := extractRetryAfter(err)
			if retryAfter > 0 {
				time.Sleep(time.Duration(retryAfter) * time.Second)
				continue
			}
		}

		// Exponential backoff for other errors
		if attempt < maxRetries {
			delay := baseRetryDelay * time.Duration(1<<(attempt-1)) // 2s, 4s, 8s...
			time.Sleep(delay)
		}
	}

	// Sanitize error to prevent credentials from appearing in error messages (M-01 fix)
	return internalerrors.Wrapf(lastErr, "failed to send message after %d retries", maxRetries)
}

// isRateLimitError checks if the error is a Telegram rate limit error (429)
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "429") || strings.Contains(errStr, "Too Many Requests")
}

// extractRetryAfter extracts the retry_after value from a rate limit error
func extractRetryAfter(err error) int {
	if err == nil {
		return 0
	}

	// Telegram API errors typically include retry_after in the message
	// Example: "Too Many Requests: retry after 30"
	errStr := err.Error()

	// Simple extraction - look for "retry after X" pattern
	if idx := strings.Index(strings.ToLower(errStr), "retry after "); idx != -1 {
		remaining := errStr[idx+len("retry after "):]
		var seconds int
		if _, err := fmt.Sscanf(remaining, "%d", &seconds); err == nil {
			return seconds
		}
	}

	// Default to a conservative wait time if we can't extract the value
	return 30
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

			// If a single line is too long, split it
			if len(line) > maxMessageLength {
				for i := 0; i < len(line); i += maxMessageLength {
					end := i + maxMessageLength
					if end > len(line) {
						end = len(line)
					}
					messages = append(messages, line[i:end])
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

// escapeMarkdown escapes special characters for Telegram MarkdownV2
func escapeMarkdown(text string) string {
	// Characters that need to be escaped in MarkdownV2
	// See: https://core.telegram.org/bots/api#markdownv2-style
	specialChars := []string{
		"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!", ":",
	}

	result := text
	for _, char := range specialChars {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}

	return result
}

// GetBotInfo returns information about the bot
func (t *TelegramClient) GetBotInfo() map[string]interface{} {
	return map[string]interface{}{
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
