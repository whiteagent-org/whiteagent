import path from "path";

const BASE = "https://gmail.googleapis.com/gmail/v1";

function requireArg(ctx, args, key) {
  if (!args[key]) ctx.fatal(`Missing --${key}`);
  return args[key];
}

function parseList(value) {
  if (!value) return [];
  return value.split(",").map((item) => item.trim()).filter(Boolean);
}

function buildHeaders({ from, to, cc, bcc, subject, inReplyTo, references }) {
  const headers = [];
  if (from) headers.push(`From: ${from}`);
  if (to) headers.push(`To: ${to}`);
  if (cc) headers.push(`Cc: ${cc}`);
  if (bcc) headers.push(`Bcc: ${bcc}`);
  if (subject) headers.push(`Subject: ${subject}`);
  if (inReplyTo) headers.push(`In-Reply-To: ${inReplyTo}`);
  if (references) headers.push(`References: ${references}`);
  headers.push('MIME-Version: 1.0');
  return headers.join("\r\n");
}

function buildMimeMessage({ headers, text, attachments, boundary }) {
  if (!attachments || attachments.length === 0) {
    return `${headers}\r\n\r\n${text || ""}`;
  }

  const parts = [];
  parts.push(`--${boundary}`);
  parts.push('Content-Type: text/plain; charset="UTF-8"');
  parts.push('Content-Transfer-Encoding: 7bit');
  parts.push("");
  parts.push(text || "");
  parts.push("");

  attachments.forEach((attachment) => {
    parts.push(`--${boundary}`);
    parts.push(`Content-Type: ${attachment.mimeType}; name="${attachment.filename}"`);
    parts.push(
      `Content-Disposition: attachment; filename="${attachment.filename}"`
    );
    parts.push('Content-Transfer-Encoding: base64');
    parts.push("");
    parts.push(attachment.contentBase64Wrapped);
    parts.push("");
  });

  parts.push(`--${boundary}--`);
  return `${headers}\r\nContent-Type: multipart/mixed; boundary="${boundary}"\r\n\r\n${parts.join(
    "\r\n"
  )}`;
}

function buildAttachment(ctx, filePath, filename, mimeType) {
  const buffer = ctx.readFileBuffer(filePath);
  const base64 = buffer.toString("base64");
  return {
    filename,
    mimeType,
    contentBase64Wrapped: ctx.base64Wrap(base64),
  };
}

function decodeBase64Url(value) {
  let base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  while (base64.length % 4) base64 += "=";
  return Buffer.from(base64, "base64").toString("utf8");
}

async function listMessages(ctx, args) {
  let q = args.q || "";
  if (args.after) q += ` after:${args.after.replace(/-/g, "/")}`;
  if (args.before) q += ` before:${args.before.replace(/-/g, "/")}`;
  q = q.trim() || undefined;

  const res = await ctx.apiRequest({
    method: "GET",
    url: `${BASE}/users/me/messages`,
    query: {
      q,
      maxResults: args.max || "10",
      pageToken: args.pageToken,
    },
  });

  const messages = res.messages || [];

  if (args.detail) {
    const detailed = [];
    for (const msg of messages) {
      const detail = await ctx.apiRequest({
        method: "GET",
        url: `${BASE}/users/me/messages/${msg.id}`,
        query: { format: "metadata" },
      });
      const headers = {};
      if (detail.payload && detail.payload.headers) {
        for (const h of detail.payload.headers) {
          headers[h.name.toLowerCase()] = h.value;
        }
      }
      detailed.push({
        id: msg.id,
        threadId: msg.threadId,
        subject: headers.subject || "",
        from: headers.from || "",
        date: headers.date || "",
        snippet: detail.snippet || "",
        labelIds: detail.labelIds || [],
      });
    }
    console.log(JSON.stringify(detailed, null, 2));
    return;
  }

  console.log(JSON.stringify(messages, null, 2));
}

