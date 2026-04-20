#!/usr/bin/env node
import https from "https";
import { URL, URLSearchParams } from "url";
import fs from "fs";
import path from "path";
import calendar from "./scripts/calendar.js";
import gmail from "./scripts/gmail.js";
import drive from "./scripts/drive.js";
import docs from "./scripts/docs.js";
import sheets from "./scripts/sheets.js";
import slides from "./scripts/slides.js";
import forms from "./scripts/forms.js";
import meet from "./scripts/meet.js";

const TOKEN_URL = "https://oauth2.googleapis.com/token";
const AUTH_URL = "https://accounts.google.com/o/oauth2/v2/auth";

const SCOPES = [
  "https://www.googleapis.com/auth/calendar",
  "https://www.googleapis.com/auth/gmail.modify",
  "https://www.googleapis.com/auth/gmail.send",
  "https://www.googleapis.com/auth/drive",
  "https://www.googleapis.com/auth/documents",
  "https://www.googleapis.com/auth/spreadsheets",
  "https://www.googleapis.com/auth/presentations",
  "https://www.googleapis.com/auth/forms",
  "https://www.googleapis.com/auth/meetings.space.created",
  "https://www.googleapis.com/auth/meetings.space.readonly",
].join(" ");

const REDIRECT_URI = "urn:ietf:wg:oauth:2.0:oob";

const PRODUCTS = {
  calendar,
  gmail,
  drive,
  docs,
  sheets,
  slides,
  forms,
  meet,
};

const DRIVE_BASE = "https://www.googleapis.com/drive/v3";

let cachedAccessToken = null;
let accessTokenExpiresAt = 0;

function httpRequest(method, url, headers = {}, body) {
  return new Promise((resolve, reject) => {
    const u = new URL(url);
    const req = https.request(
      { method, hostname: u.hostname, path: u.pathname + u.search, headers },
      (res) => {
        let data = "";
        res.on("data", (d) => (data += d));
        res.on("end", () => {
          const contentType = res.headers["content-type"] || "";
          const parsed = parseResponse(data, contentType);
          if (res.statusCode >= 400) {
            const err = new Error(`HTTP ${res.statusCode}: ${data}`);
            err.statusCode = res.statusCode;
            err.body = parsed;
            reject(err);
          } else {
            resolve(parsed);
          }
        });
      }
    );
    req.on("error", reject);
    if (body) req.write(body);
    req.end();
  });
}

function parseResponse(data, contentType) {
  if (!data) return {};
  if (contentType.includes("application/json")) {
    try {
      return JSON.parse(data);
    } catch {
      return data;
    }
  }
  try {
    return JSON.parse(data);
  } catch {
    return data;
  }
}

function isInvalidGrantError(err) {
  const body = err.body;
  if (typeof body === "object" && body !== null) {
    return body.error === "invalid_grant";
  }
  return false;
}

function isInvalidClientError(err) {
  const body = err.body;
  if (typeof body === "object" && body !== null) {
    return body.error === "unauthorized_client" || body.error === "invalid_client";
  }
  return false;
}

async function refreshAccessToken() {
  if (cachedAccessToken && Date.now() < accessTokenExpiresAt - 30_000) {
    return cachedAccessToken;
  }

  const { GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, GOOGLE_REFRESH_TOKEN } =
    process.env;
  if (!GOOGLE_REFRESH_TOKEN) {
    fatal("Missing GOOGLE_REFRESH_TOKEN. Run auth-url first, then auth-exchange.");
  }

  let res;
  try {
    res = await httpRequest(
      "POST",
      TOKEN_URL,
      { "Content-Type": "application/x-www-form-urlencoded" },
      new URLSearchParams({
        client_id: GOOGLE_CLIENT_ID,
        client_secret: GOOGLE_CLIENT_SECRET,
        refresh_token: GOOGLE_REFRESH_TOKEN,
        grant_type: "refresh_token",
      }).toString()
    );
  } catch (err) {
    if (isInvalidGrantError(err)) {
      fatalReauth(
        "Refresh token expired or revoked. Please re-authenticate by providing a new authorization code. " +
          "Tip: if tokens expire every 7 days, switch your Google Cloud project from Testing to Production mode in the OAuth consent screen settings."
      );
    }
    if (isInvalidClientError(err)) {
      fatalInvalidClient(
        "Client credentials are invalid. Please provide new GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET."
      );
    }
    throw err;
  }

  cachedAccessToken = res.access_token;
  accessTokenExpiresAt = Date.now() + res.expires_in * 1000;
  return cachedAccessToken;
}

