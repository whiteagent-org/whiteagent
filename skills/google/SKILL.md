---
name: google
description: "Google: Calendar, Gmail, Drive, Docs, Sheets, Slides, Forms, and Meet."
---

# Google Workspace Skill

Unified skill for Google APIs using raw HTTPS calls. No external dependencies.

## Required Environment Variables

```
GOOGLE_CLIENT_ID
GOOGLE_CLIENT_SECRET
GOOGLE_REFRESH_TOKEN
```

## Setup Flow

Before using any Google command, follow this flow:

### Step 1: Check existing secrets

```bash
secret_list
```

Look for `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `GOOGLE_REFRESH_TOKEN`.

### Step 2: Try what you have

- If all 3 secrets exist → run the requested command directly.
- If `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` exist but `GOOGLE_REFRESH_TOKEN` is missing → go to Step 3.
- If none exist → use `secret_request` to ask the user for `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`, then go to Step 3.

### Step 3: Generate OAuth URL

```bash
cd <skill_path> && node index.js auth-url
```

This prints `{"url": "..."}`. Send the URL to the user and ask them to:
1. Open the URL in their browser
2. Authorize the application
3. Copy the authorization code shown on screen and paste it back in the chat

### Step 4: Exchange code for refresh token

```bash
cd <skill_path> && node index.js auth-exchange --code <CODE>
```

This prints `{"refresh_token": "..."}`. Store it:

```bash
secret_set GOOGLE_REFRESH_TOKEN <the_refresh_token_value>
```

### Step 5: Execute the original command

All 3 secrets are now set. Run the user's requested command.

## Error Handling

### Exit code 2 — REAUTH_REQUIRED

The refresh token has expired or been revoked. Restart from Step 3 (generate a new OAuth URL). Do NOT request new client credentials — only a new authorization code is needed.

### Exit code 3 — INVALID_CLIENT

The client ID or secret is invalid. Use `secret_request` to ask the user for new `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`, then restart from Step 3.

### Exit code 1 — Generic error

Display the error message to the user. Common causes: missing required flags, API errors, permission issues.

## Commands

`<skill_path>` refers to this skill's path as shown in the skill list. All commands must run from the skill directory:

```bash
cd <skill_path> && node index.js <service> <command> [--flags]
```

### Calendar

```bash
cd <skill_path> && node index.js calendar calendars
cd <skill_path> && node index.js calendar list --date today
cd <skill_path> && node index.js calendar list --date 2026-04-04
cd <skill_path> && node index.js calendar list --date tomorrow --all true
cd <skill_path> && node index.js calendar list --from 2026-02-01T00:00:00Z --to 2026-02-07T00:00:00Z
cd <skill_path> && node index.js calendar list --from 2026-02-01T00:00:00Z --to 2026-02-07T00:00:00Z --all true
cd <skill_path> && node index.js calendar add --title "Demo" --start 2026-02-02T10:00:00Z --end 2026-02-02T11:00:00Z
cd <skill_path> && node index.js calendar update --eventId EVENT_ID --title "Updated"
cd <skill_path> && node index.js calendar delete --eventId EVENT_ID
```

| Command | Description |
|---------|-------------|
| `calendars` | List all calendars |
| `list` | List events. Use `--date today\|tomorrow\|YYYY-MM-DD` as shorthand for `--from`/`--to`. Use `--all true` for all calendars. |
| `add` | Create an event |
| `update` | Update an event |
| `delete` | Delete an event |

### Gmail

```bash
cd <skill_path> && node index.js gmail list --after 2026-04-01 --before 2026-04-05 --detail
cd <skill_path> && node index.js gmail list --q "from:someone" --detail
cd <skill_path> && node index.js gmail list --q "from:someone" --max 10
cd <skill_path> && node index.js gmail get --messageId MESSAGE_ID
cd <skill_path> && node index.js gmail get-raw --messageId MESSAGE_ID
cd <skill_path> && node index.js gmail send --to "a@b.com" --subject "Hi" --text "Hello"
cd <skill_path> && node index.js gmail send --to "a@b.com" --subject "Hi" --text "Hello" --attachments "/path/a.pdf,/path/b.png"
cd <skill_path> && node index.js gmail reply --to "a@b.com" --subject "Re: Hi" --text "Reply" --threadId THREAD_ID --inReplyTo MSG_ID
cd <skill_path> && node index.js gmail forward --to "a@b.com" --subject "Fwd: Hi" --messageId MESSAGE_ID
```

| Command | Description |
|---------|-------------|
| `list` | Search/list messages. Use `--after`/`--before` (YYYY-MM-DD) for date range. Use `--detail` to include subject, from, date, snippet. |
| `get` | Read a message |
| `get-raw` | Read a message in raw format |
| `send` | Send an email (supports `--attachments`) |
| `reply` | Reply to a message |
| `forward` | Forward a message |

### Drive

```bash
cd <skill_path> && node index.js drive list --type pdf
cd <skill_path> && node index.js drive list --name "report" --type doc
cd <skill_path> && node index.js drive list --type sheet --name "budget"
cd <skill_path> && node index.js drive list --q "mimeType='application/pdf'" --pageSize 10
cd <skill_path> && node index.js drive upload --file ./doc.pdf --name "doc.pdf" --folderId FOLDER_ID
cd <skill_path> && node index.js drive create-folder --name "Projects" --folderId PARENT_ID
cd <skill_path> && node index.js drive delete --fileId FILE_ID
cd <skill_path> && node index.js drive share --id FILE_ID --email "a@b.com,b@c.com" --role writer
```

| Command | Description |
|---------|-------------|
| `list` | List/search files. Use `--type` (pdf, doc, sheet, slide, form, folder, image, video) and `--name` shortcuts instead of raw `--q` syntax. |
| `upload` | Upload a file |
| `create-folder` | Create a folder |
| `delete` | Delete a file |
| `share` | Share a file with users |

### Docs

```bash
cd <skill_path> && node index.js docs create --title "Doc Title"
cd <skill_path> && node index.js docs get --docId DOC_ID
cd <skill_path> && node index.js docs insert-text --docId DOC_ID --text "Hello" --index 1
cd <skill_path> && node index.js docs update-title --docId DOC_ID --title "New Title"
cd <skill_path> && node index.js docs delete --docId DOC_ID
cd <skill_path> && node index.js docs share --id DOC_ID --email "a@b.com,b@c.com" --role writer
```

| Command | Description |
|---------|-------------|
| `create` | Create a new document |
| `get` | Get document content |
| `insert-text` | Insert text at position |
| `update-title` | Rename a document |
| `delete` | Delete a document |
| `share` | Share a document |

### Sheets

```bash
cd <skill_path> && node index.js sheets create --title "Sheet Title"
cd <skill_path> && node index.js sheets get --spreadsheetId SHEET_ID
cd <skill_path> && node index.js sheets values-get --spreadsheetId SHEET_ID --range "Sheet1!A1:C3"
cd <skill_path> && node index.js sheets values-set --spreadsheetId SHEET_ID --range "Sheet1!A1:C3" --values "[[1,2,3]]"
cd <skill_path> && node index.js sheets update-title --spreadsheetId SHEET_ID --title "New Title"
cd <skill_path> && node index.js sheets delete --spreadsheetId SHEET_ID
cd <skill_path> && node index.js sheets share --id SHEET_ID --email "a@b.com,b@c.com" --role writer
```

| Command | Description |
|---------|-------------|
| `create` | Create a new spreadsheet |
| `get` | Get spreadsheet metadata |
| `values-get` | Read cell values |
| `values-set` | Write cell values (`--input RAW\|USER_ENTERED`) |
| `update-title` | Rename a spreadsheet |
| `delete` | Delete a spreadsheet |
| `share` | Share a spreadsheet |

### Slides

```bash
cd <skill_path> && node index.js slides create --title "Deck"
cd <skill_path> && node index.js slides get --presentationId PRESENTATION_ID
cd <skill_path> && node index.js slides insert-text --presentationId PRESENTATION_ID --objectId OBJECT_ID --text "Hello" --index 0
cd <skill_path> && node index.js slides update-title --presentationId PRESENTATION_ID --title "New Title"
cd <skill_path> && node index.js slides delete --presentationId PRESENTATION_ID
cd <skill_path> && node index.js slides share --id PRESENTATION_ID --email "a@b.com,b@c.com" --role writer
```

| Command | Description |
|---------|-------------|
| `create` | Create a new presentation |
| `get` | Get presentation content |
| `insert-text` | Insert text into a shape |
| `update-title` | Rename a presentation |
| `delete` | Delete a presentation |
| `share` | Share a presentation |

### Forms

```bash
cd <skill_path> && node index.js forms create --title "Survey"
cd <skill_path> && node index.js forms get --formId FORM_ID
cd <skill_path> && node index.js forms update-info --formId FORM_ID --title "New Title" --description "Desc"
cd <skill_path> && node index.js forms add-question --formId FORM_ID --title "Question" --options "A,B" --required true
cd <skill_path> && node index.js forms delete --formId FORM_ID
cd <skill_path> && node index.js forms share --id FORM_ID --email "a@b.com,b@c.com" --role writer
```

| Command | Description |
|---------|-------------|
| `create` | Create a new form |
| `get` | Get form content |
| `update-info` | Update title/description |
| `add-question` | Add a multiple-choice question |
| `delete` | Delete a form |
| `share` | Share a form |

### Meet

```bash
cd <skill_path> && node index.js meet list --pageSize 10
cd <skill_path> && node index.js meet create --displayName "Daily Sync"
cd <skill_path> && node index.js meet update --space "spaces/AAA" --displayName "Weekly Sync"
cd <skill_path> && node index.js meet get --space "spaces/AAA"
```

| Command | Description |
|---------|-------------|
| `list` | List meeting spaces |
| `create` | Create a meeting space |
| `update` | Update a meeting space |
| `get` | Get meeting space details |

## Troubleshooting

### Tokens expire every 7 days

This happens when your Google Cloud project's OAuth consent screen is in **Testing** mode. Testing mode limits refresh tokens to 7 days.

**Fix:** Go to Google Cloud Console → APIs & Services → OAuth consent screen → change publishing status from **Testing** to **Production**. For personal use (under 100 users), no app review is required. After switching, refresh tokens will last indefinitely (or until 6 months of inactivity).

### OOB redirect not working

If `urn:ietf:wg:oauth:2.0:oob` is not supported for your OAuth client type, update the redirect URI in `index.js` to `http://localhost` and tell the user to copy the `code` parameter from the browser's address bar after authorization.
