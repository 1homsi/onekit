#!/usr/bin/env node
// Builds the onkt npm packages from a goreleaser dist directory: one
// @onkt/cli-<os>-<cpu> package per binary, and the onkt package that depends
// on all of them. Prints the package directories in publish order, the
// platform packages first.
//
//   node scripts/npm-cli.mjs <dist> <version> <out>

import { chmodSync, copyFileSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const [dist, version, out] = process.argv.slice(2);
if (!dist || !version || !out) {
  console.error("usage: node scripts/npm-cli.mjs <dist> <version> <out>");
  process.exit(2);
}
const repo = join(dirname(fileURLToPath(import.meta.url)), "..");

// goreleaser's names for each platform, and what npm calls them.
const platforms = [
  { goos: "darwin", goarch: "arm64", os: "darwin", cpu: "arm64", label: "macOS on Apple silicon" },
  { goos: "darwin", goarch: "amd64", os: "darwin", cpu: "x64", label: "macOS on Intel" },
  { goos: "linux", goarch: "arm64", os: "linux", cpu: "arm64", label: "Linux on arm64" },
  { goos: "linux", goarch: "amd64", os: "linux", cpu: "x64", label: "Linux on x64" },
  { goos: "windows", goarch: "arm64", os: "win32", cpu: "arm64", label: "Windows on arm64" },
  { goos: "windows", goarch: "amd64", os: "win32", cpu: "x64", label: "Windows on x64" },
];

const artifacts = JSON.parse(readFileSync(join(dist, "artifacts.json"), "utf8"));
const binaries = artifacts.filter((a) => a.type === "Binary" && a.extra?.ID === "onkt");

rmSync(out, { recursive: true, force: true });
const license = join(repo, "LICENSE");
const published = [];
const optionalDependencies = {};

for (const p of platforms) {
  const matches = binaries.filter((b) => b.goos === p.goos && b.goarch === p.goarch);
  if (matches.length !== 1) {
    throw new Error(`want one onkt binary for ${p.goos}/${p.goarch} in ${dist}, found ${matches.length}`);
  }
  const name = `@onkt/cli-${p.os}-${p.cpu}`;
  const exe = p.os === "win32" ? "onkt.exe" : "onkt";
  const dir = join(out, `cli-${p.os}-${p.cpu}`);
  mkdirSync(join(dir, "bin"), { recursive: true });
  copyFileSync(matches[0].path, join(dir, "bin", exe));
  // Artifact downloads drop the executable bit; npm keeps the one set here.
  chmodSync(join(dir, "bin", exe), 0o755);
  copyFileSync(license, join(dir, "LICENSE"));
  // No "libc" field: the binary is static (CGO_ENABLED=0), so the Linux
  // packages serve glibc and musl alike.
  writeJSON(join(dir, "package.json"), {
    name,
    version,
    description: `The onkt CLI binary for ${p.label}. Install the onkt package, which picks it.`,
    license: "MIT",
    repository: { type: "git", url: "git+https://github.com/1homsi/onekit.git" },
    os: [p.os],
    cpu: [p.cpu],
    files: ["bin", "LICENSE"],
    preferUnplugged: true,
  });
  writeFileSync(
    join(dir, "README.md"),
    `# ${name}\n\nThe onkt CLI binary for ${p.label}. Install [\`onkt\`](https://www.npmjs.com/package/onkt) instead; it depends on this package and runs the binary.\n`,
  );
  optionalDependencies[name] = version;
  published.push(dir);
}

const main = join(out, "onkt");
mkdirSync(join(main, "bin"), { recursive: true });
const manifest = JSON.parse(readFileSync(join(repo, "npm", "onkt", "package.json"), "utf8"));
writeJSON(join(main, "package.json"), { ...manifest, version, optionalDependencies });
copyFileSync(join(repo, "npm", "onkt", "bin", "onkt.js"), join(main, "bin", "onkt.js"));
chmodSync(join(main, "bin", "onkt.js"), 0o755);
copyFileSync(join(repo, "npm", "onkt", "README.md"), join(main, "README.md"));
copyFileSync(license, join(main, "LICENSE"));
published.push(main);

console.log(published.join("\n"));

function writeJSON(path, value) {
  writeFileSync(path, JSON.stringify(value, null, 2) + "\n");
}
