package onboarding

// feedbackMessages maps join actions to user-facing feedback strings.
var feedbackMessages = map[string]string{
	"tenant_created": "Welcome! Your workspace has been created.",
	"user_joined":    "Welcome! You have joined the workspace.",
	"auto_joined":    "Welcome! You have been added to the workspace.",
	"user_linked":    "Your account has been linked to this channel.",
	"tenant_linked":  "This workspace has been linked to the tenant.",
}

// FeedbackMessage returns the feedback text for a given join action.
// Returns empty string for unknown actions.
func FeedbackMessage(action string) string {
	if msg, ok := feedbackMessages[action]; ok {
		return msg
	}
	return ""
}
