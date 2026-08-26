package dailyreport

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"MRSS/internal/models"
	localsummary "MRSS/internal/summary"
)

const maxLocalItemsPerSection = 12

var localTermPattern = regexp.MustCompile(`[\p{Han}]+|[\p{L}\p{N}][\p{L}\p{N}_+-]*`)

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
	return localizedDefaultOutline(language), nil
}

type rankedLocalCandidate struct {
	index int
	score float64
}

func localFallback(config *models.DailyReportConfig, candidates []models.DailyReportCandidate) AIResult {
	outline, err := parseOutline(config.OutlineJSON)
	if err != nil || len(outline) == 0 {
		outline = localizedDefaultOutline(config.Language)
	}
	sectionTerms := make([][]string, len(outline))
	for index, definition := range outline {
		sectionTerms[index] = localOutlineTerms(definition)
	}
	focusTerms := localTerms(config.Focus)
	assignments := make([][]rankedLocalCandidate, len(outline))
	for candidateIndex, candidate := range candidates {
		bestSection, bestScore := -1, float64(0)
		for sectionIndex := range outline {
			score := localRelevance(candidate, sectionTerms[sectionIndex], focusTerms)
			if score > bestScore {
				bestSection, bestScore = sectionIndex, score
			}
		}
		if bestSection >= 0 && bestScore > 0 {
			assignments[bestSection] = append(assignments[bestSection], rankedLocalCandidate{index: candidateIndex, score: bestScore})
		}
	}

	summarizer := localsummary.NewSummarizer()
	sections := make([]ReportSection, 0, len(outline))
	for index, definition := range outline {
		ranked := assignments[index]
		sort.SliceStable(ranked, func(i, j int) bool {
			if ranked[i].score == ranked[j].score {
				return ranked[i].index < ranked[j].index
			}
			return ranked[i].score > ranked[j].score
		})
		if len(ranked) > maxLocalItemsPerSection {
			ranked = ranked[:maxLocalItemsPerSection]
		}
		lines := make([]string, 0, len(ranked))
		sourceIDs := make([]int, 0, len(ranked))
		for _, item := range ranked {
			candidate := candidates[item.index]
			title := cleanReportText(candidate.Title)
			text, _ := candidateContent(candidate)
			text = cleanReportText(text)
			if title == "" {
				title = localUntitled(config.Language)
			}
			line := title
			if text != "" {
				extracted := strings.TrimSpace(summarizer.Summarize(text, localsummary.Short).Summary)
				if extracted != "" && !strings.EqualFold(extracted, title) {
					line += " — " + truncateText(extracted, 240)
				}
			}
			lines = append(lines, "- "+line)
			sourceIDs = append(sourceIDs, item.index+1)
		}
		summaryText := strings.Join(lines, "\n")
		if summaryText == "" {
			summaryText = localEmptySection(config.Language)
		}
		sections = append(sections, ReportSection{
			ID: definition.ID, Title: definition.Title, Summary: summaryText, SourceIDs: sourceIDs,
		})
	}
	content := ReportContent{Sections: sections}
	markdown := renderMarkdown(content, candidates)
	if config.Language == "en" {
		markdown = "> Local summary: no article content was sent to a cloud AI service.\n\n" + markdown
	} else {
		markdown = "> 本地摘要：文章内容未发送到云端 AI 服务。\n\n" + markdown
	}
	return AIResult{Content: content, Markdown: strings.TrimSpace(markdown)}
}

func localOutlineTerms(definition OutlineSection) []string {
	for _, language := range []string{"zh-CN", "en"} {
		for _, item := range localizedDefaultOutline(language) {
			if definition.ID == item.ID && strings.EqualFold(strings.TrimSpace(definition.Title), strings.TrimSpace(item.Title)) &&
				strings.EqualFold(strings.TrimSpace(definition.Instruction), strings.TrimSpace(item.Instruction)) {
				return nil
			}
		}
	}
	return localTerms(definition.Title + " " + definition.Instruction)
}

func localRelevance(candidate models.DailyReportCandidate, sectionTerms, focusTerms []string) float64 {
	fields := []struct {
		value  string
		weight float64
	}{
		{candidate.Title, 8}, {candidate.OriginalSummary, 4}, {candidate.GeneratedSummary, 4}, {candidate.Content, 2},
		{candidate.FeedTitle, 3}, {candidate.Author, 2},
	}
	score := weightedTermScore(fields, sectionTerms)
	focusScore := weightedTermScore(fields, focusTerms)
	if len(focusTerms) > 0 && focusScore == 0 {
		return 0
	}
	score += focusScore * 1.5
	if len(sectionTerms) == 0 && len(focusTerms) == 0 {
		return 1
	}
	return score
}

func weightedTermScore(fields []struct {
	value  string
	weight float64
}, terms []string) float64 {
	var score float64
	for _, field := range fields {
		value := strings.ToLower(cleanReportText(field.value))
		for _, term := range terms {
			if term != "" && strings.Contains(value, term) {
				score += field.weight * (1 + float64(len([]rune(term)))/10)
			}
		}
	}
	return score
}

func localTerms(value string) []string {
	stop := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "from": {}, "with": {}, "report": {}, "summary": {}, "news": {}, "updates": {},
		"重点": {}, "速览": {}, "主题": {}, "动态": {}, "关注": {}, "报告": {}, "日报": {}, "摘要": {}, "内容": {}, "文章": {},
		"提炼": {}, "主要": {}, "列出": {}, "归纳": {}, "值得": {}, "结论": {}, "线索": {}, "最新": {}, "进行": {},
	}
	seen := make(map[string]struct{})
	terms := make([]string, 0)
	for _, raw := range localTermPattern.FindAllString(strings.ToLower(cleanReportText(value)), -1) {
		term := strings.TrimFunc(raw, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
		if len([]rune(term)) < 2 {
			continue
		}
		if _, blocked := stop[term]; blocked {
			continue
		}
		if _, exists := seen[term]; !exists {
			seen[term] = struct{}{}
			terms = append(terms, term)
		}
		runes := []rune(term)
		if len(runes) > 3 && allHan(runes) {
			for i := 0; i+2 <= len(runes); i++ {
				part := string(runes[i : i+2])
				if _, blocked := stop[part]; blocked {
					continue
				}
				if _, exists := seen[part]; !exists {
					seen[part] = struct{}{}
					terms = append(terms, part)
				}
			}
		}
	}
	return terms
}

func allHan(runes []rune) bool {
	for _, value := range runes {
		if !unicode.Is(unicode.Han, value) {
			return false
		}
	}
	return true
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

func localEmptySection(language string) string {
	if language == "en" {
		return "No content matched this section during the reporting period."
	}
	return "本时段没有符合该栏目要求的内容。"
}

func localUntitled(language string) string {
	if language == "en" {
		return "Untitled article"
	}
	return "无标题文章"
}

func truncateText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
