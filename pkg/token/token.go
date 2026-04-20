// Package token provides a lightweight token counting heuristic for LLM context budgeting.
package token

// Count estimates the number of tokens in s using a chars/4 heuristic.
// Returns 0 for empty strings, rounds up for non-empty strings.
func Count(s string) int {
	n := len(s)
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}
