const FORMS_BASE = "https://forms.googleapis.com/v1";
const DRIVE_BASE = "https://www.googleapis.com/drive/v3";

function requireArg(ctx, args, key) {
  if (!args[key]) ctx.fatal(`Missing --${key}`);
  return args[key];
}

function parseList(value) {
  if (!value) return [];
  return value.split(",").map((item) => item.trim()).filter(Boolean);
}

async function createForm(ctx, args) {
  const title = requireArg(ctx, args, "title");
  const res = await ctx.apiRequest({
    method: "POST",
    url: `${FORMS_BASE}/forms`,
    body: { info: { title } },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function getForm(ctx, args) {
  const formId = requireArg(ctx, args, "formId");
  const res = await ctx.apiRequest({
    method: "GET",
    url: `${FORMS_BASE}/forms/${formId}`,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function updateInfo(ctx, args) {
  const formId = requireArg(ctx, args, "formId");
  const info = {};
  if (args.title) info.title = args.title;
  if (args.description) info.description = args.description;

  const updateMask = Object.keys(info).join(",");
  if (!updateMask) ctx.fatal("Missing --title or --description");

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${FORMS_BASE}/forms/${formId}:batchUpdate`,
    body: {
      requests: [
        {
          updateFormInfo: {
            info,
            updateMask,
          },
        },
      ],
    },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function addQuestion(ctx, args) {
  const formId = requireArg(ctx, args, "formId");
  const title = requireArg(ctx, args, "title");
  const options = parseList(args.options);
  if (!options.length) ctx.fatal("Missing --options");
  const required = args.required === "true" || args.required === true;
  const index = ctx.parseNumberArg(args.index, "index");

  const item = {
    title,
    questionItem: {
      question: {
        required,
        choiceQuestion: {
          type: "RADIO",
          options: options.map((value) => ({ value })),
        },
      },
    },
  };

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${FORMS_BASE}/forms/${formId}:batchUpdate`,
    body: {
      requests: [
        {
          createItem: {
            item,
            location: index !== undefined ? { index } : undefined,
          },
        },
      ],
    },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function deleteForm(ctx, args) {
  const formId = requireArg(ctx, args, "formId");
  await ctx.apiRequest({
    method: "DELETE",
    url: `${DRIVE_BASE}/files/${formId}`,
  });
  console.log(JSON.stringify({ deleted: formId }, null, 2));
}

async function shareForm(ctx, args) {
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
  name: "forms",
  help: [
    "Forms commands:",
    "  create --title <text>",
    "  get --formId <id>",
    "  update-info --formId <id> [--title <text>] [--description <text>]",
    "  add-question --formId <id> --title <text> --options <A,B> [--required true] [--index <num>]",
    "  delete --formId <id>",
    "  share --id <fileId> --email <a@b.com,b@c.com> [--role reader|writer|commenter] [--notify true|false] [--message <text>]",
  ].join("\n"),
  commands: {
    create: createForm,
    get: getForm,
    "update-info": updateInfo,
    "add-question": addQuestion,
    delete: deleteForm,
    share: shareForm,
  },
};
