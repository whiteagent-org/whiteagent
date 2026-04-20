package telegram

import (
	"testing"
	"time"
)

func TestChatMetadata(t *testing.T) {
	t.Run("supergroup with title and username", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "supergroup", Title: "Dev Team", Username: "devteam"},
			Date: 1709827200,
		}
		meta := buildMetadata(msg, false, true)

		assertMeta(t, meta, "chat_type", "supergroup")
		assertMeta(t, meta, "chat_title", "Dev Team")
		assertMeta(t, meta, "chat_username", "devteam")
	})

	t.Run("private chat omits title and username", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 2, Type: "private"},
			Date: 1709827200,
		}
		meta := buildMetadata(msg, false, true)

		assertMeta(t, meta, "chat_type", "private")
		assertAbsent(t, meta, "chat_title")
		assertAbsent(t, meta, "chat_username")
	})
}

func TestSenderMetadata(t *testing.T) {
	t.Run("full sender info", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			From: &tgUser{
				ID:           123,
				FirstName:    "Dmitry",
				LastName:     "K",
				Username:     "dmitry",
				IsBot:        false,
				LanguageCode: "en",
			},
		}
		meta := buildMetadata(msg, false, true)

		assertMeta(t, meta, "sender_username", "dmitry")
		assertMeta(t, meta, "sender_is_bot", "false")
		assertMeta(t, meta, "sender_language", "en")
	})

	t.Run("missing username and language omits keys", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			From: &tgUser{
				ID:        456,
				FirstName: "Alice",
				IsBot:     false,
			},
		}
		meta := buildMetadata(msg, false, true)

		assertAbsent(t, meta, "sender_username")
		assertAbsent(t, meta, "sender_language")
		assertMeta(t, meta, "sender_is_bot", "false")
	})

	t.Run("bot sender", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			From: &tgUser{ID: 789, FirstName: "BotName", IsBot: true},
		}
		meta := buildMetadata(msg, false, true)
		assertMeta(t, meta, "sender_is_bot", "true")
	})
}

func TestMessageContextMetadata(t *testing.T) {
	t.Run("message date as RFC3339", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
		}
		meta := buildMetadata(msg, false, true)

		expected := time.Unix(1709827200, 0).UTC().Format(time.RFC3339)
		assertMeta(t, meta, "message_date", expected)
	})

	t.Run("forwarded message from user", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			ForwardOrigin: &tgForwardOrigin{
				Type: "user",
				Date: 1709820000,
				SenderUser: &tgUser{
					ID:        999,
					FirstName: "Original",
					LastName:  "Sender",
				},
			},
		}
		meta := buildMetadata(msg, false, true)

		assertMeta(t, meta, "is_forwarded", "true")
		assertMeta(t, meta, "forward_from_name", "Original Sender")

		expectedDate := time.Unix(1709820000, 0).UTC().Format(time.RFC3339)
		assertMeta(t, meta, "forward_date", expectedDate)
	})

	t.Run("forwarded message from hidden user", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			ForwardOrigin: &tgForwardOrigin{
				Type:           "hidden_user",
				Date:           1709820000,
				SenderUserName: "Hidden Name",
			},
		}
		meta := buildMetadata(msg, false, true)

		assertMeta(t, meta, "is_forwarded", "true")
		assertMeta(t, meta, "forward_from_name", "Hidden Name")
	})

	t.Run("forwarded message from chat", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			ForwardOrigin: &tgForwardOrigin{
				Type:       "chat",
				Date:       1709820000,
				SenderChat: &tgChat{ID: 555, Type: "supergroup", Title: "Source Chat"},
			},
		}
		meta := buildMetadata(msg, false, true)

		assertMeta(t, meta, "is_forwarded", "true")
		assertMeta(t, meta, "forward_from_name", "Source Chat")
	})

	t.Run("forwarded message from channel with signature", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			ForwardOrigin: &tgForwardOrigin{
				Type:            "channel",
				Date:            1709820000,
				SenderChat:      &tgChat{ID: 666, Type: "channel", Title: "News Channel"},
				AuthorSignature: "Editor",
			},
		}
		meta := buildMetadata(msg, false, true)

		assertMeta(t, meta, "is_forwarded", "true")
		assertMeta(t, meta, "forward_from_name", "News Channel (Editor)")
	})
}

func TestEmptyMetadataOmission(t *testing.T) {
	msg := &tgMessage{
		Chat: tgChat{ID: 1, Type: "private"},
		Date: 1709827200,
	}
	meta := buildMetadata(msg, false, true)

	// No From set -- sender keys should be absent.
	assertAbsent(t, meta, "sender_name")
	assertAbsent(t, meta, "sender_username")
	assertAbsent(t, meta, "sender_is_bot")
	assertAbsent(t, meta, "sender_language")

	// No forward -- forward keys should be absent.
	assertAbsent(t, meta, "is_forwarded")
	assertAbsent(t, meta, "forward_from_name")
	assertAbsent(t, meta, "forward_date")

	// chat_type always present, title/username absent for private.
	assertMeta(t, meta, "chat_type", "private")
	assertAbsent(t, meta, "chat_title")
	assertAbsent(t, meta, "chat_username")
}

func TestSenderNameBackwardCompat(t *testing.T) {
	t.Run("first and last name", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			From: &tgUser{ID: 1, FirstName: "John", LastName: "Doe"},
		}
		meta := buildMetadata(msg, false, true)
		assertMeta(t, meta, "sender_name", "John Doe")
	})

	t.Run("first name only", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			From: &tgUser{ID: 1, FirstName: "John"},
		}
		meta := buildMetadata(msg, false, true)
		assertMeta(t, meta, "sender_name", "John")
	})

	t.Run("no name omits key", func(t *testing.T) {
		msg := &tgMessage{
			Chat: tgChat{ID: 1, Type: "private"},
			Date: 1709827200,
			From: &tgUser{ID: 1},
		}
		meta := buildMetadata(msg, false, true)
		assertAbsent(t, meta, "sender_name")
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func assertMeta(t *testing.T, meta map[string]string, key, expected string) {
	t.Helper()
	v, ok := meta[key]
	if !ok {
		t.Errorf("expected metadata key %q to be present", key)
		return
	}
	if v != expected {
		t.Errorf("metadata key %q: got %q, want %q", key, v, expected)
	}
}

func assertAbsent(t *testing.T, meta map[string]string, key string) {
	t.Helper()
	if v, ok := meta[key]; ok {
		t.Errorf("expected metadata key %q to be absent, got %v", key, v)
	}
}