async function getMessage(ctx, args) {
  const messageId = requireArg(ctx, args, "messageId");
  const res = await ctx.apiRequest({
    method: "GET",
    url: `${BASE}/users/me/messages/${messageId}`,
    query: { format: args.format || "full" },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function getMessageRaw(ctx, args) {
  const messageId = requireArg(ctx, args, "messageId");
  const res = await ctx.apiRequest({
    method: "GET",
    url: `${BASE}/users/me/messages/${messageId}`,
    query: { format: "raw" },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function sendMessage(ctx, args) {
  const to = requireArg(ctx, args, "to");
  const subject = requireArg(ctx, args, "subject");
  const text = requireArg(ctx, args, "text");

  const attachments = parseList(args.attachments).map((filePath) =>
    buildAttachment(
      ctx,
      filePath,
      path.basename(filePath),
      ctx.getMimeType(filePath)
    )
  );

  const boundary = `----=_Opencode_${Date.now()}_${Math.random().toString(16).slice(2)}`;
  const headers = buildHeaders({
    from: args.from,
    to,
    cc: args.cc,
    bcc: args.bcc,
    subject,
  });

  const mime = buildMimeMessage({ headers, text, attachments, boundary });
  const raw = ctx.base64UrlEncode(mime);

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${BASE}/users/me/messages/send`,
    body: { raw },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function replyMessage(ctx, args) {
  const to = requireArg(ctx, args, "to");
  const subject = requireArg(ctx, args, "subject");
  const text = requireArg(ctx, args, "text");
  const threadId = requireArg(ctx, args, "threadId");
  const inReplyTo = requireArg(ctx, args, "inReplyTo");

  const headers = buildHeaders({
    from: args.from,
    to,
    cc: args.cc,
    bcc: args.bcc,
    subject,
    inReplyTo,
    references: args.references || inReplyTo,
  });

  const mime = buildMimeMessage({ headers, text, attachments: [] });
  const raw = ctx.base64UrlEncode(mime);

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${BASE}/users/me/messages/send`,
    body: { raw, threadId },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function forwardMessage(ctx, args) {
  const to = requireArg(ctx, args, "to");
  const subject = requireArg(ctx, args, "subject");
  const messageId = requireArg(ctx, args, "messageId");
  const text = args.text || "";

  const rawMessage = await ctx.apiRequest({
    method: "GET",
    url: `${BASE}/users/me/messages/${messageId}`,
    query: { format: "raw" },
  });

  if (!rawMessage.raw) ctx.fatal("No raw message returned for forward");
  const original = decodeBase64Url(rawMessage.raw);
  const originalBase64 = Buffer.from(original).toString("base64");

  const attachments = [
    {
      filename: "forwarded.eml",
      mimeType: "message/rfc822",
      contentBase64Wrapped: ctx.base64Wrap(originalBase64),
    },
  ];

  const boundary = `----=_Opencode_${Date.now()}_${Math.random().toString(16).slice(2)}`;
  const headers = buildHeaders({
    from: args.from,
    to,
    cc: args.cc,
    bcc: args.bcc,
    subject,
  });

  const mime = buildMimeMessage({ headers, text, attachments, boundary });
  const raw = ctx.base64UrlEncode(mime);

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${BASE}/users/me/messages/send`,
    body: { raw },
  });
  console.log(JSON.stringify(res, null, 2));
}

export default {
  name: "gmail",
  help: [
    "Gmail commands:",
    "  list [--q <query>] [--after YYYY-MM-DD] [--before YYYY-MM-DD] [--detail] [--max 10]",
    "  get --messageId <id> [--format full|metadata|minimal|raw]",
    "  get-raw --messageId <id>",
    "  send --to <email> --subject <text> --text <text> [--attachments <paths>]",
    "  reply --to <email> --subject <text> --text <text> --threadId <id> --inReplyTo <id>",
    "  forward --to <email> --subject <text> --messageId <id> [--text <text>]",
  ].join("\n"),
  commands: {
    list: listMessages,
    get: getMessage,
    "get-raw": getMessageRaw,
    send: sendMessage,
    reply: replyMessage,
    forward: forwardMessage,
  },
};
