package dailyreport

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultScheduleTime  = "08:00"
	MaxFocusLength       = 2000
	MaxTitleLength       = 80
	MaxOutlineSections   = 12
	MaxInstructionLength = 500
)

var allowedTitleVariables = map[string]struct{}{
	"date":          {},
	"start_time":    {},
	"end_time":      {},
	"article_count": {},
}

func parseScheduleTime(value string) (int, int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, fmt.Errorf("schedule_time must use HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("schedule_time hour is invalid")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("schedule_time minute is invalid")
	}
	return hour, minute, nil
}

func validateTitleTemplate(value string) error {
	if len([]rune(value)) > MaxTitleLength {
		return fmt.Errorf("title_template must not exceed %d characters", MaxTitleLength)
	}
	remaining := value
	for {
		start := strings.Index(remaining, "{{")
		if start < 0 {
			if strings.Contains(remaining, "}}") || strings.Contains(remaining, "{") || strings.Contains(remaining, "}") {
				return fmt.Errorf("title_template contains malformed braces")
			}
			break
		}
		if strings.ContainsAny(remaining[:start], "{}") {
			return fmt.Errorf("title_template contains malformed braces")
		}
		end := strings.Index(remaining[start+2:], "}}")
		if end < 0 {
			return fmt.Errorf("title_template contains an unclosed variable")
		}
		name := remaining[start+2 : start+2+end]
		if _, ok := allowedTitleVariables[name]; !ok {
			return fmt.Errorf("title_template variable %q is not supported", name)
		}
		remaining = remaining[start+2+end+2:]
	}
	return nil
}

func normalizeLanguage(value string) (string, error) {
	if value == "" {
		return "auto", nil
	}
	switch value {
	case "auto", "zh-CN", "en":
		return value, nil
	default:
		return "", fmt.Errorf("language must be auto, zh-CN, or en")
	}
}

func formatTitle(template string, start, end time.Time, articleCount int) string {
	if template == "" {
		template = "24 小时 AI 日报 · {{date}}"
	}
	replacer := strings.NewReplacer(
		"{{date}}", end.Format("2006-01-02"),
		"{{start_time}}", start.Format(time.RFC3339),
		"{{end_time}}", end.Format(time.RFC3339),
		"{{article_count}}", strconv.Itoa(articleCount),
	)
	return replacer.Replace(template)
}
