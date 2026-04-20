const MEET_BASE = "https://meet.googleapis.com/v2";

function requireArg(ctx, args, key) {
  if (!args[key]) ctx.fatal(`Missing --${key}`);
  return args[key];
}

async function listSpaces(ctx, args) {
  const res = await ctx.apiRequest({
    method: "GET",
    url: `${MEET_BASE}/spaces`,
    query: {
      pageSize: args.pageSize || "10",
      pageToken: args.pageToken,
    },
  });
  console.log(JSON.stringify(res.spaces || [], null, 2));
}

async function getSpace(ctx, args) {
  const space = requireArg(ctx, args, "space");
  const res = await ctx.apiRequest({
    method: "GET",
    url: `${MEET_BASE}/${space}`,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function createSpace(ctx, args) {
  const displayName = requireArg(ctx, args, "displayName");
  const res = await ctx.apiRequest({
    method: "POST",
    url: `${MEET_BASE}/spaces`,
    body: { displayName },
  });
  console.log(JSON.stringify(res, null, 2));
}

async function updateSpace(ctx, args) {
  const space = requireArg(ctx, args, "space");
  const displayName = requireArg(ctx, args, "displayName");
  const res = await ctx.apiRequest({
    method: "PATCH",
    url: `${MEET_BASE}/${space}`,
    query: { updateMask: "displayName" },
    body: { displayName },
  });
  console.log(JSON.stringify(res, null, 2));
}

export default {
  name: "meet",
  help: [
    "Meet commands:",
    "  list --pageSize 10",
    "  create --displayName <text>",
    "  update --space <spaces/id> --displayName <text>",
    "  get --space <spaces/id>",
  ].join("\n"),
  commands: {
    list: listSpaces,
    create: createSpace,
    update: updateSpace,
    get: getSpace,
  },
};
