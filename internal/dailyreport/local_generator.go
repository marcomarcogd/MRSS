package dailyreport

import (
	"context"
	"fmt"
	"strings"

	"MRSS/internal/models"
)

// LocalGenerator is the privacy-preserving fallback. It never performs a
// network request and creates a deterministic report from cached data.
type LocalGenerator struct{}

func (LocalGenerator) Generate(_ context.Context, config *models.DailyReportConfig, candidates []models.DailyReportCandidate) (AIResult, error) {
	return localFallback(config, candidates), nil
}

func (LocalGenerator) OptimizeOutline(_ context.Context, focus, language string, _ *int64) ([]OutlineSection, error) {
	if len([]rune(focus)) > MaxFocusLength {
		return nil, fmt.Errorf("focus must not exceed %d characters", MaxFocusLength)
	}
	if _, err := normalizeLanguage(language); err != nil {
		return nil, err
	}
	return defaultOutline(), nil
}

func localFallback(config *models.DailyReportConfig, candidates []models.DailyReportCandidate) AIResult {
	outline, err := parseOutline(config.OutlineJSON)
	if err != nil || len(outline) == 0 {
		outline = defaultOutline()
	}
	sections := make([]ReportSection, 0, len(outline))
	for i, definition := range outline {
		var lines []string
		var sourceIDs []int
		for index := i; index < len(candidates); index += len(outline) {
			if len(lines) >= 20 {
				break
			}
			candidate := candidates[index]
			line := candidate.Title
			if strings.TrimSpace(candidate.Summary) != "" {
				line += " — " + truncateText(strings.TrimSpace(candidate.Summary), 240)
			}
			lines = append(lines, "- "+line)
			sourceIDs = append(sourceIDs, index+1)
		}
		if len(lines) == 0 {
			continue
		}
		sections = append(sections, ReportSection{ID: definition.ID, Title: definition.Title, Summary: strings.Join(lines, "\n"), SourceIDs: sourceIDs})
	}
	content := ReportContent{Sections: sections}
	return AIResult{Content: content, Markdown: renderMarkdown(content, candidates)}
}

func renderMarkdown(content ReportContent, candidates []models.DailyReportCandidate) string {
	var builder strings.Builder
	for _, section := range content.Sections {
		builder.WriteString("## ")
		builder.WriteString(section.Title)
		builder.WriteString("\n\n")
		builder.WriteString(section.Summary)
		builder.WriteString("\n\n")
		if len(section.SourceIDs) == 0 {
			continue
		}
		builder.WriteString("来源：")
		for _, id := range section.SourceIDs {
			if id <= 0 || id > len(candidates) {
				continue
			}
			candidate := candidates[id-1]
			if candidate.URL == "" {
				builder.WriteString(fmt.Sprintf(" [%d]", id))
			} else {
				builder.WriteString(fmt.Sprintf(" [%d](%s)", id, candidate.URL))
			}
		}
		builder.WriteString("\n\n")
	}
	return strings.TrimSpace(builder.String())
}

func truncateText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
