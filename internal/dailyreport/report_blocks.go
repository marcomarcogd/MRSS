package dailyreport

import (
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	generatedScriptStylePattern = regexp.MustCompile(`(?is)<(?:script|style)[^>]*>.*?</(?:script|style)>`)
	generatedHeadingPattern     = regexp.MustCompile(`(?is)<h[1-6][^>]*>(.*?)</h[1-6]>`)
	generatedOrderedListPattern = regexp.MustCompile(`(?is)<ol\b[^>]*>.*?</ol>`)
	generatedUnorderedPattern   = regexp.MustCompile(`(?is)<ul\b[^>]*>.*?</ul>`)
	generatedListItemPattern    = regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`)
	generatedBlockEndPattern    = regexp.MustCompile(`(?is)</(?:p|div|section|article|blockquote|ul|ol|table|tr)>`)
	generatedBreakPattern       = regexp.MustCompile(`(?is)<br\s*/?>`)
	generatedTagPattern         = regexp.MustCompile(`(?is)<[^>]+>`)
	markdownImagePattern        = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	markdownLinkPattern         = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	markdownHeadingPattern      = regexp.MustCompile(`^\s*#{1,6}\s+(.+?)\s*#*\s*$`)
	markdownBulletPattern       = regexp.MustCompile(`^\s*[-+*]\s+(.+)$`)
	markdownOrderedPattern      = regexp.MustCompile(`^\s*\d+[.)]\s+(.+)$`)
	markdownRulePattern         = regexp.MustCompile(`^\s*(?:-{3,}|\*{3,}|_{3,})\s*$`)
	inlineCodePattern           = regexp.MustCompile("`+([^`]*)`+")
	markdownEmphasisPattern     = regexp.MustCompile(`(?:\*\*|__|~~)`)
	generatedPunctuationSpace   = regexp.MustCompile(`\s+([,.;:!?，。；：！？])`)
)

func parseGeneratedSection(raw string, definition OutlineSection, outline []OutlineSection, allowed map[int]struct{}) []ReportBlock {
	text := extractCurrentSection(normalizeGeneratedMarkup(raw), definition, outline)
	lines := strings.Split(text, "\n")
	blocks := make([]ReportBlock, 0)
	paragraphLines := make([]string, 0)
	paragraphSources := make(map[int]struct{})

	flushParagraph := func() {
		if len(paragraphLines) == 0 {
			return
		}
		value := cleanGeneratedInline(strings.Join(paragraphLines, " "))
		if value != "" {
			blocks = append(blocks, ReportBlock{Type: ReportBlockParagraph, Text: value, SourceIDs: sortedSourceIDs(paragraphSources)})
		}
		paragraphLines = paragraphLines[:0]
		clear(paragraphSources)
	}
	appendListItem := func(kind, rawItem string) {
		value := cleanGeneratedInline(rawItem)
		if value == "" {
			return
		}
		item := ReportBlockItem{Text: value, SourceIDs: validatedReferences(rawItem, allowed)}
		if len(blocks) == 0 || blocks[len(blocks)-1].Type != kind {
			blocks = append(blocks, ReportBlock{Type: kind})
		}
		blocks[len(blocks)-1].Items = append(blocks[len(blocks)-1].Items, item)
	}

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || markdownRulePattern.MatchString(line) {
			flushParagraph()
			continue
		}
		if match := markdownHeadingPattern.FindStringSubmatch(line); len(match) > 0 {
			flushParagraph()
			title := cleanGeneratedInline(match[1])
			if title != "" && !sameGeneratedTitle(title, definition.Title) {
				blocks = append(blocks, ReportBlock{Type: ReportBlockHeading, Text: title, SourceIDs: validatedReferences(line, allowed)})
			}
			continue
		}
		if match := markdownBulletPattern.FindStringSubmatch(line); len(match) > 0 {
			flushParagraph()
			appendListItem(ReportBlockUnorderedList, match[1])
			continue
		}
		if match := markdownOrderedPattern.FindStringSubmatch(line); len(match) > 0 {
			flushParagraph()
			appendListItem(ReportBlockOrderedList, match[1])
			continue
		}
		paragraphLines = append(paragraphLines, line)
		for _, sourceID := range validatedReferences(line, allowed) {
			paragraphSources[sourceID] = struct{}{}
		}
	}
	flushParagraph()
	return deduplicateReportBlocks(blocks)
}

