const SHEETS_BASE = "https://sheets.googleapis.com/v4";
const DRIVE_BASE = "https://www.googleapis.com/drive/v3";

function requireArg(ctx, args, key) {
  if (!args[key]) ctx.fatal(`Missing --${key}`);
  return args[key];
}

async function createSheet(ctx, args) {
  const title = requireArg(ctx, args, "title");
  const res = await ctx.apiRequest({
    method: "POST",
    url: `${SHEETS_BASE}/spreadsheets`,
    body: { properties: { title } },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function getSheet(ctx, args) {
  const spreadsheetId = requireArg(ctx, args, "spreadsheetId");
  const res = await ctx.apiRequest({
    method: "GET",
    url: `${SHEETS_BASE}/spreadsheets/${spreadsheetId}`,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function valuesGet(ctx, args) {
  const spreadsheetId = requireArg(ctx, args, "spreadsheetId");
  const range = requireArg(ctx, args, "range");
  const res = await ctx.apiRequest({
    method: "GET",
    url: `${SHEETS_BASE}/spreadsheets/${spreadsheetId}/values/${encodeURIComponent(
      range
    )}`,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function valuesSet(ctx, args) {
  const spreadsheetId = requireArg(ctx, args, "spreadsheetId");
  const range = requireArg(ctx, args, "range");
  const values = ctx.parseJsonArg(args.values, "values");
  if (!values) ctx.fatal("Missing --values");

  const res = await ctx.apiRequest({
    method: "PUT",
    url: `${SHEETS_BASE}/spreadsheets/${spreadsheetId}/values/${encodeURIComponent(
      range
    )}`,
    query: { valueInputOption: args.input || "RAW" },
    body: { range, values },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function updateTitle(ctx, args) {
  const spreadsheetId = requireArg(ctx, args, "spreadsheetId");
  const title = requireArg(ctx, args, "title");
  const res = await ctx.apiRequest({
    method: "PATCH",
    url: `${DRIVE_BASE}/files/${spreadsheetId}`,
    query: { fields: "id,name" },
    body: { name: title },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function deleteSheet(ctx, args) {
  const spreadsheetId = requireArg(ctx, args, "spreadsheetId");
  await ctx.apiRequest({
    method: "DELETE",
    url: `${DRIVE_BASE}/files/${spreadsheetId}`,
  });
  console.log(JSON.stringify({ deleted: spreadsheetId }, null, 2));
}

async function shareSheet(ctx, args) {
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
  name: "sheets",
  help: [
    "Sheets commands:",
    "  create --title <text>",
    "  get --spreadsheetId <id>",
    "  values-get --spreadsheetId <id> --range <A1> ",
    "  values-set --spreadsheetId <id> --range <A1> --values <json> [--input RAW|USER_ENTERED]",
    "  update-title --spreadsheetId <id> --title <text>",
    "  delete --spreadsheetId <id>",
    "  share --id <fileId> --email <a@b.com,b@c.com> [--role reader|writer|commenter] [--notify true|false] [--message <text>]",
  ].join("\n"),
  commands: {
    create: createSheet,
    get: getSheet,
    "values-get": valuesGet,
    "values-set": valuesSet,
    "update-title": updateTitle,
    delete: deleteSheet,
    share: shareSheet,
  },
};
