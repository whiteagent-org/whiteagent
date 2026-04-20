const SLIDES_BASE = "https://slides.googleapis.com/v1";
const DRIVE_BASE = "https://www.googleapis.com/drive/v3";

function requireArg(ctx, args, key) {
  if (!args[key]) ctx.fatal(`Missing --${key}`);
  return args[key];
}

async function createPresentation(ctx, args) {
  const title = requireArg(ctx, args, "title");
  const res = await ctx.apiRequest({
    method: "POST",
    url: `${SLIDES_BASE}/presentations`,
    body: { title },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function getPresentation(ctx, args) {
  const presentationId = requireArg(ctx, args, "presentationId");
  const res = await ctx.apiRequest({
    method: "GET",
    url: `${SLIDES_BASE}/presentations/${presentationId}`,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function insertText(ctx, args) {
  const presentationId = requireArg(ctx, args, "presentationId");
  const objectId = requireArg(ctx, args, "objectId");
  const text = requireArg(ctx, args, "text");
  const index = ctx.parseNumberArg(args.index, "index") ?? 0;

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${SLIDES_BASE}/presentations/${presentationId}:batchUpdate`,
    body: {
      requests: [
        {
          insertText: {
            objectId,
            insertionIndex: index,
            text,
          },
        },
      ],
    },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function updateTitle(ctx, args) {
  const presentationId = requireArg(ctx, args, "presentationId");
  const title = requireArg(ctx, args, "title");
  const res = await ctx.apiRequest({
    method: "PATCH",
    url: `${DRIVE_BASE}/files/${presentationId}`,
    query: { fields: "id,name" },
    body: { name: title },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function deletePresentation(ctx, args) {
  const presentationId = requireArg(ctx, args, "presentationId");
  await ctx.apiRequest({
    method: "DELETE",
    url: `${DRIVE_BASE}/files/${presentationId}`,
  });
  console.log(JSON.stringify({ deleted: presentationId }, null, 2));
}

async function sharePresentation(ctx, args) {
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
  name: "slides",
  help: [
    "Slides commands:",
    "  create --title <text>",
    "  get --presentationId <id>",
    "  insert-text --presentationId <id> --objectId <id> --text <text> [--index <num>]",
    "  update-title --presentationId <id> --title <text>",
    "  delete --presentationId <id>",
    "  share --id <fileId> --email <a@b.com,b@c.com> [--role reader|writer|commenter] [--notify true|false] [--message <text>]",
  ].join("\n"),
  commands: {
    create: createPresentation,
    get: getPresentation,
    "insert-text": insertText,
    "update-title": updateTitle,
    delete: deletePresentation,
    share: sharePresentation,
  },
};