func normalizeGeneratedMarkup(value string) string {
	value = markdownFencePattern.ReplaceAllString(value, "")
	value = generatedScriptStylePattern.ReplaceAllString(value, "")
	value = generatedHeadingPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := generatedHeadingPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return "\n"
		}
		return "\n### " + generatedTagPattern.ReplaceAllString(parts[1], "") + "\n"
	})
	value = generatedOrderedListPattern.ReplaceAllStringFunc(value, func(match string) string {
		return normalizeGeneratedHTMLList(match, true)
	})
	value = generatedUnorderedPattern.ReplaceAllStringFunc(value, func(match string) string {
		return normalizeGeneratedHTMLList(match, false)
	})
	value = generatedListItemPattern.ReplaceAllString(value, "\n- $1\n")
	value = generatedBreakPattern.ReplaceAllString(value, "\n")
	value = generatedBlockEndPattern.ReplaceAllString(value, "\n\n")
	value = generatedTagPattern.ReplaceAllString(value, "")
	return strings.TrimSpace(html.UnescapeString(value))
}

func normalizeGeneratedHTMLList(value string, ordered bool) string {
	items := generatedListItemPattern.FindAllStringSubmatch(value, -1)
	var builder strings.Builder
	for index, item := range items {
		if len(item) < 2 {
			continue
		}
		builder.WriteString("\n")
		if ordered {
			builder.WriteString(strconv.Itoa(index + 1))
			builder.WriteString(". ")
		} else {
			builder.WriteString("- ")
		}
		builder.WriteString(item[1])
		builder.WriteString("\n")
	}
	return builder.String()
}

