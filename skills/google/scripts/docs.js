const DOCS_BASE = "https://docs.googleapis.com/v1";
const DRIVE_BASE = "https://www.googleapis.com/drive/v3";

function requireArg(ctx, args, key) {
  if (!args[key]) ctx.fatal(`Missing --${key}`);
  return args[key];
}

async function createDoc(ctx, args) {
  const title = requireArg(ctx, args, "title");
  const res = await ctx.apiRequest({
    method: "POST",
    url: `${DOCS_BASE}/documents`,
    body: { title },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function getDoc(ctx, args) {
  const docId = requireArg(ctx, args, "docId");
  const res = await ctx.apiRequest({
    method: "GET",
    url: `${DOCS_BASE}/documents/${docId}`,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function insertText(ctx, args) {
  const docId = requireArg(ctx, args, "docId");
  const text = requireArg(ctx, args, "text");
  const index = ctx.parseNumberArg(args.index, "index") ?? 1;

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${DOCS_BASE}/documents/${docId}:batchUpdate`,
    body: {
      requests: [
        {
          insertText: {
            location: { index },
            text,
          },
        },
      ],
    },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function updateTitle(ctx, args) {
  const docId = requireArg(ctx, args, "docId");
  const title = requireArg(ctx, args, "title");
  const res = await ctx.apiRequest({
    method: "PATCH",
    url: `${DRIVE_BASE}/files/${docId}`,
    query: { fields: "id,name" },
    body: { name: title },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function deleteDoc(ctx, args) {
  const docId = requireArg(ctx, args, "docId");
  await ctx.apiRequest({
    method: "DELETE",
    url: `${DRIVE_BASE}/files/${docId}`,
  });
  console.log(JSON.stringify({ deleted: docId }, null, 2));
}

async function shareDoc(ctx, args) {
  const fileId = requireArg(ctx, args, "id");
  const emails = ctx.parseEmailList(requireArg(ctx, args, "email"));
  if (!emails.length) ctx.fatal("Missing --email");
  const role = args.role || "writer";
  const notify = ctx.parseBoolean(args.notify, "notify", true);
  const results = await ctx.shareFileWithUsers({
    fileId,
    emails,
    role,
    notify,
    message: args.message,
  });
  console.log(JSON.stringify(results, null, 2));
}

export default {
  name: "docs",
  help: [
    "Docs commands:",
    "  create --title <text>",
    "  get --docId <id>",
    "  insert-text --docId <id> --text <text> [--index <num>]",
    "  update-title --docId <id> --title <text>",
    "  delete --docId <id>",
    "  share --id <fileId> --email <a@b.com,b@c.com> [--role reader|writer|commenter] [--notify true|false] [--message <text>]",
  ].join("\n"),
  commands: {
    create: createDoc,
    get: getDoc,
    "insert-text": insertText,
    "update-title": updateTitle,
    delete: deleteDoc,
    share: shareDoc,
  },
};