function generateAuthUrl() {
  const { GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET } = process.env;
  if (!GOOGLE_CLIENT_ID || !GOOGLE_CLIENT_SECRET) {
    fatalInvalidClient(
      "Missing GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET. Please provide valid client credentials."
    );
  }

  const params = new URLSearchParams({
    client_id: GOOGLE_CLIENT_ID,
    redirect_uri: REDIRECT_URI,
    response_type: "code",
    scope: SCOPES,
    access_type: "offline",
    prompt: "consent",
  });

  const url = `${AUTH_URL}?${params}`;
  console.log(JSON.stringify({ url }));
}

async function exchangeCode(code) {
  const { GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET } = process.env;
  if (!GOOGLE_CLIENT_ID || !GOOGLE_CLIENT_SECRET) {
    fatalInvalidClient(
      "Missing GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET. Please provide valid client credentials."
    );
  }

  let tokenRes;
  try {
    tokenRes = await httpRequest(
      "POST",
      TOKEN_URL,
      { "Content-Type": "application/x-www-form-urlencoded" },
      new URLSearchParams({
        client_id: GOOGLE_CLIENT_ID,
        client_secret: GOOGLE_CLIENT_SECRET,
        code,
        grant_type: "authorization_code",
        redirect_uri: REDIRECT_URI,
      }).toString()
    );
  } catch (err) {
    if (isInvalidClientError(err)) {
      fatalInvalidClient(
        "Client credentials are invalid. Please provide new GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET."
      );
    }
    fatal(`Code exchange failed: ${err.message}`);
  }

  if (!tokenRes.refresh_token) {
    fatal("No refresh token returned. Ensure access_type=offline and prompt=consent were used.");
  }

  console.log(JSON.stringify({ refresh_token: tokenRes.refresh_token }));
}

async function apiRequest({ method, url, query, headers = {}, body, rawBody }) {
  const token = await refreshAccessToken();
  const u = new URL(url);
  if (query) {
    Object.entries(query).forEach(([key, value]) => {
      if (value !== undefined && value !== null) u.searchParams.set(key, value);
    });
  }

  const hasRaw = rawBody !== undefined && rawBody !== null;
  const payload = hasRaw ? rawBody : body ? JSON.stringify(body) : undefined;

  const finalHeaders = {
    Authorization: `Bearer ${token}`,
    Accept: "application/json",
    ...headers,
  };

  if (!hasRaw && body) {
    finalHeaders["Content-Type"] = "application/json";
  }

  return httpRequest(method, u.toString(), finalHeaders, payload);
}

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg.startsWith("--")) {
      const key = arg.slice(2);
      const next = argv[i + 1];
      if (!next || next.startsWith("--")) {
        out[key] = true;
      } else {
        out[key] = next;
        i++;
      }
    }
  }
  return out;
}

function parseJsonArg(value, label) {
  if (value === undefined) return undefined;
  try {
    return JSON.parse(value);
  } catch {
    fatal(`Invalid JSON for --${label}`);
  }
}

function parseEmailList(value) {
  if (!value) return [];
  return value
    .split(",")
    .map((email) => email.trim())
    .filter(Boolean);
}

function parseBoolean(value, label, defaultValue) {
  if (value === undefined) return defaultValue;
  if (value === true || value === false) return value;
  const normalized = String(value).toLowerCase();
  if (["true", "1", "yes"].includes(normalized)) return true;
  if (["false", "0", "no"].includes(normalized)) return false;
  fatal(`Invalid boolean for --${label}`);
}

