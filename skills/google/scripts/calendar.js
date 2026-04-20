const BASE = "https://www.googleapis.com/calendar/v3";

function requireArg(ctx, args, key) {
  if (!args[key]) ctx.fatal(`Missing --${key}`);
  return args[key];
}

async function listCalendars(ctx, args) {
  const items = await listCalendarEntries(ctx, args);
  console.log(JSON.stringify(items, null, 2));
}

async function listEvents(ctx, args) {
  if (args.date) {
    let dateStr;
    if (args.date === "today") {
      dateStr = new Date().toISOString().slice(0, 10);
    } else if (args.date === "tomorrow") {
      const d = new Date();
      d.setDate(d.getDate() + 1);
      dateStr = d.toISOString().slice(0, 10);
    } else {
      dateStr = args.date;
    }
    args.from = `${dateStr}T00:00:00Z`;
    const next = new Date(dateStr + "T00:00:00Z");
    next.setUTCDate(next.getUTCDate() + 1);
    args.to = next.toISOString();
  }

  if (args.all === true || args.all === "true") {
    const calendars = await listCalendarEntries(ctx, args);
    const output = [];
    for (const calendar of calendars) {
      const items = await listEventsForCalendar(ctx, args, calendar.id);
      output.push({ calendar, events: items });
    }
    console.log(JSON.stringify(output, null, 2));
    return;
  }

  const calendarId = args.calendar || "primary";
  const items = await listEventsForCalendar(ctx, args, calendarId);
  console.log(JSON.stringify(items, null, 2));
}

async function listCalendarEntries(ctx, args) {
  const items = [];
  let pageToken;
  const showHidden = args.showHidden === true || args.showHidden === "true";
  do {
    const res = await ctx.apiRequest({
      method: "GET",
      url: `${BASE}/users/me/calendarList`,
      query: {
        pageToken,
        showHidden: showHidden ? "true" : "false",
        minAccessRole: args.minAccessRole,
      },
    });
    if (res.items) items.push(...res.items);
    pageToken = res.nextPageToken;
  } while (pageToken);
  return items;
}

async function listEventsForCalendar(ctx, args, calendarId) {
  const items = [];
  let pageToken;
  do {
    const res = await ctx.apiRequest({
      method: "GET",
      url: `${BASE}/calendars/${encodeURIComponent(calendarId)}/events`,
      query: {
        singleEvents: "true",
        orderBy: "startTime",
        maxResults: args.max || "10",
        timeMin: args.from,
        timeMax: args.to,
        pageToken,
      },
    });
    if (res.items) items.push(...res.items);
    pageToken = res.nextPageToken;
  } while (pageToken);
  return items;
}

async function addEvent(ctx, args) {
  const calendarId = args.calendar || "primary";
  const title = requireArg(ctx, args, "title");
  const start = requireArg(ctx, args, "start");
  const end = requireArg(ctx, args, "end");

  const event = {
    summary: title,
    description: args.desc,
    location: args.location,
    start: { dateTime: start },
    end: { dateTime: end },
    attendees: args.attendees
      ? args.attendees.split(",").map((email) => ({ email: email.trim() }))
      : undefined,
  };

  const res = await ctx.apiRequest({
    method: "POST",
    url: `${BASE}/calendars/${encodeURIComponent(calendarId)}/events`,
    body: event,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function updateEvent(ctx, args) {
  const calendarId = args.calendar || "primary";
  const eventId = requireArg(ctx, args, "eventId");

  const patch = {};
  if (args.title) patch.summary = args.title;
  if (args.desc) patch.description = args.desc;
  if (args.location) patch.location = args.location;
  if (args.start) patch.start = { dateTime: args.start };
  if (args.end) patch.end = { dateTime: args.end };

  const res = await ctx.apiRequest({
    method: "PATCH",
    url: `${BASE}/calendars/${encodeURIComponent(calendarId)}/events/${eventId}`,
    body: patch,
  });
  console.log(JSON.stringify(res, null, 2));
}

async function deleteEvent(ctx, args) {
  const calendarId = args.calendar || "primary";
  const eventId = requireArg(ctx, args, "eventId");
  await ctx.apiRequest({
    method: "DELETE",
    url: `${BASE}/calendars/${encodeURIComponent(calendarId)}/events/${eventId}`,
  });
  console.log(JSON.stringify({ deleted: eventId }, null, 2));
}

export default {
  name: "calendar",
  help: [
    "Calendar commands:",
    "  calendars [--showHidden true] [--minAccessRole <role>]",
    "  list --date <today|tomorrow|YYYY-MM-DD> [--max 10] [--calendar <id>]",
    "  list --from <ISO> --to <ISO> [--max 10] [--calendar <id>]",
    "  list --from <ISO> --to <ISO> --all true [--showHidden true] [--minAccessRole <role>]",
    "  add --title <text> --start <ISO> --end <ISO> [--desc <text>] [--location <text>]",
    "  update --eventId <id> [--title <text>] [--start <ISO>] [--end <ISO>]",
    "  delete --eventId <id>",
  ].join("\n"),
  commands: {
    calendars: listCalendars,
    list: listEvents,
    add: addEvent,
    update: updateEvent,
    delete: deleteEvent,
  },
};
