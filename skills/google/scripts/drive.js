import path from "path";

const BASE = "https://www.googleapis.com/drive/v3";
const UPLOAD_BASE = "https://www.googleapis.com/upload/drive/v3";

const DRIVE_TYPE_MAP = {
  pdf: "application/pdf",
  doc: "application/vnd.google-apps.document",
  sheet: "application/vnd.google-apps.spreadsheet",
  slide: "application/vnd.google-apps.presentation",
  form: "application/vnd.google-apps.form",
  folder: "application/vnd.google-apps.folder",
  image: "image/",
  video: "video/",
};

function requireArg(ctx, args, key) {
  if (!args[key]) ctx.fatal(`Missing --${key}`);
  return args[key];
}

function buildMultipartBody({ metadata, fileBuffer, mimeType, boundary }) {
  const delimiter = `--${boundary}`;
  const closeDelimiter = `--${boundary}--`;

  const metaPart = [
    delimiter,
    "Content-Type: application/json; charset=UTF-8",
    "",
    JSON.stringify(metadata),
    "",
  ].join("\r\n");

  const filePartHeader = [
    delimiter,
    `Content-Type: ${mimeType}`,
    "",
  ].join("\r\n");

  const endPart = ["", closeDelimiter, ""].join("\r\n");

  return Buffer.concat([
    Buffer.from(metaPart, "utf8"),
    Buffer.from(filePartHeader, "utf8"),
    fileBuffer,
    Buffer.from(endPart, "utf8"),
  ]);
}

async function listFiles(ctx, args) {
  const clauses = [];
  if (args.q) clauses.push(args.q);

  if (args.type) {
    const mapped = DRIVE_TYPE_MAP[args.type.toLowerCase()];
    if (!mapped) {
      ctx.fatal(
        `Unknown --type "${args.type}". Valid: ${Object.keys(DRIVE_TYPE_MAP).join(", ")}`
      );
    }
    if (mapped.endsWith("/")) {
      clauses.push(`mimeType contains '${mapped}'`);
    } else {
      clauses.push(`mimeType = '${mapped}'`);
    }
  }

  if (args.name) {
    clauses.push(`name contains '${args.name}'`);
  }

  const q = clauses.length > 0 ? clauses.join(" and ") : undefined;

  const res = await ctx.apiRequest({
    method: "GET",
    url: `${BASE}/files`,
    query: {
      q,
      pageSize: args.pageSize || "10",
      pageToken: args.pageToken,
      fields: args.fields || "files(id,name,mimeType,parents,modifiedTime)",
    },
  });
  console.log(JSON.stringify(res.files || [], null, 2));
}

async function uploadFile(ctx, args) {
  const filePath = requireArg(ctx, args, "file");
  const fileBuffer = ctx.readFileBuffer(filePath);
  const name = args.name || path.basename(filePath);
  const mimeType = ctx.getMimeType(filePath);
  const boundary = `----=_Opencode_${Date.now()}_${Math.random().toString(16).slice(2)}`;

  const metadata = { name };
  if (args.folderId) metadata.parents = [args.folderId];

  const body = buildMultipartBody({ metadata, fileBuffer, mimeType, boundary });

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${UPLOAD_BASE}/files`,
    query: { uploadType: "multipart" },
    headers: { "Content-Type": `multipart/related; boundary=${boundary}` },
    rawBody: body,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function createFolder(ctx, args) {
  const name = requireArg(ctx, args, "name");
  const body = {
    name,
    mimeType: "application/vnd.google-apps.folder",
  };
  if (args.folderId) body.parents = [args.folderId];

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${BASE}/files`,
    body,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function deleteFile(ctx, args) {
  const fileId = requireArg(ctx, args, "fileId");
  await ctx.apiRequest({
    method: "DELETE",
    url: `${BASE}/files/${fileId}`,
  });
  console.log(JSON.stringify({ deleted: fileId }, null, 2));
}

async function shareFile(ctx, args) {
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
  name: "drive",
  help: [
    "Drive commands:",
    "  list [--q <query>] [--type <pdf|doc|sheet|slide|form|folder|image|video>] [--name <text>] [--pageSize 10]",
    "  upload --file <path> [--name <name>] [--folderId <id>]",
    "  create-folder --name <text> [--folderId <parentId>]",
    "  delete --fileId <id>",
    "  share --id <fileId> --email <a@b.com,b@c.com> [--role reader|writer|commenter] [--notify true|false] [--message <text>]",
  ].join("\n"),
  commands: {
    list: listFiles,
    upload: uploadFile,
    "create-folder": createFolder,
    delete: deleteFile,
    share: shareFile,
  },
};