func extractCurrentSection(value string, current OutlineSection, outline []OutlineSection) string {
	lines := strings.Split(value, "\n")
	known := make(map[string]string, len(outline))
	for _, item := range outline {
		known[normalizeGeneratedTitle(item.Title)] = item.ID
	}
	currentTitle := normalizeGeneratedTitle(current.Title)
	start := -1
	end := len(lines)
	firstKnownOther := -1
	for index, line := range lines {
		match := markdownHeadingPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) == 0 {
			continue
		}
		title := normalizeGeneratedTitle(match[1])
		if title == currentTitle {
			if start < 0 {
				start = index + 1
			}
			continue
		}
		if _, exists := known[title]; exists && firstKnownOther < 0 {
			firstKnownOther = index
		}
		if start >= 0 {
			if _, exists := known[title]; exists {
				end = index
				break
			}
		}
	}
	if start >= 0 {
		lines = lines[start:end]
	} else if firstKnownOther >= 0 {
		// The model returned a different known section but omitted the one we
		// requested. Do not attach the preamble (or that other section) to the
		// current section; let the generation path retry instead.
		return ""
	}
	for len(lines) > 0 && sameGeneratedTitle(strings.TrimSpace(strings.TrimLeft(lines[0], "# ")), current.Title) {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func cleanGeneratedInline(value string) string {
	value = html.UnescapeString(value)
	value = markdownImagePattern.ReplaceAllString(value, "$1")
	value = markdownLinkPattern.ReplaceAllString(value, "$1")
	value = inlineCodePattern.ReplaceAllString(value, "$1")
	value = markdownEmphasisPattern.ReplaceAllString(value, "$1")
	value = stripSingleMarkdownEmphasis(value)
	value = generatedTagPattern.ReplaceAllString(value, "")
	value = sourceReferencePattern.ReplaceAllString(value, "")
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	return generatedPunctuationSpace.ReplaceAllString(value, "$1")
}

func stripSingleMarkdownEmphasis(value string) string {
	runes := []rune(value)
	var builder strings.Builder
	for index, r := range runes {
		if r == '*' || r == '~' {
			continue
		}
		if r == '_' {
			leftWord := index > 0 && (unicode.IsLetter(runes[index-1]) || unicode.IsNumber(runes[index-1]))
			rightWord := index+1 < len(runes) && (unicode.IsLetter(runes[index+1]) || unicode.IsNumber(runes[index+1]))
			if !leftWord || !rightWord {
				continue
			}
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func sameGeneratedTitle(left, right string) bool {
	return normalizeGeneratedTitle(left) == normalizeGeneratedTitle(right)
}

func normalizeGeneratedTitle(value string) string {
	value = strings.ToLower(cleanGeneratedInline(strings.TrimSpace(strings.TrimLeft(value, "# "))))
	return strings.TrimFunc(value, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
}

func sortedSourceIDs(values map[int]struct{}) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func reportBlockSourceIDs(blocks []ReportBlock) []int {
	seen := make(map[int]struct{})
	for _, block := range blocks {
		for _, sourceID := range block.SourceIDs {
			seen[sourceID] = struct{}{}
		}
		for _, item := range block.Items {
			for _, sourceID := range item.SourceIDs {
				seen[sourceID] = struct{}{}
			}
		}
	}
	return sortedSourceIDs(seen)
}

func reportBlocksPlainText(blocks []ReportBlock) string {
	lines := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text != "" {
			lines = append(lines, block.Text)
		}
		for _, item := range block.Items {
			if item.Text != "" {
				lines = append(lines, item.Text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func deduplicateReportBlocks(blocks []ReportBlock) []ReportBlock {
	type seenEntry struct {
		normalized string
		text       string
		sourceIDs  []int
	}
	seen := make([]seenEntry, 0)
	isDuplicate := func(text string, sourceIDs []int) bool {
		normalized := normalizeComparableText(text)
		if normalized == "" {
			return true
		}
		for _, entry := range seen {
			if normalized == entry.normalized {
				return true
			}
			if sourcesOverlap(sourceIDs, entry.sourceIDs) && termSimilarity(text, entry.text) >= 0.85 {
				return true
			}
		}
		seen = append(seen, seenEntry{normalized: normalized, text: text, sourceIDs: append([]int(nil), sourceIDs...)})
		return false
	}

	result := make([]ReportBlock, 0, len(blocks))
	for _, block := range blocks {
		if len(block.Items) > 0 {
			items := make([]ReportBlockItem, 0, len(block.Items))
			for _, item := range block.Items {
				if !isDuplicate(item.Text, item.SourceIDs) {
					items = append(items, item)
				}
			}
			if len(items) > 0 {
				block.Items = items
				result = append(result, block)
			}
			continue
		}
		if !isDuplicate(block.Text, block.SourceIDs) {
			result = append(result, block)
		}
	}
	return result
}

func normalizeComparableText(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(cleanGeneratedInline(value)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Han, r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func sourcesOverlap(left, right []int) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[int]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, exists := seen[value]; exists {
			return true
		}
	}
	return false
}

func termSimilarity(left, right string) float64 {
	leftTerms := comparableTerms(left)
	rightTerms := comparableTerms(right)
	if len(leftTerms) == 0 || len(rightTerms) == 0 {
		return 0
	}
	intersection := 0
	for term := range leftTerms {
		if _, exists := rightTerms[term]; exists {
			intersection++
		}
	}
	union := len(leftTerms) + len(rightTerms) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func comparableTerms(value string) map[string]struct{} {
	result := make(map[string]struct{})
	runes := []rune(value)
	for _, field := range strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if utf8.RuneCountInString(field) >= 2 {
			result[field] = struct{}{}
		}
	}
	for index := 0; index+2 <= len(runes); index++ {
		if unicode.Is(unicode.Han, runes[index]) && unicode.Is(unicode.Han, runes[index+1]) {
			result[string(runes[index:index+2])] = struct{}{}
		}
	}
	return result
}

func splitCompleteGeneratedText(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	runes := []rune(value)
	last := -1
	for index, r := range runes {
		if strings.ContainsRune("。！？.!?；;", r) {
			last = index + 1
			for last < len(runes) && strings.ContainsRune("\"'”’）)]】", runes[last]) {
				last++
			}
		}
	}
	if last <= 0 {
		return "", value
	}
	return strings.TrimSpace(string(runes[:last])), strings.TrimSpace(string(runes[last:]))
}

func mergeGeneratedContinuation(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	leftLower := strings.ToLower(left)
	rightLower := strings.ToLower(right)
	if strings.Contains(rightLower, leftLower) {
		return right
	}
	if strings.Contains(leftLower, rightLower) {
		return left
	}
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	limit := min(len(leftRunes), len(rightRunes), 320)
	for size := limit; size >= 6; size-- {
		if strings.EqualFold(string(leftRunes[len(leftRunes)-size:]), string(rightRunes[:size])) {
			return strings.TrimSpace(left + string(rightRunes[size:]))
		}
	}
	separator := " "
	if strings.HasSuffix(left, "\n") || strings.HasPrefix(right, "\n") {
		separator = ""
	}
	return strings.TrimSpace(left + separator + right)
}

func looksLikeTruncatedOutput(content, finishReason string, explicit bool, outputTokens int64, maxTokens int) bool {
	if explicit {
		return true
	}
	reason := strings.ToLower(strings.TrimSpace(finishReason))
	if reason == "length" || reason == "max_tokens" || reason == "max_output_tokens" {
		return true
	}
	if reason != "" || maxTokens <= 0 || outputTokens < int64(float64(maxTokens)*0.9) {
		return false
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return true
	}
	last, _ := utf8.DecodeLastRuneInString(trimmed)
	return !strings.ContainsRune("。！？.!?；;\"'”’）)]】", last)
}

func continuationPrompt(section OutlineSection, completed, unfinished string) string {
	contextTail := []rune(strings.TrimSpace(completed))
	if len(contextTail) > 500 {
		contextTail = contextTail[len(contextTail)-500:]
	}
	return "Continue only the unfinished RSS digest section named " + strconv.Quote(section.Title) + ". " +
		"Do not repeat the section title or any completed text. Finish every sentence and keep source markers.\n\n" +
		"Completed ending:\n" + string(contextTail) + "\n\nUnfinished fragment:\n" + strings.TrimSpace(unfinished)
}

func sectionOutputBudget(sourceCount int) int {
	budget := 4096 + sourceCount*512
	if budget > 6144 {
		return 6144
	}
	return budget
}
