package dedup

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/steveyegge/beads/internal/types"
)

// DEDUPLICATION THRESHOLDS AND GATING
//
// These constants define the single shared matching criteria across beads:
// pre-creation similarity checks in `bd create`, recurrence duplicate prevention
// on `--repeat` closes, and mechanical similarity in `bd find-duplicates`.
const (
	// MinDomainTokens is the minimum number of shared domain keyword tokens
	// required to consider two issues as potential duplicates.
	//
	// REASONING:
	// A single shared domain token (e.g. "migration", "test", "auth", "bug")
	// produces massive false-positive rates across distinct issues in the same
	// subsystem (e.g. "Database migration fails on table users" vs "Database
	// migration performance degradation during bulk index creation").
	// Requiring at least 2 shared domain keyword tokens ensures that issues must
	// share a specific topic/scope, not merely a common subsystem or action keyword.
	MinDomainTokens = 2

	// SimilarityThreshold is the combined similarity threshold (0.0 to 1.0)
	// above which two issues with >= MinDomainTokens are considered duplicates.
	//
	// REASONING:
	// 0.35 (35%) catches paraphrased titles, reordered words, and minor additions
	// (e.g. "Fix auth token refresh" vs "Auth token refresh bug in client")
	// while the >= 2 domain token gate prevents keyword-partial matches between
	// deliberately distinct tasks that share subsystem vocabulary.
	SimilarityThreshold = 0.35
)

// StopWords contains common English words, articles, prepositions, conjunctions,
// pronouns, and auxiliary verbs that do not convey domain-specific meaning.
// Tokens in this map are ignored when counting shared domain keyword tokens.
var StopWords = map[string]bool{
	"a": true, "about": true, "above": true, "after": true, "again": true,
	"against": true, "all": true, "am": true, "an": true, "and": true,
	"any": true, "are": true, "as": true, "at": true, "be": true,
	"because": true, "been": true, "before": true, "being": true, "below": true,
	"between": true, "both": true, "but": true, "by": true, "can": true,
	"could": true, "did": true, "do": true, "does": true, "doing": true,
	"down": true, "during": true, "each": true, "few": true, "for": true,
	"from": true, "further": true, "had": true, "has": true, "have": true,
	"having": true, "he": true, "her": true, "here": true, "hers": true,
	"herself": true, "him": true, "himself": true, "his": true, "how": true,
	"i": true, "if": true, "in": true, "into": true, "is": true,
	"it": true, "its": true, "itself": true, "just": true, "me": true,
	"more": true, "most": true, "my": true, "myself": true, "no": true,
	"nor": true, "not": true, "now": true, "of": true, "off": true,
	"on": true, "once": true, "only": true, "or": true, "other": true,
	"our": true, "ours": true, "ourselves": true, "out": true, "over": true,
	"own": true, "same": true, "she": true, "should": true, "so": true,
	"some": true, "such": true, "than": true, "that": true, "the": true,
	"their": true, "theirs": true, "them": true, "themselves": true, "then": true,
	"there": true, "these": true, "they": true, "this": true, "those": true,
	"through": true, "to": true, "too": true, "under": true, "until": true,
	"up": true, "very": true, "was": true, "we": true, "were": true,
	"what": true, "when": true, "where": true, "which": true, "while": true,
	"who": true, "whom": true, "why": true, "will": true, "with": true,
	"would": true, "you": true, "your": true, "yours": true, "yourself": true,
	"yourselves": true,
	// Common contractions without apostrophe (since Tokenize strips punctuation)
	"dont": true, "cant": true, "wont": true, "isnt": true, "arent": true,
	"wasnt": true, "werent": true, "hasnt": true, "havent": true, "hadnt": true,
	"doesnt": true, "didnt": true, "shouldnt": true, "wouldnt": true, "couldnt": true,
}

// Tokenize splits text into lowercase word tokens, removing punctuation.
// Reusable across beads duplicate detection and similarity analysis.
func Tokenize(text string) map[string]int {
	tokens := make(map[string]int)
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-'
	})
	for _, w := range words {
		if len(w) > 1 { // Skip single chars
			tokens[w]++
		}
	}
	return tokens
}

// ExtractDomainTokens filters out common stopwords, returning only
// domain-specific keyword tokens.
func ExtractDomainTokens(tokens map[string]int) map[string]int {
	domain := make(map[string]int)
	for w, count := range tokens {
		if !StopWords[w] {
			domain[w] = count
		}
	}
	return domain
}

// CountSharedTokens counts how many distinct keys exist in both token maps.
func CountSharedTokens(a, b map[string]int) int {
	shared := 0
	for token := range a {
		if _, ok := b[token]; ok {
			shared++
		}
	}
	return shared
}

// JaccardSimilarity computes the Jaccard similarity between two token frequency maps.
func JaccardSimilarity(a, b map[string]int) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}

	intersection := 0
	union := 0

	for token, countA := range a {
		if countB, ok := b[token]; ok {
			if countA < countB {
				intersection += countA
			} else {
				intersection += countB
			}
			if countA > countB {
				union += countA
			} else {
				union += countB
			}
		} else {
			union += countA
		}
	}
	for token, countB := range b {
		if _, ok := a[token]; !ok {
			union += countB
		}
	}

	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// CosineSimilarity computes the cosine similarity between two token frequency vectors.