function parseNumberArg(value, label) {
  if (value === undefined) return undefined;
  const parsed = Number(value);
  if (Number.isNaN(parsed)) fatal(`Invalid number for --${label}`);
  return parsed;
}

function getMimeType(filePath) {
  const ext = path.extname(filePath).toLowerCase();
  const map = {
    ".txt": "text/plain",
    ".md": "text/markdown",
    ".json": "application/json",
    ".csv": "text/csv",
    ".pdf": "application/pdf",
    ".png": "image/png",
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".gif": "image/gif",
    ".svg": "image/svg+xml",
    ".webp": "image/webp",
    ".zip": "application/zip",
  };
  return map[ext] || "application/octet-stream";
}

function base64UrlEncode(value) {
  return Buffer.from(value)
    .toString("base64")
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

function base64Wrap(value) {
  return value.replace(/(.{76})/g, "$1\r\n");
}

function readFileBuffer(filePath) {
  return fs.readFileSync(filePath);
}

async function shareFileWithUsers({ fileId, emails, role, notify, message }) {
  const results = [];
  const finalRole = role || "writer";
  if (finalRole === "owner") fatal("Sharing with role=owner is not allowed");

  for (const email of emails) {
    const res = await apiRequest({
      method: "POST",
      url: `${DRIVE_BASE}/files/${fileId}/permissions`,
      query: {
        sendNotificationEmail: notify ? "true" : "false",
        emailMessage: message,
      },
      body: {
        type: "user",
        role: finalRole,
        emailAddress: email,
      },
    });
    results.push({ email, permission: res });
  }

  return results;
}

function fatal(msg) {
  console.error(msg);
  process.exit(1);
}

function fatalReauth(msg) {
  console.error(JSON.stringify({ error: "REAUTH_REQUIRED", message: msg }));
  process.exit(2);
}

function fatalInvalidClient(msg) {
  console.error(JSON.stringify({ error: "INVALID_CLIENT", message: msg }));
  process.exit(3);
}

function printGeneralHelp() {
  console.log("Usage: node index.js <product|command> [subcommand] [--flags]");
  console.log("\nAuth commands:");
  console.log("  auth-url              Generate OAuth consent URL");
  console.log("  auth-exchange --code  Exchange authorization code for refresh token");
  console.log("\nProducts:");
  Object.keys(PRODUCTS).forEach((key) => console.log(`  ${key}`));
}

(async () => {
  const [product, command, ...rest] = process.argv.slice(2);
  if (!product) return printGeneralHelp();

  if (product === "auth-url") return generateAuthUrl();
  if (product === "auth-exchange") {
    const args = parseArgs([command, ...rest].filter(Boolean));
    if (!args.code) fatal("Missing --code flag. Usage: node index.js auth-exchange --code <CODE>");
    return exchangeCode(args.code);
  }

  const module = PRODUCTS[product];
  if (!module) {
    printGeneralHelp();
    fatal(`\nUnknown product: ${product}`);
  }

  const args = parseArgs(rest);
  if (!command || command === "help" || args.help) {
    console.log(module.help || `No help available for ${product}`);
    return;
  }

  const handler = module.commands[command];
  if (!handler) {
    console.log(module.help || `No help available for ${product}`);
    fatal(`Unknown command: ${command}`);
  }

  const ctx = {
    apiRequest,
    parseJsonArg,
    parseNumberArg,
    parseEmailList,
    parseBoolean,
    shareFileWithUsers,
    getMimeType,
    base64UrlEncode,
    base64Wrap,
    readFileBuffer,
    fatal,
  };

  try {
    await handler(ctx, args);
  } catch (err) {
    if (err.message && err.message.includes("invalid_grant")) {
      fatalReauth(
        "Refresh token expired or revoked. Please re-authenticate by providing a new authorization code. " +
          "Tip: if tokens expire every 7 days, switch your Google Cloud project from Testing to Production mode in the OAuth consent screen settings."
      );
    }
    if (err.message && (err.message.includes("invalid_client") || err.message.includes("unauthorized_client"))) {
      fatalInvalidClient(
        "Client credentials are invalid. Please provide new GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET."
      );
    }
    fatal(err.message || String(err));
  }
})();
