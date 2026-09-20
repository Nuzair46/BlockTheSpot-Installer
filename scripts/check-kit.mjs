// Checks whether a shipped kit still patches a given Spotify build.
//
// Spotify's web bundle (Apps/xpui.spa) is the same for every desktop platform of a build, and the
// macOS package is a plain bzip2 tar, so a kit's byte signatures can be verified against any
// catalogued version without Windows and without installing anything. This turns "a new Spotify is
// out" into "the kit still matches" or "the kit needs rebuilding" — the failure this repo
// previously only learned about from a user.
//
// Versions come from the catalog published by RobyRew/spotify-versions-history.
//
//   node scripts/check-kit.mjs                # newest Windows x64 build in the catalog
//   node scripts/check-kit.mjs 1.3.1.234      # a specific build
//
// Exits non-zero when a signature no longer matches.
import { spawnSync } from 'node:child_process';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createWriteStream } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';

// Mirrors Compatibility.Kits in src/BlockTheSpot.Core/Compatibility.cs: floor -> kit, highest wins.
const KITS = [
  { id: 'legacy', floor: [1, 2, 70, 0] },
  { id: 'current', floor: [1, 2, 96, 0] },
];

const compare = (a, b) => {
  for (let i = 0; i < 4; i++) if ((a[i] ?? 0) !== (b[i] ?? 0)) return (a[i] ?? 0) - (b[i] ?? 0);
  return 0;
};
const numbers = version => version.split('.').slice(0, 4).map(Number);

function kitFor(version) {
  const value = numbers(version);
  let match = null;
  for (const kit of KITS) if (compare(value, kit.floor) >= 0) match = kit;
  return match;
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { stdio: 'inherit', ...options });
  if (result.status !== 0) throw new Error(`${command} ${args.join(' ')} exited with ${result.status}`);
}

async function download(url, path) {
  const response = await fetch(url, { redirect: 'follow', signal: AbortSignal.timeout(20 * 60_000) });
  if (!response.ok || !response.body) throw new Error(`${url}: HTTP ${response.status}`);
  await pipeline(Readable.fromWeb(response.body), createWriteStream(path));
}

/** The package to pull the bundle out of: the macOS build covers far more versions than Linux. */
function pickSource(entries, version) {
  const of = (platform, format) => entries.find(entry =>
    entry.version === version && entry.platform === platform && entry.format === format &&
    (platform !== 'macos' || entry.architecture === 'x64') && entry.sources.length);
  const macos = of('macos', 'tbz');
  if (macos) return { kind: 'tbz', entry: macos };
  const linux = of('linux', 'deb');
  if (linux) return { kind: 'deb', entry: linux };
  return null;
}

async function extractBundle(kind, file, directory) {
  if (kind === 'tbz') {
    run('tar', ['xjf', file, 'Contents/Resources/Apps/xpui.spa'], { cwd: directory, stdio: 'ignore' });
    return join(directory, 'Contents/Resources/Apps/xpui.spa');
  }
  run('ar', ['x', file, 'data.tar.xz'], { cwd: directory, stdio: 'ignore' });
  run('tar', ['xJf', 'data.tar.xz', '--wildcards', '*/Apps/xpui.spa'], { cwd: directory, stdio: 'ignore' });
  return join(directory, 'usr/share/spotify/Apps/xpui.spa');
}

const CATALOG = 'https://robyrew.github.io/spotify-versions-history/api/v1/catalog.json';
const response = await fetch(CATALOG, { signal: AbortSignal.timeout(60_000) });
if (!response.ok) {
  console.error(`Could not read the version catalog (${CATALOG}): HTTP ${response.status}`);
  process.exit(2);
}
const catalog = await response.json();
const windows = catalog.entries.filter(entry => entry.platform === 'windows' && entry.architecture === 'x64');
const requested = process.argv[2]?.split('.').slice(0, 4).join('.');
const version = requested ?? windows.map(entry => entry.version).sort((a, b) => compare(numbers(a), numbers(b))).at(-1);

const source = pickSource(catalog.entries, version);
if (!source) {
  console.error(`No macOS or Linux package in the catalog for Spotify ${version}; cannot read its bundle.`);
  process.exit(2);
}
const kit = kitFor(version);
if (!kit) {
  console.error(`Spotify ${version} is below every kit floor; nothing to check.`);
  process.exit(2);
}
const url = source.entry.sources[0].url;
console.log(`Spotify ${version} -> ${kit.id} kit\n  bundle: ${url}`);

const work = await mkdtemp(join(tmpdir(), 'bts-kit-'));
try {
  const pkg = join(work, source.kind === 'tbz' ? 'spotify.tbz' : 'spotify.deb');
  await download(url, pkg);
  // verify-config.py reads the .spa zip directly, so there is no unzip step to go wrong.
  const bundle = await extractBundle(source.kind, pkg, work);
  const config = new URL(`../src/BlockTheSpot.Core/Patch/${kit.id}/config.ini`, import.meta.url).pathname;
  const check = spawnSync('python3', [new URL('./verify-config.py', import.meta.url).pathname, config, bundle], { encoding: 'utf8' });
  process.stdout.write(check.stdout ?? '');
  process.stderr.write(check.stderr ?? '');
  if (process.env.GITHUB_STEP_SUMMARY) {
    const verdict = check.status === 0 ? 'matches' : 'DOES NOT MATCH';
    await writeFile(process.env.GITHUB_STEP_SUMMARY,
      `\n### Kit check\n\nThe **${kit.id}** kit ${verdict} Spotify ${version}.\n\n\`\`\`\n${(check.stdout ?? '').trim()}\n\`\`\`\n`,
      { flag: 'a' });
  }
  process.exit(check.status ?? 1);
} finally {
  await rm(work, { recursive: true, force: true });
}
