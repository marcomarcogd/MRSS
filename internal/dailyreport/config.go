package dailyreport

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"MRSS/internal/models"
)

func normalizeConfig(config *models.DailyReportConfig, now time.Time) error {
	if config.ScheduleTime == "" {
		config.ScheduleTime = DefaultScheduleTime
	}
	if _, _, err := parseScheduleTime(config.ScheduleTime); err != nil {
		return err
	}
	if config.FeedScope == "" {
		config.FeedScope = "all"
	}
	if config.FeedScope != "all" && config.FeedScope != "selected" {
		return fmt.Errorf("feed_scope must be all or selected")
	}
	if config.ArticleSummaryMode == "" {
		config.ArticleSummaryMode = "ai"
	}
	if config.ArticleSummaryMode != "ai" && config.ArticleSummaryMode != "local" {
		return fmt.Errorf("article_summary_mode must be ai or local")
	}
	if len([]rune(config.Focus)) > MaxFocusLength {
		return fmt.Errorf("focus must not exceed %d characters", MaxFocusLength)
	}
	language, err := normalizeLanguage(config.Language)
	if err != nil {
		return err
	}
	config.Language = language
	if err := validateTitleTemplate(config.TitleTemplate); err != nil {
		return err
	}
	outline, err := parseOutline(config.OutlineJSON)
	if err != nil {
		return fmt.Errorf("outline is invalid: %w", err)
	}
	if len(outline) == 0 {
		outline = defaultOutline()
	}
	if len(outline) < 1 || len(outline) > MaxOutlineSections {
		return fmt.Errorf("outline must contain 1 to %d sections", MaxOutlineSections)
	}
	seen := make(map[string]struct{}, len(outline))
	for i := range outline {
		outline[i].ID = strings.TrimSpace(outline[i].ID)
		outline[i].Title = strings.TrimSpace(outline[i].Title)
		outline[i].Instruction = strings.TrimSpace(outline[i].Instruction)
		if outline[i].ID == "" {
			outline[i].ID = fmt.Sprintf("section-%d", i+1)
		}
		if outline[i].Title == "" {
			return fmt.Errorf("outline section %d title is required", i+1)
		}
		if len([]rune(outline[i].Instruction)) > MaxInstructionLength {
			return fmt.Errorf("outline section %d instruction must not exceed %d characters", i+1, MaxInstructionLength)
		}
		if _, exists := seen[outline[i].ID]; exists {
			return fmt.Errorf("outline section id %q is duplicated", outline[i].ID)
		}
		seen[outline[i].ID] = struct{}{}
	}
	normalizedOutline, _ := json.Marshal(outline)
	config.OutlineJSON = string(normalizedOutline)
	if config.TitleTemplate == "" {
		config.TitleTemplate = "24 小时 AI 日报 · {{date}}"
	}
	return nil
}

func (s *Service) applyLocalizedDefaults(config *models.DailyReportConfig) {
	if config == nil {
		return
	}
	language := strings.TrimSpace(config.Language)
	if language == "" || language == "auto" {
		language, _ = s.store.GetSetting("language")
	}
	english := strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "en")
	if strings.TrimSpace(config.TitleTemplate) == "" {
		if english {
			config.TitleTemplate = "24-Hour AI Digest · {{date}}"
		} else {
			config.TitleTemplate = "24 小时 AI 日报 · {{date}}"
		}
	}
	if raw := strings.TrimSpace(config.OutlineJSON); raw == "" || raw == "[]" || raw == "null" {
		outputLanguage := "zh-CN"
		if english {
			outputLanguage = "en"
		}
		outline, _ := json.Marshal(localizedDefaultOutline(outputLanguage))
		config.OutlineJSON = string(outline)
	}
}