func CosineSimilarity(a, b map[string]int) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	dotProduct := 0.0
	magA := 0.0
	magB := 0.0

	for token, countA := range a {
		fa := float64(countA)
		magA += fa * fa
		if countB, ok := b[token]; ok {
			dotProduct += fa * float64(countB)
		}
	}
	for _, countB := range b {
		fb := float64(countB)
		magB += fb * fb
	}

	if magA == 0 || magB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(magA) * math.Sqrt(magB))
}

// TextSimilarity computes the average of Jaccard and cosine similarity
// between two token frequency maps, as used by beads duplicate detection.
func TextSimilarity(a, b map[string]int) float64 {
	jaccard := JaccardSimilarity(a, b)
	cosine := CosineSimilarity(a, b)
	return (jaccard + cosine) / 2
}

// IsContainment reports whether one token set is entirely contained within the other.
func IsContainment(a, b map[string]int) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	// Check if a is subset of b
	aInB := true
	for token := range a {
		if _, ok := b[token]; !ok {
			aInB = false
			break
		}
	}
	if aInB {
		return true
	}
	// Check if b is subset of a
	bInA := true
	for token := range b {
		if _, ok := a[token]; !ok {
			bInA = false
			break
		}
	}
	return bInA
}

// NormalizeTitle lowercases and trims a title for exact-equality comparison.
func NormalizeTitle(title string) string {
	return strings.TrimSpace(strings.ToLower(title))
}

// IsDuplicate compares two issue titles and reports whether they are duplicates,
// returning (isDuplicate, similarityScore, reason).
func IsDuplicate(titleA, titleB string) (bool, float64, string) {
	normA := NormalizeTitle(titleA)
	normB := NormalizeTitle(titleB)
	if normA == "" || normB == "" {
		return false, 0, ""
	}
	if normA == normB {
		return true, 1.0, "exact title match"
	}

	tokensA := Tokenize(titleA)
	tokensB := Tokenize(titleB)
	domainA := ExtractDomainTokens(tokensA)
	domainB := ExtractDomainTokens(tokensB)

	return CheckDuplicateTokens(normA, normB, tokensA, tokensB, domainA, domainB)
}

// CheckDuplicateTokens checks duplicate conditions given precomputed tokens.
func CheckDuplicateTokens(
	normA, normB string,
	tokensA, tokensB map[string]int,
	domainA, domainB map[string]int,
) (bool, float64, string) {
	if normA != "" && normA == normB {
		return true, 1.0, "exact title match"
	}

	sharedDomain := CountSharedTokens(domainA, domainB)
	if sharedDomain < MinDomainTokens {
		// Does not satisfy >= 2 domain keyword tokens gate
		return false, 0, ""
	}

	sim := TextSimilarity(tokensA, tokensB)

	// Containment: either domain tokens of one are a subset of the other,
	// or normalized string containment.
	containment := IsContainment(domainA, domainB) ||
		(normA != "" && normB != "" && (strings.Contains(normA, normB) || strings.Contains(normB, normA)))

	if containment {
		effectiveSim := sim
		if effectiveSim < SimilarityThreshold {
			effectiveSim = SimilarityThreshold
		}
		return true, effectiveSim, "containment"
	}

	if sim >= SimilarityThreshold {
		return true, sim, fmt.Sprintf("%.0f%% similarity", sim*100)
	}

	return false, sim, ""
}

// ActiveIssueEntry holds precomputed token sets for an active issue.
type ActiveIssueEntry struct {
	Issue        *types.Issue
	NormTitle    string
	Tokens       map[string]int
	DomainTokens map[string]int
}

// Matcher caches token sets of active issues to keep create latency fast.
type Matcher struct {
	entries []ActiveIssueEntry
}

// ConflictResult contains information about a conflicting active issue.
type ConflictResult struct {
	ConflictIssue *types.Issue `json:"conflict_issue"`
	Similarity    float64      `json:"similarity"`
	Reason        string       `json:"reason"`
}

// NewMatcher builds a matcher from active issues, precomputing token sets once per invocation.
func NewMatcher(activeIssues []*types.Issue) *Matcher {
	entries := make([]ActiveIssueEntry, 0, len(activeIssues))
	for _, issue := range activeIssues {
		if issue == nil || issue.Title == "" {
			continue
		}
		// Active-only scope: never index closed/pinned
		if issue.Status == types.StatusClosed || issue.Status == types.StatusPinned {
			continue
		}
		norm := NormalizeTitle(issue.Title)
		tokens := Tokenize(issue.Title)
		domain := ExtractDomainTokens(tokens)
		entries = append(entries, ActiveIssueEntry{
			Issue:        issue,
			NormTitle:    norm,
			Tokens:       tokens,
			DomainTokens: domain,
		})
	}
	return &Matcher{entries: entries}
}

// FindConflict checks if title conflicts with any active issue.
// Returns the conflict with highest similarity, or nil if none found.
func (m *Matcher) FindConflict(title string) *ConflictResult {
	if m == nil || len(m.entries) == 0 {
		return nil
	}
	normTitle := NormalizeTitle(title)
	if normTitle == "" {
		return nil
	}
	titleTokens := Tokenize(title)
	titleDomain := ExtractDomainTokens(titleTokens)

	var bestConflict *ConflictResult
	for _, entry := range m.entries {
		isDup, sim, reason := CheckDuplicateTokens(
			normTitle, entry.NormTitle,
			titleTokens, entry.Tokens,
			titleDomain, entry.DomainTokens,
		)
		if isDup {
			if bestConflict == nil || sim > bestConflict.Similarity {
				bestConflict = &ConflictResult{
					ConflictIssue: entry.Issue,
					Similarity:    sim,
					Reason:        reason,
				}
			}
		}
	}
	return bestConflict
}
