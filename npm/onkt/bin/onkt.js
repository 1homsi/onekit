#!/usr/bin/env node
"use strict";

// onkt is a Go binary published with each GitHub release. On first use this
// script downloads the one for this machine and this package's version,
// checks it against the release's checksums.txt, and caches it where onkt
// keeps the releases a project pins (<user cache>/onekit/releases/<version>).
// Every later run starts the cached binary directly.

const { createHash } = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const { version } = require("../package.json");
const defaultBase = "https://github.com/1homsi/onekit/releases/download";

const goos = { darwin: "darwin", linux: "linux", win32: "windows" }[process.platform];
const goarch = { x64: "amd64", arm64: "arm64" }[process.arch];
if (goos === undefined || goarch === undefined) {
  fail(
    `onkt has no prebuilt binary for ${process.platform}/${process.arch}.\n` +
      `Install it with Go instead: go install github.com/1homsi/onekit/cmd/onkt@v${version}`,
  );
}
const exe = goos === "windows" ? "onkt.exe" : "onkt";
const binary = path.join(cacheDir(), "releases", version, exe);

main().catch((err) => fail(`onkt: ${err.message}`));

async function main() {
  if (!fs.existsSync(binary)) await download();
  const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });
  if (result.error) fail(`onkt: ${result.error.message}`);
  if (result.signal) process.kill(process.pid, result.signal);
  process.exit(result.status ?? 1);
}

async function download() {
  const asset = `onkt-${version}-${goos}-${goarch}${goos === "windows" ? ".exe" : ""}`;
  const base = (env("DOWNLOAD_BASE") || defaultBase).replace(/\/+$/, "");
  const tag = `${base}/v${version}/`;
  process.stderr.write(`onkt: downloading onkt ${version} for ${goos}/${goarch}\n`);
  let checksums, data;
  try {
    checksums = Buffer.from(await get(tag + "checksums.txt")).toString("utf8");
    data = Buffer.from(await get(tag + asset));
  } catch (err) {
    fail(
      `onkt: could not download ${err.message}\n` +
        "Behind a proxy, run Node with NODE_USE_ENV_PROXY=1 and HTTPS_PROXY set, or point\n" +
        "ONKT_DOWNLOAD_BASE at a mirror of the GitHub release downloads.",
    );
  }
  const line = checksums.split("\n").find((l) => l.trim().split(/\s+/)[1] === asset);
  if (line === undefined) fail(`onkt: the v${version} release lists no checksum for ${asset}`);
  const want = line.trim().split(/\s+/)[0];
  const got = createHash("sha256").update(data).digest("hex");
  if (got !== want) fail(`onkt: checksum mismatch for ${asset}: the release lists ${want}, downloaded ${got}`);

  // Written beside its final name and renamed into place, so a concurrent
  // run never starts a half-written binary.
  fs.mkdirSync(path.dirname(binary), { recursive: true });
  const tmp = path.join(path.dirname(binary), `.onkt-download-${process.pid}`);
  fs.writeFileSync(tmp, data, { mode: 0o755 });
  try {
    fs.renameSync(tmp, binary);
  } catch (err) {
    fs.rmSync(tmp, { force: true });
    if (!fs.existsSync(binary)) throw err;
  }
}

async function get(url) {
  let res;
  try {
    res = await fetch(url, { redirect: "follow" });
  } catch (err) {
    throw new Error(`${url}: ${err.cause?.message ?? err.message}`);
  }
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`);
  return res.arrayBuffer();
}

// The directory onkt caches releases in: ONKT_CACHE_DIR, else the user cache
// directory Go's os.UserCacheDir names, plus "onekit".
function cacheDir() {
  const override = env("CACHE_DIR");
  if (override) return override;
  const home = os.homedir();
  let base;
  if (process.platform === "darwin") base = path.join(home, "Library", "Caches");
  else if (process.platform === "win32") base = process.env.LOCALAPPDATA || path.join(home, "AppData", "Local");
  else base = process.env.XDG_CACHE_HOME || path.join(home, ".cache");
  return path.join(base, "onekit");
}

// An ONKT_* environment variable.
function env(name) {
  return process.env[`ONKT_${name}`] || "";
}

function fail(message) {
  process.stderr.write(message + "\n");
  process.exit(1);
}
